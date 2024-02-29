package twipi

import (
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pkg/errors"
	"github.com/twilio/twilio-go/twiml"
	"github.com/twipi/twikit/internal/pubsub"
	"github.com/twipi/twikit/internal/slogctx"
	"github.com/twipi/twikit/internal/srvutil"
)

// PhoneNumber is a phone number.
type PhoneNumber string

// Message describes an SMS message.
type Message struct {
	From PhoneNumber
	To   PhoneNumber
	Body string

	replier chan Message
}

func (m *Message) tryReply(msg Message) bool {
	if m.replier == nil {
		return false
	}

	select {
	case m.replier <- msg:
		return true
	default:
		return false
	}
}

// MessageHandler is a handler for incoming messages.
type MessageHandler struct {
	subs  pubsub.Subscriber[Message]
	msgs  chan Message
	stop  chan struct{}
	group slog.Attr

	incomingPath string
	deliveryPath string
}

var _ WebhookRegisterer = (*MessageHandler)(nil)

// NewMessageHandler creates a new MessageHandler. The user should give the
// constructor the paths for the incoming webhook and the delivery webhook. If
// any of the paths are empty, the corresponding webhook will be disabled.
func NewMessageHandler(incomingPath, deliveryPath string) *MessageHandler {
	h := &MessageHandler{
		msgs: make(chan Message),
		stop: make(chan struct{}),
		group: slog.Group(
			"message_handler",
			"incoming_path", incomingPath,
			"delivery_path", deliveryPath),
		incomingPath: incomingPath,
		deliveryPath: deliveryPath,
	}
	h.subs.Listen(h.msgs)
	return h
}

// SubscribeMessages subscribes the given channel to incoming messages. If
// recipient is not empty, only messages sent to that recipient will be
// published to the channel.
func (l *MessageHandler) SubscribeMessages(recipient PhoneNumber, ch chan<- Message) {
	filter := func(msg Message) bool { return true }
	if recipient != "" {
		filter = func(msg Message) bool { return msg.To == recipient }
	}

	l.subs.Subscribe(ch, filter)
}

// UnsubscribeMessages unsubscribes the given channel from incoming messages.
func (l *MessageHandler) UnsubscribeMessages(ch chan<- Message) {
	l.subs.Unsubscribe(ch)
}

// Mount implements WebhookRegisterer.
func (l *MessageHandler) Mount(r chi.Router) {
	r.Use(srvutil.ParseForm)

	if l.incomingPath != "" {
		r.Get(l.incomingPath, l.handleIncoming)
		r.Post(l.incomingPath, l.handleIncoming)
	}
	if l.deliveryPath != "" {
		r.Get(l.deliveryPath, l.postDelivery)
		r.Post(l.deliveryPath, l.postDelivery)
	}
}

// Close implements WebhookRegisterer.
func (l *MessageHandler) Close() error {
	if l.msgs == nil {
		return net.ErrClosed
	}

	close(l.stop)
	l.msgs = nil
	return nil
}

type incomingMessage struct {
	MessageSID          string `schema:"MessageSid"`
	SMSSID              string `schema:"SmsSid"`
	AccountSID          string `schema:"AccountSid"`
	MessagingServiceSID string `schema:"MessagingServiceSid"`
	From                string `schema:"From"`
	To                  string `schema:"To"`
	Body                string `schema:"Body"`
}

func (l *MessageHandler) handleIncoming(w http.ResponseWriter, r *http.Request) {
	incoming := incomingMessage{
		MessageSID:          r.FormValue("MessageSid"),
		SMSSID:              r.FormValue("SmsSid"),
		AccountSID:          r.FormValue("AccountSid"),
		MessagingServiceSID: r.FormValue("MessagingServiceSid"),
		From:                r.FormValue("From"),
		To:                  r.FormValue("To"),
		Body:                r.FormValue("Body"),
	}

	replyCh := make(chan Message, 1)

	msg := Message{
		From:    PhoneNumber(incoming.From),
		To:      PhoneNumber(incoming.To),
		Body:    incoming.Body,
		replier: replyCh,
	}

	go func() {
		select {
		case l.msgs <- msg:
		case <-l.stop:
		}
	}()

	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()

	var reply Message
	select {
	case reply = <-replyCh:
		// ok, do the reply
	case <-timeout.C:
		// Timeout. Block the channel by trying to fill it with an empty
		// message. If the channel is already full, then the message will be
		// taken.
		select {
		case replyCh <- Message{}:
			// We blocked the channel. Do nothing.
		case reply = <-replyCh:
			// The channel was already blocked. Do the reply.
		}
	case <-r.Context().Done():
		return
	}

	xml, err := messageToTwiML(reply)
	if err != nil {
		logger := slogctx.From(r.Context())
		logger.ErrorContext(r.Context(),
			"failed to encode reply TwiML, dropping request", l.group,
			"err", err)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Write(xml)
}

func messageToTwiML(msg Message) ([]byte, error) {
	xmlDoc, rootElem := twiml.CreateDocument()
	if msg.Body != "" {
		token := xmlDoc.CreateElement("Message")
		token.SetText(msg.Body)
		rootElem.AddChild(token)
	}

	xml, err := twiml.ToXML(xmlDoc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal TwiML")
	}

	return []byte(xml), nil
}

func (l *MessageHandler) postDelivery(w http.ResponseWriter, r *http.Request) {
}
