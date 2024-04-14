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

	services := &initializedServices{}
	defer func() { closeAll(logger, services.closers...) }()

	if err := initializeTwisms(cfg, services, logger); err != nil {
		return fmt.Errorf("failed to initialize TwiSMS: %w", err)
	}

	for _, starter := range services.starters {
		starter := starter
		errg.Go(func() error { return starter.Start(ctx) })
	}

	errg.Go(func() error {
		httpRoutes := chi.NewMux()
		httpRoutes.Get("/health", srvutil.Respond200)
		httpRoutes.Handle("/metrics", promhttp.Handler())
		httpRoutes.Handle("/", srvutil.TryHandlers(services.handlers...))

		logger.Info("starting HTTP server", "addr", cfg.ListenAddr)
		if err := hserve.ListenAndServe(ctx, cfg.ListenAddr, httpRoutes); err != nil {
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
	twisms   []twisms.MessageService
	handlers []http.Handler
	starters []Starter
	closers  []io.Closer
}

func (s *initializedServices) add(service any) {
	if v, ok := service.(http.Handler); ok {
		s.handlers = append(s.handlers, v)
	}
	if v, ok := service.(Starter); ok {
		s.starters = append(s.starters, v)
	}
	if v, ok := service.(io.Closer); ok {
		s.closers = append(s.closers, v)
	}
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
