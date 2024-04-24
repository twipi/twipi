package twisms

import (
	"context"
	"errors"
	"regexp"

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
	// SendingNumber returns the number to send messages from and the cost of
	// sending a message. The cost should be within [0.0, 1.0]. Lower values
	// are chosen first.
	//
	// TODO: figure out a better way to load-balance a pool of phone numbers.
	// Perhaps that is worthy of its own service.
	SendingNumber() (string, float64)
}

// SendTextMessage sends an SMS message with the given text from the given
// sender to the given recipient.
func SendTextMessage(ctx context.Context, s MessageSender, from, to, text string) error {
	return s.SendMessage(ctx, &twismsproto.Message{
		From: from,
		To:   to,
		Body: &twismsproto.MessageBody{
			Text: &twismsproto.TextBody{
				Text: text,
			},
		},
	})
}

// SendAutoTextMessage sends an SMS message with the given text from the
// service's sending number to the given recipient.
func SendAutoTextMessage(ctx context.Context, s MessageSender, to, text string) error {
	from, _ := s.SendingNumber()
	return SendTextMessage(ctx, s, from, to, text)
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

// MessageService describes a service that can both send and receive message
// events. It is a combination of the two interfaces [MessageSubscriber] and
// [MessageSender].
type MessageService interface {
	MessageSubscriber
	MessageSender
}

type combinedMessageService struct {
	MessageSubscriber
	MessageSender
}

var e164Re = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// ErrInvalidPhoneNumber is returned when the phone number is not in E.164
// format.
var ErrInvalidPhoneNumber = errors.New("invalid phone number, must be E.164 format")

// ValidatePhoneNumber validates the given phone number.
func ValidatePhoneNumber(number string) error {
	if !e164Re.MatchString(number) {
		return ErrInvalidPhoneNumber
	}
	return nil
}
