package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"

	"github.com/lmittmann/tint"
	"github.com/spf13/pflag"
	"libdb.so/hserve"
)

var (
	listenAddr = ":8080"
	verbose    = false
	jsonLog    = false
)

func main() {
	pflag.StringVarP(&listenAddr, "listen", "l", listenAddr, "Listen address")
	pflag.BoolVarP(&verbose, "verbose", "v", verbose, "Verbose output")
	pflag.BoolVarP(&jsonLog, "json-log", "j", jsonLog, "log output as JSON to stdout")
	pflag.Parse()

	logger := setupLogging()
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	r := http.NewServeMux()
	r.Handle("GET /", newWSHandler(logger))

	logger.Info(
		"starting server",
		"listen_addr", listenAddr)

	if err := hserve.ListenAndServe(ctx, listenAddr, r); err != nil {
		logger.Error(
			"failed to start server",
			"listen_addr", listenAddr,
			"err", err)

		os.Exit(1)
	}
}

func setupLogging() *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	var handler slog.Handler
	if jsonLog {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	} else {
		handler = tint.NewHandler(os.Stderr, &tint.Options{
			Level:   level,
			NoColor: os.Getenv("NO_COLOR") != "",
		})
	}

	logger := slog.New(handler)
	return logger
}
