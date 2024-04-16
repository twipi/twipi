package twicmd

import (
	"context"

	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
)

// CommandParser describes a message body parser that returns a command.
// A CommandParser must be thread-safe.
type CommandParser interface {
	// Name returns the name of the parser.
	// This name is only used internally.
	Name() string
	// Parse parses the given message body and returns the parsed command.
	// It may return (nil, nil) if the message body is not applicable to the
	// parser.
	Parse(context.Context, *ServiceLookup, *twismsproto.MessageBody) (*twicmdproto.Command, error)
}
