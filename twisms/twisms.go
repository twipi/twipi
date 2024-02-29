package twisms

import (
	"context"
	"errors"

	"github.com/twipi/twipi/internal/pubsub"
)

// PhoneNumber is a phone number.
type PhoneNumber string

// Message describes an SMS message.
type Message interface {
	// From returns the phone number the message was sent from.
	From() PhoneNumber
	// To returns the phone number the message was sent to.
	To() PhoneNumber
	// Body returns the message body.
	Body() MessageBody
}

// MessageBody describes various types of message bodies.
// The most common type is [MessageBodyText].
type MessageBody interface {
	messageBody()
}

var (
	_ MessageBody = MessageBodyText("")
)

// ErrNonTextMessageBody is returned when a message body is not a text message.
// This is used by [MessageSender] to indicate that the message body is not
// supported.
var ErrNonTextMessageBody = errors.New("non-text message body unsupported")

// MessageBodyText is a simple text message body.
type MessageBodyText string

func (MessageBodyText) messageBody() {}

// MessageSubscriber describes a service that can subscribe to incoming
// messages.
type MessageSubscriber interface {
	// SubscribeMessages subscribes to incoming messages for the given
	// recipient.
	SubscribeMessages(ch chan<- Message, filter pubsub.FilterFunc[Message])
	// UnsubscribeMessages unsubscribes the given channel from incoming
	// messages.
	UnsubscribeMessages(ch chan<- Message)
}

// MessageSender describes a service that can send and reply to messages.
type MessageSender interface {
	// SendSMS sends an SMS message to the given recipient.
	SendSMS(ctx context.Context, msg Message) error
	// ReplySMS replies to the given message.
	ReplySMS(ctx context.Context, msg Message, body MessageBody) error
}
