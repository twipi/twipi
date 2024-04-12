package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/lmittmann/tint"
	"github.com/spf13/pflag"
	"github.com/twipi/cfgutil"
	"github.com/twipi/twipi/twid"
	"github.com/twipi/twipi/twid/config"

	_ "github.com/twipi/twipi/twisms/wsbridge"
)

var (
	configFile = "twid.toml"
	verbosity  = 0
)

func main() {
	pflag.StringVarP(&configFile, "config", "c", configFile, "configuration file")
	pflag.CountVarP(&verbosity, "verbose", "v", "verbosity level: warn (0), info, debug")
	pflag.Parse()

	logger := setupLogging()
	slog.SetDefault(logger)

	cfg, err := cfgutil.ParseFile[config.Root](configFile)
	if err != nil {
		logger.Error("failed to parse config file", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := twid.Start(ctx, *cfg, logger); err != nil {
		logger.Error("failed to start twid", "err", err)
		os.Exit(1)
	}
}

func setupLogging() *slog.Logger {
	handler := tint.NewHandler(os.Stderr, &tint.Options{
		Level:   cfgutil.VerbosityToLevel(slog.LevelWarn, verbosity),
		NoColor: os.Getenv("NO_COLOR") != "",
	})
	logger := slog.New(handler)
	return logger
}
