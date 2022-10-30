package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/diamondburned/twikit/cmd/twidiscord/store"
	"github.com/diamondburned/twikit/internal/cfgutil"
	"github.com/diamondburned/twikit/twipi"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

var configFile = "twipi.toml"

func main() {
	flag.StringVar(&configFile, "c", configFile, "config file")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	c, err := cfgutil.ParseFile[config](configFile)
	if err != nil {
		return errors.Wrap(err, "failed to parse config file")
	}

	db, err := store.Open(ctx, c.Discord.Database.String(), false)
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer store.Close(db)

	twipisrv, err := twipi.NewConfiguredServer(c.Config)
	if err != nil {
		return err
	}
	defer twipisrv.Close()

	handler, err := bindHandler(twipisrv, c, db.(storer))
	if err != nil {
		return err
	}

	errg, ctx := errgroup.WithContext(ctx)
	errg.Go(func() error {
		cmd := handler.Command()
		cmd.Loop(ctx, twipisrv.Message, twipisrv.Client)
		return nil
	})
	errg.Go(func() error {
		log.Println("connecting to Discord accounts...")
		defer log.Println("all Discord accounts disconnected")

		return handler.Connect(ctx)
	})
	errg.Go(func() error {
		log.Printf("starting twipi server on %s", c.Twipi.ListenAddr)
		defer log.Println("twipi server stopped")

		return twipisrv.ListenAndServe(ctx)
	})

	return errg.Wait()
}
