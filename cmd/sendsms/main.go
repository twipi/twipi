package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/diamondburned/twikit/internal/cfgutil"
	"github.com/diamondburned/twikit/twipi"
	"github.com/pkg/errors"
)

var ErrInvalidUsage = errors.New("invalid usage")

var (
	configFile = "twipi.toml"
)

func main() {
	flag.StringVar(&configFile, "c", configFile, "config file")
	flag.Usage = func() {
		printf := func(format string, args ...interface{}) {
			fmt.Fprintf(flag.CommandLine.Output(), format, args...)
		}

		printf("Usage:\n")
		printf("  %s [options] <from> <to> [content]\n", os.Args[0])
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
	from := twipi.PhoneNumber(flag.Arg(0))
	to := twipi.PhoneNumber(flag.Arg(1))

	if from == "" || to == "" {
		return ErrInvalidUsage
	}

	content := flag.Arg(2)
	if content == "" {
		log.Println("reading SMS content from stdin...")

		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return errors.Wrap(err, "failed to read from stdin")
		}

		content = string(b)
		content = strings.TrimSuffix(content, "\n")
	}

	c, err := cfgutil.ParseFile[twipi.Config](configFile)
	if err != nil {
		return errors.Wrap(err, "failed to parse config file")
	}

	twipiClient := twipi.NewClient()
	for _, account := range c.Twipi.Accounts {
		twipiClient.AddAccount(account.Value())
	}

	err = twipiClient.SendSMS(ctx, twipi.Message{
		From: from,
		To:   to,
		Body: content,
	})
	if err != nil {
		return errors.Wrap(err, "failed to send SMS")
	}
	return nil
}
