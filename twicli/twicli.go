// Package twicli contains a command-line interface for Twilio. It's designed to
// easily allow modules to parse user packages in a tidy way.
//
// Its API is highly influenced by urfave/cli.
package twicli

import (
	"context"
	"strings"
	"sync"

	"github.com/diamondburned/twikit/logger"
	"github.com/diamondburned/twikit/twipi"
	"github.com/pkg/errors"
)

// PrefixFunc returns true if the given message body string should activate the
// current command.
type PrefixFunc func(string) (string, bool)

// NewNaturalPrefix returns a PrefixFunc that matches the prefix of a message
// with the phrase "X, ", e.g. "Discord, message <1> ABC". The prefix is matched
// in a case-insensitive manner.
func NewNaturalPrefix(name string) PrefixFunc {
	prefix := strings.ToLower(name) + ","
	return func(msg string) (string, bool) {
		first, tail, err := PopFirstWord(msg)
		if err != nil {
			return "", false
		}

		if strings.ToLower(first) != prefix {
			return "", false
		}

		return tail, true
	}
}

// NewSlashPrefix returns a PrefixFunc that matches the prefix of a message with
// the phrase "/X ", e.g. "/message <1> ABC". The prefix is matched in a
// case-sensitive manner.
func NewSlashPrefix(name string) PrefixFunc {
	prefix := "/" + name
	return func(msg string) (string, bool) {
		first, tail, err := PopFirstWord(msg)
		if err != nil {
			return "", false
		}

		if first != prefix {
			return "", false
		}

		return tail, true
	}
}

// NewWordPrefix returns a PrefixFunc that matches the prefix of a message with
// a word.
func NewWordPrefix(word string, cased bool) PrefixFunc {
	return func(msg string) (string, bool) {
		first, tail, err := PopFirstWord(msg)
		if err != nil {
			return "", false
		}

		var ok bool
		if cased {
			ok = first == word
		} else {
			ok = strings.EqualFold(first, word)
		}

		return tail, ok
	}
}

// CombinePrefixes combines multiple PrefixFuncs into a single PrefixFunc. The
// returned PrefixFunc will return true if any of the given PrefixFuncs return
// true.
func CombinePrefixes(prefixes ...PrefixFunc) PrefixFunc {
	return func(msg string) (string, bool) {
		for _, prefix := range prefixes {
			if body, ok := prefix(msg); ok {
				return body, true
			}
		}
		return "", false
	}
}

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

// Do runs the command. ErrNotMatched is returned if the command does not match
// the given message.
func (c *Command) Do(ctx context.Context, msg Message) error {
	if body, ok := c.Prefix(msg.Body); ok {
		msg.Body = body
		return c.Action(ctx, msg)
	}
	return ErrNotMatched
}

// Loop starts an event loop for the given MessageHandler that spins and
// dispatches command actions. Actions will be dispatched in goroutines.
func (c *Command) Loop(ctx context.Context, h *twipi.MessageHandler, cli *twipi.Client) {
	log := logger.FromContext(ctx)
	log = log.WithPrefix("twicli")

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
				defer wg.Done()

				reply := func(body string) {
					if err := cli.ReplySMS(ctx, msg, body); err != nil {
						log.Printf("%s replying to %s: cannot send SMS: %v", msg.From, msg.To, err)
					}
				}

				if err := c.Do(ctx, Message{msg, msg.Body}); err != nil {
					if errors.Is(err, ErrNotMatched) {
						reply("Sorry! I'm not sure what you mean.")
					} else {
						reply("Sorry, an error occured: " + err.Error())
					}
				}
			}()
		}
	}
}
