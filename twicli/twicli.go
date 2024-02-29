// Package twicli contains a command-line interface for Twilio. It's designed to
// easily allow modules to parse user packages in a tidy way.
//
// Its API is highly influenced by urfave/cli.
package twicli

import (
	"context"
	"log/slog"
	"sync"

	"github.com/pkg/errors"
	"github.com/twipi/twikit/twipi"
	"libdb.so/ctxt"
)

// Message is a Twilio message. It wraps the orignal Twipi message to add a
// modified body.
type Message struct {
	twipi.Message
	Body string
}

// ActionFunc is the type of the function called by a Command.
type ActionFunc func(ctx context.Context, msg Message) error

// Command is a command-line interface that can be used to parse
// command-line-like messages from users and dispatch them to handlers.
type Command struct {
	Prefix PrefixFunc
	Action ActionFunc
}

// ErrNotMatched is returned by Command.Do if the command does not match the
// given message. The user rarely needs to check for this error.
var ErrNotMatched = errors.New("message did not match command")

// Subcommands creates a new ActionFunc that acts like a nested command.
func Subcommands(cmds []Command) ActionFunc {
	return func(ctx context.Context, msg Message) error {
		for _, cmd := range cmds {
			err := cmd.Do(ctx, msg)
			if errors.Is(err, ErrNotMatched) {
				continue
			}
			if err != nil {
				return err
			}
			return nil
		}
		return ErrNotMatched
	}
}

// Loop starts an event loop for the given MessageHandler that spins and
// dispatches command actions. Actions will be dispatched in goroutines.
func (c *Command) Loop(ctx context.Context, h *twipi.MessageHandler, cli *twipi.Client) {
	ch := make(chan twipi.Message)

	var wg sync.WaitGroup
	defer wg.Wait()

	h.SubscribeMessages("", ch)
	defer h.UnsubscribeMessages(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			wg.Add(1)
			go func() {
				c.DoAndReply(ctx, cli, msg)
				wg.Done()
			}()
		}
	}
}

// Do runs the command. ErrNotMatched is returned if the command does not match
// the given message.
func (c *Command) Do(ctx context.Context, msg Message) error {
	if body, ok := c.Prefix(msg.Body); ok {
		msg.Body = body
		return c.Action(ctx, msg)
	}
	return ErrNotMatched
}

// DoAndReply runs the command and replies to the given message with the
// returned error. If the error is nil, no reply is sent.
func (c *Command) DoAndReply(ctx context.Context, cli *twipi.Client, msg twipi.Message) {
	if err := c.Do(ctx, Message{msg, msg.Body}); err != nil {
		errBody := ErrorMessage(err)
		if err := cli.ReplySMS(ctx, msg, errBody); err != nil {
			logger := ctxt.FromOrFunc(ctx, slog.Default)
			logger.ErrorContext(ctx,
				"cannot reply with error message",
				"do_err", err,
				"reply_err", err,
				"from", msg.From,
				"to", msg.To)
		}
	}
}

// ErrorMessage is a helper function that returns a new message body from the
// given error.
func ErrorMessage(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNotMatched):
		return "Sorry! I'm not sure what you mean."
	default:
		return "Sorry, an error occured: " + err.Error()
	}
}
