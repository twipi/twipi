package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"

	"github.com/diamondburned/listener"
	"github.com/diamondburned/twikit/cmd/twid/twid"
	"github.com/diamondburned/twikit/internal/cfgutil"
	"github.com/diamondburned/twikit/twipi"
	"github.com/pkg/errors"
)

var ErrInvalidUsage = errors.New("invalid usage")

var (
	configFile = "twipi.toml"
)

type Config struct {
	Twid  twid.Config  `toml:"twid" json:"twid"`
	Twipi twipi.Config `toml:"twipi" json:"twipi"`
}

func main() {
	flag.StringVar(&configFile, "c", configFile, "config file")
	flag.Usage = func() {
		printf := func(format string, args ...interface{}) {
			fmt.Fprintf(flag.CommandLine.Output(), format, args...)
		}

		printf("Usage:\n")
		printf("  %s [options] [from]\n", os.Args[0])
		if flag.NFlag() > 0 {
			printf("Options:\n")
			flag.PrintDefaults()
		}
	}
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx); err != nil {
		if errors.Is(err, ErrInvalidUsage) {
			flag.Usage()
			os.Exit(2)
		}

		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	from := twipi.PhoneNumber(flag.Arg(0))

	c, err := cfgutil.ParseFile[Config](configFile)
	if err != nil {
		return errors.Wrap(err, "failed to parse config file")
	}

	twipisrv, err := twipi.NewConfiguredServer(c.Twipi)
	if err != nil {
		return err
	}
	defer twipisrv.Close()

	msgCh := make(chan twipi.Message)

	wg.Add(1)
	go func() {
		defer wg.Done()

		for msg := range msgCh {
			if err := json.NewEncoder(os.Stdout).Encode(msg); err != nil {
				log.Println("failed to encode and print message:", err)
			}
		}
		log.Println("message listener exiting")
	}()

	twipisrv.Message.SubscribeMessages(from, msgCh)
	defer twipisrv.Message.UnsubscribeMessages(msgCh)

	server := http.Server{
		Addr:    c.Twid.HTTP.ListenAddr.Value(),
		Handler: twipisrv,
	}

	return listener.HTTPListenAndServeCtx(ctx, &server)
}
