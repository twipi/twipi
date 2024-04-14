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
	"github.com/twipi/twipi/twisms"
	"golang.org/x/sync/errgroup"
	"libdb.so/hserve"
)

// Start starts the twid daemon. It runs until the context is canceled.
func Start(ctx context.Context, cfg config.Root, logger *slog.Logger) error {
	errg, ctx := errgroup.WithContext(ctx)

	router := chi.NewMux()
	router.Get("/health", srvutil.Respond200)
	router.Mount("/metrics", promhttp.Handler())

	services := &initializedServices{
		router: router,
	}
	defer func() { closeAll(logger, services.closers...) }()

	if err := initializeTwisms(cfg, services, logger); err != nil {
		return fmt.Errorf("failed to initialize TwiSMS: %w", err)
	}

	for _, starter := range services.starters {
		starter := starter
		errg.Go(func() error { return starter.Start(ctx) })
	}

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

type initializedServices struct {
	router   *chi.Mux
	twisms   []twisms.MessageService
	starters []Starter
	closers  []io.Closer
}

func (s *initializedServices) add(service any, cfg config.TwismsService, logger *slog.Logger) (err error) {
	if v, ok := service.(http.Handler); ok {
		if cfg.HTTPPath == "" {
			logger.Warn("service implements http.Handler but has no HTTP path")
		} else {
			logger.Debug("adding HTTP handler", "path", cfg.HTTPPath)
			(func() {
				// Handle chi's panic.
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("%v", r)
					}
				}()
				s.router.Mount(cfg.HTTPPath, v)
			})()
		}
	}

	if v, ok := service.(Starter); ok {
		logger.Debug("adding starter")
		s.starters = append(s.starters, v)
	}

	if v, ok := service.(io.Closer); ok {
		logger.Debug("adding closer")
		s.closers = append(s.closers, v)
	}

	return nil
}

func closeAll(logger *slog.Logger, closers ...io.Closer) {
	for _, c := range closers {
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
