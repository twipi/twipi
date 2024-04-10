package twisms

import (
	"context"

	"github.com/twipi/twipi/proto/out/twismsproto"
)

// MessageSubscriber describes a service that can subscribe to incoming
// messages.
type MessageSubscriber interface {
	// SubscribeMessages subscribes to incoming messages for the given
	// recipient.
	SubscribeMessages(ch chan<- *twismsproto.Message, filters *twismsproto.MessageFilters)
	// UnsubscribeMessages unsubscribes the given channel from incoming
	// messages.
	UnsubscribeMessages(ch chan<- *twismsproto.Message)
}

// FilterMessage filters the given message based on the given filters.
// It implements the given set of filters in Go code.
func FilterMessage(filters *twismsproto.MessageFilters, msg *twismsproto.Message) bool {
	for _, filter := range filters.GetFilters() {
		switch filter := filter.GetFilter().(type) {
		case *twismsproto.MessageFilter_MatchFrom:
			if msg.GetFrom() != filter.MatchFrom {
				return false
			}
		case *twismsproto.MessageFilter_MatchTo:
			if msg.GetTo() != filter.MatchTo {
				return false
			}
		}
	}
	return true
}

// MessageSender describes a service that can send messages.
type MessageSender interface {
	// SendMessage sends an SMS message to the given recipient.
	SendMessage(ctx context.Context, msg *twismsproto.Message) error
}

// MessageReplier describes a service that can reply to messages.
// It is meant to extend the MessageSender interface and provide services with a
// fast path for synchronous replies. It is optional and services can choose to
// not implement it.
type MessageReplier interface {
	MessageSender

	// ReplyMessage replies to the given message.
	ReplyMessage(ctx context.Context, msg *twismsproto.Message, body *twismsproto.MessageBody) error
}

// NewReplyingMessage creates a new message that is a reply to the given message
// with the given body.
func NewReplyingMessage(msg *twismsproto.Message, body *twismsproto.MessageBody) *twismsproto.Message {
	return &twismsproto.Message{
		From: msg.To,
		To:   msg.From,
		Body: body,
	}
}

// ReplyMessage is a helper function that replies to the given message using the
// provided MessageSender. If it implements MessageReplier, it will use the fast
// path for synchronous replies.
func ReplyMessage(ctx context.Context, s MessageSender, msg *twismsproto.Message, body *twismsproto.MessageBody) error {
	if r, ok := s.(MessageReplier); ok {
		return r.ReplyMessage(ctx, msg, body)
	}
	reply := twismsproto.Message{
		From: msg.To,
		To:   msg.From,
		Body: body,
	}
	return s.SendMessage(ctx, &reply)
}

// MessageSubscribeSender is a convenience interface that combines
// [MessageSubscriber] and [MessageSender].
type MessageSubscribeSender interface {
	MessageSubscriber
	MessageSender
}

type combinedMessageSubscribeSender struct {
	MessageSubscriber
	MessageSender
}

// CombineMessageSubscribeSender combines a [MessageSubscriber] and a
// [MessageSender] into a single [MessageSubscribeSender].
func CombineMessageSubscribeSender(sub MessageSubscriber, send MessageSender) MessageSubscribeSender {
	return &combinedMessageSubscribeSender{
		MessageSubscriber: sub,
		MessageSender:     send,
	}
}
