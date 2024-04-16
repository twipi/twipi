package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chzyer/readline"
	"github.com/lmittmann/tint"
	"github.com/spf13/pflag"
	"github.com/twipi/cfgutil"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twisms"
	"github.com/twipi/twipi/twisms/wsbridge"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	wsURL     = "ws://localhost:8080/sms/ws"
	verbosity = 0
)

func main() {
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s [flags] <your_number> [your_number]...\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		pflag.PrintDefaults()
	}
	pflag.StringVarP(&wsURL, "url", "u", wsURL, "URL of the WebSocket server")
	pflag.CountVarP(&verbosity, "verbose", "v", "verbosity level: warn (0), info, debug")
	pflag.Parse()

	if !start(context.Background()) {
		os.Exit(1)
	}
}

func start(ctx context.Context) (ok bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	reader, err := readline.New("Send message: ")
	if err != nil {
		slog.Error(
			"failed to create readline instance",
			"err", err)
		return false
	}

	logHandler := tint.NewHandler(reader.Stdout(), &tint.Options{
		Level:   cfgutil.VerbosityToLevel(slog.LevelInfo, verbosity),
		NoColor: os.Getenv("NO_COLOR") != "",
	})

	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	phoneNumbers := pflag.Args()
	if len(phoneNumbers) == 0 {
		pflag.Usage()
		return false
	}

	clientConfig := wsbridge.ClientServiceConfig{
		PhoneNumbers:           phoneNumbers,
		WSAddress:              wsURL,
		AcknowledgementTimeout: cfgutil.Duration(5 * time.Second),
	}

	clientStartOpts := wsbridge.ClientStartOpts{}

	client := wsbridge.NewClientService(clientConfig, logger)

	errg, ctx := errgroup.WithContext(ctx)
	defer func() { ok = ok && (errg.Wait() == nil) }()

	msgCh := make(chan *twismsproto.Message)

	client.SubscribeMessages(msgCh, nil)
	defer client.UnsubscribeMessages(msgCh)

	var lastReceivedMessage atomic.Pointer[twismsproto.Message]

	errg.Go(func() error {
		for msg := range msgCh {
			if !slices.Contains(phoneNumbers, msg.To) {
				logger.Debug(
					"dropping received message as it is not addressed to this client",
					"from", msg.From,
					"to", msg.To)
				continue
			}
			logger.Info(
				"received message",
				"from", msg.From,
				"to", msg.To,
				"body", msg.Body)
			lastReceivedMessage.Store(msg)
		}
		return nil
	})

	errg.Go(func() error {
		if err := client.Start(ctx, clientStartOpts); err != nil && ctx.Err() == nil {
			logger.Error("failed to start client", tint.Err(err))
			return err
		}
		return nil
	})

	slog.Info("enabling prompt, usage: [recipient_number] [message...]")
	for {
		line, err := reader.Readline()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, readline.ErrInterrupt) {
				return true
			}
			logger.Error("failed to read line", tint.Err(err))
			return false
		}

		toNumber, message, ok := strings.Cut(line, " ")

		// Writing the recipient phone number is optional.
		if !ok || twisms.ValidatePhoneNumber(toNumber) == nil {
			message = line

			// If we have a last received message, reply to that number
			if lastMsg := lastReceivedMessage.Load(); lastMsg != nil {
				toNumber = lastMsg.From
			} else {
				logger.Error("no recipient number specified")
				continue
			}
		}

		if err := client.SendMessage(ctx, &twismsproto.Message{
			From:      phoneNumbers[0],
			To:        toNumber,
			Timestamp: timestamppb.Now(),
			Body: &twismsproto.MessageBody{
				Text: &twismsproto.TextBody{Text: message},
			},
		}); err != nil {
			logger.Error(
				"failed to send message",
				"from", phoneNumbers[0],
				"to", toNumber,
				"body", message,
				tint.Err(err))
		}
	}
}
