package twid

import (
	"encoding/json"
	"fmt"
	"log/slog"

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

func initializeTwicmd(cfg config.Root, lifecycle *lifecycle, sms twisms.MessageService, logger *slog.Logger) error {
	var parsers []twicmd.CommandParser
	for _, cfg := range cfg.Twicmd.Parsers {
		module, ok := twicmdParsers[cfg.Module]
		if !ok {
			return fmt.Errorf("unknown twicmd parser %s", cfg.Module)
		}

		raw, _ := cfg.MarshalJSON()

		parser, err := module.New(raw, logger)
		if err != nil {
			return fmt.Errorf("cannot create twicmd parser %s: %w", cfg.Module, err)
		}

		parsers = append(parsers, parser)
	}

	services := twicmd.NewServiceLookup()
	for _, cfg := range cfg.Twicmd.Services {
		module, ok := twicmdServices[cfg.Module]
		if !ok {
			return fmt.Errorf("unknown twicmd service %s", cfg.Module)
		}

		raw, _ := cfg.MarshalJSON()

		service, err := module.New(raw, logger)
		if err != nil {
			return fmt.Errorf("cannot create twicmd service %s: %w", cfg.Module, err)
		}

		services.Register(service)
	}

	manager := &twicmd.Manager{
		SMS:      sms,
		Parsers:  parsers,
		Services: services,
		Logger:   logger.With("module", "twicmd"),
		Opts:     twicmd.StartOpts{},
	}

	lifecycle.add(manager, logger)
	return nil
}
