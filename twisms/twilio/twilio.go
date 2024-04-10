// Package twilop contains basic abstractions around some Twilio APIs. It's
// designed to easily allow Twilio to be used in a Go application by providing
// high-level abstractions around the API.
//
// If you're making a Twilio application that can handle replying to commands,
// also consider using the twicli package.
package twilio

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twisms"
)

var (
	// ErrUnsupportedMessageBody is returned when a message body is unsupported.
	// Currently, only text messages are supported.
	ErrUnsupportedMessageBody = errors.New("unsupported message body")
	// ErrNoAccount is returned when no account is found for a given phone
	// number.
	ErrNoAccount = errors.New("no account found associated with given input")
)

// Service is the main Twilio service capable of receiving messages using its
// [http.Handler] and sending messages through the API. Note that a Service must
// be closed after use.
//
// It implements the following interfaces:
//   - [twisms.MessageSender]
//   - [twisms.MessageReplier]
//   - [twisms.MessageSubscriber]
//   - [io.Closer]
type Service struct {
	*MessageSender
	*MessageHandler
}

var (
	_ twisms.MessageSender     = (*Service)(nil)
	_ twisms.MessageReplier    = (*Service)(nil)
	_ twisms.MessageSubscriber = (*Service)(nil)
	_ io.Closer                = (*Service)(nil)
)

// NewService creates a new Twilio service using the given configuration.
func NewService(cfg Config) (*Service, error) {
	sender, err := NewMessageSenderFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("could not create Twilio message sender: %w", err)
	}

	handler := NewMessageHandler(
		cfg.Message.Webhook.IncomingEndpoint,
		cfg.Message.Webhook.DeliveryEndpoint,
	)

	return &Service{
		MessageSender:  sender,
		MessageHandler: handler,
	}, nil
}

// Close closes the service. It stops all background work and returns once
// they're all cleaned up.
func (s *Service) Close() error {
	return s.MessageHandler.Close()
}

// ReplyMessage replies to the given message. It tries to reply directly using
// the HTTP handler if possible, deferring to the slower Twilio API otherwise.
func (s *Service) ReplyMessage(ctx context.Context, msg *twismsproto.Message, body *twismsproto.MessageBody) error {
	if s.tryReply(msg, body) {
		return nil
	}
	return s.SendMessage(ctx, twisms.NewReplyingMessage(msg, body))
}
