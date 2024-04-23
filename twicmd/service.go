package twicmd

import (
	"context"
	"fmt"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/twipi/twipi/internal/xiter"
	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/twisms"
)

// Service describes a command service capable of executing parsed commands
// using [Service.Execute] that comply to its own definition returned by
// [Service.Service].
type Service interface {
	// Name returns the name of the service. This name must be constant and must
	// match what is returned by [Service.Service].Name.
	Name() string
	// Service returns the service blob that describes the command service.
	// It is expected to return the same service for the lifetime of the
	// executor.
	Service(ctx context.Context) (*twicmdproto.Service, error)
	// Execute executes the given command and returns the message body
	// to be replied back to the sender.
	Execute(context.Context, *twicmdproto.ExecuteRequest) (*twicmdproto.ExecuteResponse, error)

	twisms.MessageSubscriber
}

// validateService validates the twicmd service.
// It is meant to be called by implementations of
// [CommandParser.RegisterService], but the parser may also do additional
// validation as suitable.
func validateService(service *twicmdproto.Service) error {
	// TODO: validate command names
	for _, cmd := range service.Commands {
		if cmd.Name == "" {
			return fmt.Errorf("empty command name")
		}

		if len(cmd.ArgumentPositions) > 0 {
			for _, name := range cmd.ArgumentPositions {
				if cmd.Arguments[name] == nil {
					return fmt.Errorf(
						"command %q: missing argument %q",
						cmd.Name, name)
				}
			}
		} else if cmd.ArgumentTrailing {
			return fmt.Errorf(
				"command %q: trailing arguments are not supported",
				cmd.Name)
		}
	}

	return nil
}

// ServiceLookup provides ways to lookup services, which can be arbitrarily
// removed and added.
type ServiceLookup struct {
	services *xsync.MapOf[string, Service]
}

// TODO: convert the local map to a ServiceRegistry interface.

// NewServiceLookup creates a new empty [ServiceLookup] instance.
func NewServiceLookup() *ServiceLookup {
	return &ServiceLookup{
		services: xsync.NewMapOf[string, Service](),
	}
}

// Register registers a service for the command parser.
// If the service already exists, it will be replaced.
func (l *ServiceLookup) Register(service Service) {
	l.services.Store(service.Name(), service)
}

// Service returns a service by its name.
func (l *ServiceLookup) Service(name string) (Service, bool) {
	service, ok := l.services.Load(name)
	return service, ok
}

// ResolvedService is a tuple of a service and its description.
type ResolvedService struct {
	Service     Service
	Description *twicmdproto.Service
}

// Lookup looks up a service by its name
// If the service is not found, nil on each or both is returned.
func (l *ServiceLookup) Lookup(ctx context.Context, serviceName string) (*ResolvedService, error) {
	service, ok := l.services.Load(serviceName)
	if !ok {
		return nil, nil
	}

	serviceDesc, err := service.Service(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateService(serviceDesc); err != nil {
		return nil, fmt.Errorf("invalid service %q definition: %w", serviceName, err)
	}

	return &ResolvedService{
		Service:     service,
		Description: serviceDesc,
	}, nil
}

// ServiceCommandTuple is a tuple of a service and a command.
type ServiceCommandTuple struct {
	ResolvedService
	Command *twicmdproto.CommandDescription
}

// LookupCommand looks up a command by its name.
func (l *ServiceLookup) LookupCommand(ctx context.Context, serviceName, commandName string) (*ServiceCommandTuple, error) {
	service, err := l.Lookup(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	for _, cmd := range service.Description.Commands {
		if cmd.Name == commandName {
			return &ServiceCommandTuple{
				ResolvedService: *service,
				Command:         cmd,
			}, nil
		}
	}

	return nil, fmt.Errorf("unknown command %q for service %q", commandName, serviceName)
}

// AllServices resolves and returns all registered services.
// The iterator will be valid for as long as the context is valid.
func (l *ServiceLookup) AllServices(ctx context.Context) xiter.Seq2[*ResolvedService, error] {
	// TODO: parallelize me!
	return func(yield func(*ResolvedService, error) bool) bool {
		ok := true
		l.services.Range(func(_ string, service Service) bool {
			desc, err := service.Service(ctx)
			if err != nil {
				yield(nil, fmt.Errorf("failed to resolve service %q: %w", service.Name(), err))
				ok = false
				return ok
			}

			if err := validateService(desc); err != nil {
				yield(nil, fmt.Errorf("invalid service %q definition: %w", service.Name(), err))
				ok = false
				return ok
			}

			ok = yield(&ResolvedService{Service: service, Description: desc}, nil)
			return ok
		})
		return ok
	}
}
