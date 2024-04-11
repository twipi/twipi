// Package twicmd provides a command parsing and dispatching framework for
// Twipi. The most simple implementation of it is slashparser.
package twicmd

import (
	"context"
	"fmt"

	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
)

// CommandParser describes a message body parser that returns a command.
// A CommandParser must be thread-safe.
type CommandParser interface {
	// RegisterService registers a service for the command parser.
	// If the service is already registered with the same name, it must be
	// overwritten.
	RegisterService(context.Context, *twicmdproto.Service) error
	// Parse parses the given message body and returns the parsed command.
	Parse(context.Context, *twismsproto.MessageBody) (*twicmdproto.Command, error)
}

// ValidateService validates the twicmd service.
func ValidateService(service *twicmdproto.Service) error {
	// TODO: validate command names
	for _, cmd := range service.Commands {
		if cmd.Name == "" {
			return fmt.Errorf("service %q: empty command name", service.Name)
		}

		if len(cmd.ArgumentPositions) > 0 {
			for _, name := range cmd.ArgumentPositions {
				if cmd.Arguments[name] == nil {
					return fmt.Errorf(
						"service %q: command %q: missing argument %q",
						service.Name, cmd.Name, name)
				}
			}
		} else if cmd.ArgumentTrailing {
			return fmt.Errorf(
				"service %q: command %q: trailing arguments are not supported",
				service.Name, cmd.Name)
		}
	}

	return nil
}
