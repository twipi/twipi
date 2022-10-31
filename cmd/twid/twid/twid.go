package twid

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/diamondburned/listener"
	"github.com/diamondburned/twikit/internal/cfgutil"
	"github.com/diamondburned/twikit/logger"
	"github.com/diamondburned/twikit/twicli"
	"github.com/diamondburned/twikit/twipi"
	"github.com/go-chi/chi/v5"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

// Config is the twid config block.
type Config struct {
	HTTP struct {
		ListenAddr cfgutil.EnvString `toml:"listen_addr" json:"listen_addr"`
	} `toml:"http" json:"http"`
}

// ConfigType is the type of the configuration file, e.g. "toml" or "json".
type ConfigType string

const (
	TOMLConfig ConfigType = "toml"
	JSONConfig ConfigType = "json"
)

var modules = []Module{}

var illegalNames = []string{
	"twid",
	"twipi",
}

// Register registers a module to be loaded by the twid server.
func Register(mod Module) {
	for _, illegalName := range illegalNames {
		if mod.Name == illegalName {
			panic("illegal module name: " + illegalName)
		}
	}

	modules = append(modules, mod)
}

// Module is a module that can be loaded by the twid server.
type Module struct {
	// Name is the name of the module.
	Name string
	// New is the constructor that creates a new Handler.
	New func() Handler
}

// Handler is a handler instance created by a registered module.
type Handler interface {
	// Config returns the module's configuration. The configuration is assumed
	// to be the root structure, and each module should wrap its configuration
	// in a block named after the module.
	Config() any
	// Start starts the module.
	Start(ctx context.Context) error
}

// TwipiHandler is a module that can bind a Twipi server.
type TwipiHandler interface {
	Handler
	// BindTwipi binds the configured Twipi server to the module.
	BindTwipi(*twipi.ConfiguredServer)
}

// CommandHandler is a module that uses the twicli.Command API.
type CommandHandler interface {
	Handler
	// Command returns the module's root command. Commands are checked against
	// collisions.
	Command() twicli.Command
}

// HTTPCommander is a module that implements HTTP serving.
type HTTPCommander interface {
	Handler
	// HTTPHandler returns the HTTP handler for the module.
	HTTPHandler() http.Handler
	// HTTPPrefix returns the HTTP prefix that the module will serve on. The
	// prefix must not contain a trailing slash.
	HTTPPrefix() string
}

// Loader is a module loader. It assists in loading a list of modules and
// starting them.
type Loader struct {
	Config struct {
		Twid  Config       `toml:"twid" json:"twid"`
		Twipi twipi.Config `toml:"twipi" json:"twipi"`
	}

	handlers map[string]Handler
	enabled  map[string]bool

	mux   *chi.Mux
	http  *http.Server
	twipi *twipi.ConfiguredServer
}

// NewLoader creates a new loader with the given modules.
func NewLoader(modules []Module) *Loader {
	handlers := make(map[string]Handler, len(modules))
	for _, module := range modules {
		handlers[module.Name] = module.New()
	}

	return &Loader{
		handlers: handlers,
		enabled:  make(map[string]bool, len(modules)),
	}
}

// NewGlobalLoader creates a new loader with the global modules.
func NewGlobalLoader() *Loader {
	return NewLoader(modules)
}

// Main runs the twid server as if it were to be executed from a package main
// program. This function is extremely useful when code-generating files.
func Main() {
	configFile := "twipi.toml"

	flag.StringVar(&configFile, "c", configFile, "config file")
	flag.Parse()

	loader := NewGlobalLoader()

	if err := loader.LoadConfigFile(configFile); err != nil {
		log.Fatalln("failed to load config file:", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := loader.Start(ctx); err != nil {
		log.Fatalln("failed to start twid:", err)
	}
}

// LoadConfigFile loads the configuration file from the given path into all the
// module handlers.
func (l *Loader) LoadConfigFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrap(err, "failed to read config file")
	}

	return l.LoadConfig(b, strings.TrimPrefix(filepath.Ext(path), "."))
}

// LoadConfig loads the configuration from the given bytes into all the module
// handlers. configType determines the type of the configuration file, e.g.
// "toml" or "json".
func (l *Loader) LoadConfig(b []byte, configType string) error {
	var enabledBlocks map[string]struct {
		Enable bool `toml:"enable" json:"enable"`
	}

	configs := []any{
		&l.Config,
		&enabledBlocks,
	}
	for _, handler := range l.handlers {
		if config := handler.Config(); config != nil {
			configs = append(configs, config)
		}
	}

	if err := cfgutil.ParseMany(b, configType, configs...); err != nil {
		return errors.Wrap(err, "failed to parse config")
	}

	for name, enabled := range enabledBlocks {
		l.enabled[name] = enabled.Enable
	}

	if l.enabled["twipi"] {
		twipisrv, err := twipi.NewConfiguredServer(l.Config.Twipi)
		if err != nil {
			return errors.Wrap(err, "failed to create twipi server")
		}

		l.twipi = twipisrv

		for name, handler := range l.handlers {
			if !l.enabled[name] {
				continue
			}
			if twipiHandler, ok := handler.(TwipiHandler); ok {
				twipiHandler.BindTwipi(twipisrv)
			}
		}
	}

	if l.Config.Twid.HTTP.ListenAddr != "" {
		l.mux = chi.NewMux()
		l.http = &http.Server{
			Addr:    l.Config.Twid.HTTP.ListenAddr.Value(),
			Handler: l.mux,
		}

		for name, handler := range l.handlers {
			if !l.enabled[name] {
				continue
			}

			if httpHandler, ok := handler.(HTTPCommander); ok {
				l.mux.Mount(httpHandler.HTTPPrefix(), httpHandler.HTTPHandler())
			}
		}
	}

	// Bind the twipi router last.
	if l.twipi != nil {
		l.mux.Mount("/", l.twipi)
	}

	return nil
}

// Start starts the HTTP server and the loaded modules' handlers. It blocks
// until the context is canceled or any of the handlers fail to start.
func (l *Loader) Start(ctx context.Context) error {
	ctx = logger.WithLogPrefix(ctx, "twid")

	if !l.enabled["twid"] {
		return errors.New("twid is not enabled") // lol ??
	}

	errg, ctx := errgroup.WithContext(ctx)

	if l.twipi != nil {
		errg.Go(func() error {
			l.twipi.UpdateTwilio(ctx)
			return nil
		})
		defer l.twipi.Close()
	}

	if l.http != nil {
		errg.Go(func() error {
			log := logger.FromContext(ctx)
			log.Printf("starting HTTP server on %s", l.http.Addr)
			defer log.Println("HTTP server stopped")

			return listener.HTTPListenAndServeCtx(ctx, l.http)
		})
	}

	for name, handler := range l.handlers {
		if !l.enabled[name] {
			log := logger.FromContext(ctx)
			log.Println("skipping disabled module", name)
			continue
		}

		ctx := logger.WithLogPrefix(ctx, name)
		handler := handler

		errg.Go(func() error {
			log := logger.FromContext(ctx)
			log.Println("starting module")
			defer log.Println("module stopped")

			return handler.Start(ctx)
		})

		if commander, ok := handler.(CommandHandler); ok {
			if l.twipi == nil {
				return errors.New("twipi is not configured")
			}

			errg.Go(func() error {
				cmd := commander.Command()
				cmd.Loop(ctx, l.twipi.Message, l.twipi.Client)
				return nil
			})
		}
	}

	return errg.Wait()
}
