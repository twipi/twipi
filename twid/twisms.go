package twid

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twid/config"
	"github.com/twipi/twipi/twisms"
)

var twismsModules = map[string]TwismsModule{}

// TwismsModule describes a Twisms module.
type TwismsModule struct {
	Name string
	Desc string
	New  func(cfg json.RawMessage, logger *slog.Logger) (twisms.MessageService, error)
}

// RegisterTwismsModule registers a new Twisms module globally.
func RegisterTwismsModule(module TwismsModule) {
	if _, ok := twismsModules[module.Name]; ok {
		panic(fmt.Sprintf("twisms module %q already registered", module.Name))
	}
	twismsModules[module.Name] = module
}

func initializeTwisms(cfg config.Root, lifecycle *lifecycle, router *chi.Mux, logger *slog.Logger) (twisms.MessageService, error) {
	var services []twisms.MessageService

	for _, serviceCfg := range cfg.Twisms.Services {
		module, ok := twismsModules[serviceCfg.Module]
		if !ok {
			return nil, fmt.Errorf("unknown twisms module %s", serviceCfg.Module)
		}

		logger := logger.With(
			"module", "twisms",
			"twisms_module", module.Name)

		serviceCfgRaw, _ := serviceCfg.MarshalJSON()

		service, err := module.New(serviceCfgRaw, logger)
		if err != nil {
			return nil, fmt.Errorf("cannot create twisms service: %w", err)
		}

		if handler, ok := service.(http.Handler); ok {
			if err := addRoute(router, serviceCfg, handler, logger); err != nil {
				return nil, fmt.Errorf("cannot add twisms service route: %w", err)
			}
		}

		services = append(services, service)
		lifecycle.add(service, logger)
	}

	return &twismsWrapper{services: services}, nil
}

type twismsWrapper struct {
	services []twisms.MessageService
}

var _ twisms.MessageSubscriber = (*twismsWrapper)(nil)

func (s *twismsWrapper) SubscribeMessages(ch chan<- *twismsproto.Message, filters *twismsproto.MessageFilters) {
	for _, sub := range s.services {
		sub.SubscribeMessages(ch, filters)
	}
}

func (s *twismsWrapper) UnsubscribeMessages(ch chan<- *twismsproto.Message) {
	for _, sub := range s.services {
		sub.UnsubscribeMessages(ch)
	}
}

func (s *twismsWrapper) SendMessage(ctx context.Context, msg *twismsproto.Message) error {
	var err error
	for _, sender := range s.services {
		err = sender.SendMessage(ctx, msg)
		if err == nil {
			return nil
		}
	}
	return err
}

func (s *twismsWrapper) ReplyMessage(ctx context.Context, msg *twismsproto.Message, body *twismsproto.MessageBody) error {
	var err error
	for _, sender := range s.services {
		err = twisms.ReplyMessage(ctx, sender, msg, body)
		if err == nil {
			return nil
		}
	}
	return err
}
