package twid

import (
	"context"
	"fmt"
	"io"
	"log/slog"

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

	twisms, err := initializeTwismsService(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize TwiSMS: %w", err)
	}
	defer closeAll(logger, twisms.closers...)

	for _, starter := range twisms.starters {
		starter := starter
		errg.Go(func() error { return starter.Start(ctx) })
	}

	errg.Go(func() error {
		httpRoutes := chi.NewMux()
		httpRoutes.Get("/health", srvutil.Respond200)
		httpRoutes.Handle("/metrics", promhttp.Handler())
		httpRoutes.Handle("/", srvutil.TryHandlers(twisms.handlers...))

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
