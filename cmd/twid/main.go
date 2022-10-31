package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/diamondburned/twikit/cmd/twid/twid"

	_ "github.com/diamondburned/twikit/cmd/twid/twidiscord/module"
)

var configFile = "twipi.toml"

func init() {
	flag.StringVar(&configFile, "c", configFile, "config file")
}

func main() {
	flag.Parse()

	loader := twid.NewGlobalLoader()

	if err := loader.LoadConfigFile(configFile); err != nil {
		log.Fatalln("failed to load config file:", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := loader.Start(ctx); err != nil {
		log.Fatalln("failed to start twid:", err)
	}
}
