package twid

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/twipi/twipi/internal/srvutil"
	"github.com/twipi/twipi/twid/config"
	"golang.org/x/sync/errgroup"
	"libdb.so/hserve"
)

// Start starts the twid daemon. It runs until the context is canceled.
func Start(ctx context.Context, cfg config.Root, logger *slog.Logger) error {
	errg, ctx := errgroup.WithContext(ctx)

	lifecycle := &lifecycle{}
	defer lifecycle.close(logger)

	router := chi.NewMux()
	router.Get("/health", srvutil.Respond200)
	router.Mount("/metrics", promhttp.Handler())

	sms, err := initializeTwisms(cfg, lifecycle, router, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize TwiSMS: %w", err)
	}

	if err := initializeTwicmd(cfg, lifecycle, sms, logger); err != nil {
		return fmt.Errorf("failed to initialize Twicmd: %w", err)
	}

	errg.Go(func() error {
		logger.Info("starting all services")
		return lifecycle.start(ctx)
	})

	errg.Go(func() error {
		logger.Info("starting HTTP server", "addr", cfg.ListenAddr)
		if err := hserve.ListenAndServe(ctx, cfg.ListenAddr, router); err != nil {
			slog.Error(
				"failed to start HTTP server",
				"addr", cfg.ListenAddr,
				"err", err)
			return err
		}
		return nil
	})

	return errg.Wait()
}

func addRoute(router *chi.Mux, cfg config.TwismsService, service http.Handler, logger *slog.Logger) (err error) {
	if cfg.HTTPPath == "" {
		return fmt.Errorf(
			"service %T implements http.Handler but has no http_path configured",
			service)
	}

	logger.Debug("adding HTTP handler", "path", cfg.HTTPPath)

	// Handle chi's panic.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	router.Mount(cfg.HTTPPath, service)
	return nil
}

// lifecycle is a helper struct to manage the lifecycle of services.
type lifecycle struct {
	starters []Starter
	closers  []io.Closer
}

func (s *lifecycle) add(service any, logger *slog.Logger) {
	if v, ok := service.(Starter); ok {
		logger.Debug("adding starter")
		s.starters = append(s.starters, v)
	}

	if v, ok := service.(io.Closer); ok {
		logger.Debug("adding closer")
		s.closers = append(s.closers, v)
	}
}

func (s *lifecycle) start(ctx context.Context) error {
	errg, ctx := errgroup.WithContext(ctx)
	for _, starter := range s.starters {
		starter := starter
		errg.Go(func() error { return starter.Start(ctx) })
	}
	return errg.Wait()
}

func (s *lifecycle) close(logger *slog.Logger) {
	for _, c := range s.closers {
		if err := c.Close(); err != nil {
			logger.Error("failed to close", "err", err)
		}
	}
}

// Starter describes any service that needs to be started.
// It is recommended to implement this over [io.Closer].
type Starter interface {
	Start(ctx context.Context) error
}
