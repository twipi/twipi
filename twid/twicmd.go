package twid

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twicmd"
	"github.com/twipi/twipi/twid/config"
	"github.com/twipi/twipi/twisms"
)

var twicmdParsers = map[string]TwicmdParser{}

// TwicmdParser describes a Twicmd parser module.
type TwicmdParser struct {
	Name string
	New  func(cfg json.RawMessage, logger *slog.Logger) (twicmd.CommandParser, error)
}

// RegisterTwicmdParser registers a new Twicmd parser module globally.
func RegisterTwicmdParser(module TwicmdParser) {
	if _, ok := twicmdParsers[module.Name]; ok {
		panic("twicmd parser already registered")
	}
	twicmdParsers[module.Name] = module
}

var twicmdServices = map[string]TwicmdService{}

// TwicmdService describes a Twicmd service module.
type TwicmdService struct {
	Name string
	New  func(cfg json.RawMessage, logger *slog.Logger) (twicmd.Service, error)
}

// RegisterTwicmdService registers a new Twicmd service module globally.
func RegisterTwicmdService(service TwicmdService) {
	if _, ok := twicmdServices[service.Name]; ok {
		panic("twicmd service already registered")
	}
	twicmdServices[service.Name] = service
}

func initializeTwicmd(cfg config.Root, lifecycle *lifecycle, sms twisms.MessageService, logger *slog.Logger) (*twicmd.Manager, error) {
	var parsers []twicmd.CommandParser
	for _, cfg := range cfg.Twicmd.Parsers {
		module, ok := twicmdParsers[cfg.Module]
		if !ok {
			return nil, fmt.Errorf("unknown twicmd parser %s", cfg.Module)
		}

		logger := logger.With(
			"module", "twicmd",
			"twicmd.component", "parser",
			"twicmd.parser", cfg.Module)

		raw, _ := cfg.MarshalJSON()

		parser, err := module.New(raw, logger)
		if err != nil {
			return nil, fmt.Errorf("cannot create twicmd parser %s: %w", cfg.Module, err)
		}

		parsers = append(parsers, parser)
		lifecycle.add(parser, logger)
	}

	services := twicmd.NewServiceLookup()
	for _, cfg := range cfg.Twicmd.Services {
		module, ok := twicmdServices[cfg.Module]
		if !ok {
			return nil, fmt.Errorf("unknown twicmd service %s", cfg.Module)
		}

		logger := logger.With(
			"module", "twicmd",
			"twicmd.component", "service",
			"twicmd.service", cfg.Module)

		raw, _ := cfg.MarshalJSON()

		service, err := module.New(raw, logger)
		if err != nil {
			return nil, fmt.Errorf("cannot create twicmd service %s: %w", cfg.Module, err)
		}

		services.Register(service)
		lifecycle.add(service, logger)

		// TODO: make module.New accept sms so we don't have to handle this
		// ourselves.

		lifecycle.starters = append(lifecycle.starters,
			starterFunc(func(ctx context.Context) error {
				ch := make(chan *twismsproto.Message)
				service.SubscribeMessages(ch, nil)
				defer service.UnsubscribeMessages(ch)
				for {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case msg := <-ch:
						sms.SendMessage(ctx, msg)
					}
				}
			}),
		)
	}

	manager := &twicmd.Manager{
		SMS:      sms,
		Parsers:  parsers,
		Services: services,
		Logger:   logger.With("module", "twicmd"),
		Opts:     twicmd.StartOpts{},
	}

	lifecycle.add(manager, manager.Logger)
	return manager, nil
}

type starterFunc func(ctx context.Context) error

func (f starterFunc) Start(ctx context.Context) error {
	return f(ctx)
}
