// Package twicmd provides a command parsing and dispatching framework for
// Twipi. The most simple implementation of it is slashparser.
package twicmd

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twisms"
)

// StartOpts describes the optional options for starting the command parsing
// and dispatching framework. All fields are optional.
type StartOpts struct {
	// Filters is the list of message filters to apply.
	// This parameter is optional.
	Filters *twismsproto.MessageFilters
}

// Manager handles the command parsing and dispatching framework.
// It is in charge of reading incoming messages from the channel and dispatching
// them to the appropriate command parsers.
type Manager struct {
	SMS      twisms.MessageService
	Parsers  []CommandParser
	Services *ServiceLookup
	Logger   *slog.Logger
	Opts     StartOpts
}

// Start starts the manager.
func (s *Manager) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	msgCh := make(chan *twismsproto.Message)
	logger := s.Logger

	s.SMS.SubscribeMessages(msgCh, s.Opts.Filters)
	defer s.SMS.UnsubscribeMessages(msgCh)

	for {
		var msg *twismsproto.Message
		var ok bool

		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok = <-msgCh:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("message channel closed")
			}
		}

		dispatchCtx := &dispatchContext{
			msg: msg,
			logger: logger.With(
				"from", msg.From,
				"to", msg.To,
				"timestamp", msg.Timestamp.AsTime()),
			lookup:  s.Services,
			msgs:    s.SMS,
			parsers: s.Parsers,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			dispatchCtx.logger.Debug("dispatching message")
			dispatchCtx.dispatch(ctx)
		}()
	}
}

type dispatchContext struct {
	msg     *twismsproto.Message
	logger  *slog.Logger
	lookup  *ServiceLookup
	msgs    twisms.MessageSender
	parsers []CommandParser
}

func (d *dispatchContext) dispatch(ctx context.Context) {
	var commandParser CommandParser
	var command *twicmdproto.Command
	var err error

	for _, parser := range d.parsers {
		command, err = parser.Parse(ctx, d.lookup, d.msg.Body)
		if err != nil {
			d.replyText(ctx, err.Error())
			return
		}
		if command != nil {
			commandParser = parser
			break
		}
	}

	if command == nil {
		d.replyText(ctx, "Cannot understand command (no available parser)")
		return
	}

	service, ok := d.lookup.Service(command.Service)
	if !ok {
		d.logger.Error(
			"parser returned unknown service (bug)",
			"parser", commandParser.Name(),
			"service", command.Service)
		return
	}

	d.logger.Debug(
		"dispatching command",
		"service", service.Name(),
		"command", command.Command)

	resp, err := service.Execute(ctx, &twicmdproto.ExecuteRequest{
		Command: command,
		Message: d.msg,
	})
	if err != nil {
		d.logger.Error(
			"failed to execute command",
			"service", service.Name(),
			"command", command.Command,
			"message", d.msg.String(),
			"err", err)
		d.replyText(ctx, "An error occurred while executing the command.")
		return
	}

	switch response := resp.Response.(type) {
	case *twicmdproto.ExecuteResponse_Text:
		d.replyText(ctx, response.Text)
	case *twicmdproto.ExecuteResponse_Body:
		d.reply(ctx, response.Body)
	case *twicmdproto.ExecuteResponse_Status:
		// TODO: use AI to transform the status into a more human-friendly message
		d.replyText(ctx, response.Status)
	}
}

func (d *dispatchContext) replyText(ctx context.Context, status string) {
	d.reply(ctx, &twismsproto.MessageBody{
		Text: &twismsproto.TextBody{
			Text: status,
		},
	})
}

func (d *dispatchContext) reply(ctx context.Context, body *twismsproto.MessageBody) {
	d.logger.Debug(
		"replying with message",
		"body", body.String())

	if err := twisms.ReplyMessage(ctx, d.msgs, d.msg, body); err != nil {
		d.logger.Error(
			"failed to send reply",
			"body", body.String(),
			"err", err)
	}
}
