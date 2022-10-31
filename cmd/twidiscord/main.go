package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/diamondburned/listener"
	"github.com/diamondburned/twikit/cmd/twidiscord/store"
	"github.com/diamondburned/twikit/cmd/twidiscord/twidiscord"
	"github.com/diamondburned/twikit/cmd/twidiscord/web/routes"
	"github.com/diamondburned/twikit/internal/cfgutil"
	"github.com/diamondburned/twikit/twipi"
	"github.com/go-chi/chi/v5"
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
	c, err := cfgutil.ParseFile[twidiscord.Config](configFile)
	if err != nil {
		return errors.Wrap(err, "failed to parse config file")
	}

	db, err := store.Open(ctx, c.Discord.DatabaseURI.String(), false)
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer store.Close(db)

	twipisrv, err := twipi.NewConfiguredServer(c.Config)
	if err != nil {
		return err
	}
	defer twipisrv.Close()

	handler := bindHandler(twipisrv, c, db)

	r := chi.NewMux()
	r.Mount("/discord", routes.Mount(twipisrv, c, handler.accountAdder()))
	r.Mount("/", twipisrv)

	errg, ctx := errgroup.WithContext(ctx)
	errg.Go(func() error {
		cmd := handler.Command()
		cmd.Loop(ctx, twipisrv.Message, twipisrv.Client)
		return nil
	})
	errg.Go(func() error {
		return handler.Connect(ctx)
	})
	errg.Go(func() error {
		log.Printf("starting twipi server on %s", c.ListenAddr)
		defer log.Println("twipi server stopped")

		server := &http.Server{
			Addr:    c.ListenAddr,
			Handler: r,
		}

		return listener.HTTPListenAndServeCtx(ctx, server)
	})

	return errg.Wait()
}
