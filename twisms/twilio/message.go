package twilio

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pkg/errors"
	"github.com/twilio/twilio-go/twiml"
	"github.com/twipi/twipi/internal/pubsub"
	"github.com/twipi/twipi/internal/srvutil"
	"github.com/twipi/twipi/twisms"
	"libdb.so/ctxt"
)

// SMS describes an SMS message.
type SMS struct {
	from    twisms.PhoneNumber
	to      twisms.PhoneNumber
	body    string
	replier chan SMS
}

var _ twisms.Message = SMS{}

// From implements twisms.Message.
func (m SMS) From() twisms.PhoneNumber {
	return m.from
}

// To implements twisms.Message.
func (m SMS) To() twisms.PhoneNumber {
	return m.to
}

// Body implements twisms.Message.
func (m SMS) Body() twisms.MessageBody {
	return twisms.MessageBodyText(m.body)
}

func (m SMS) tryReply(msg SMS) bool {
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
	subs   pubsub.Subscriber[twisms.Message]
	msgs   chan twisms.Message
	stop   chan struct{}
	group  slog.Attr
	router *chi.Mux

	incomingPath string
	deliveryPath string
}

var (
	_ io.Closer                = (*MessageHandler)(nil)
	_ http.Handler             = (*MessageHandler)(nil)
	_ twisms.MessageSubscriber = (*MessageHandler)(nil)
)

// NewMessageHandler creates a new MessageHandler. The user should give the
// constructor the paths for the incoming webhook and the delivery webhook. If
// any of the paths are empty, the corresponding webhook will be disabled.
func NewMessageHandler(incomingPath, deliveryPath string) *MessageHandler {
	l := &MessageHandler{
		msgs: make(chan twisms.Message),
		stop: make(chan struct{}),
		group: slog.Group(
			"message_handler",
			"incoming_path", incomingPath,
			"delivery_path", deliveryPath),
		router:       chi.NewMux(),
		incomingPath: incomingPath,
		deliveryPath: deliveryPath,
	}

	l.subs.Listen(l.msgs)

	r := l.router
	r.Use(srvutil.ParseForm)

	if l.incomingPath != "" {
		r.Get(l.incomingPath, l.handleIncoming)
		r.Post(l.incomingPath, l.handleIncoming)
	}
	if l.deliveryPath != "" {
		r.Get(l.deliveryPath, l.postDelivery)
		r.Post(l.deliveryPath, l.postDelivery)
	}

	return l
}

// NewMessageHandlerFromConfig creates a new MessageHandler from a Config.
// If the MessageHandler is disabled, nil will be returned.
func NewMessageHandlerFromConfig(c Config) *MessageHandler {
	if !c.Message.Enable {
		return nil
	}

	return NewMessageHandler(
		c.Message.Webhook.IncomingEndpoint,
		c.Message.Webhook.DeliveryEndpoint,
	)
}

// SubscribeMessages subscribes the given channel to incoming messages. If
// recipient is not empty, only messages sent to that recipient will be
// published to the channel.
func (l *MessageHandler) SubscribeMessages(ch chan<- twisms.Message, filter pubsub.FilterFunc[twisms.Message]) {
	l.subs.Subscribe(ch, filter)
}

// UnsubscribeMessages unsubscribes the given channel from incoming messages.
func (l *MessageHandler) UnsubscribeMessages(ch chan<- twisms.Message) {
	l.subs.Unsubscribe(ch)
}

// ServeHTTP implements http.Handler.
func (l *MessageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.router.ServeHTTP(w, r)
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

	replyCh := make(chan SMS, 1)

	msg := SMS{
		from:    twisms.PhoneNumber(incoming.From),
		to:      twisms.PhoneNumber(incoming.To),
		body:    incoming.Body,
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

	var reply SMS
	select {
	case reply = <-replyCh:
		// ok, do the reply
	case <-timeout.C:
		// Timeout. Block the channel by trying to fill it with an empty
		// message. If the channel is already full, then the message will be
		// taken.
		select {
		case replyCh <- SMS{}:
			// We blocked the channel. Do nothing.
		case reply = <-replyCh:
			// The channel was already blocked. Do the reply.
		}
	case <-r.Context().Done():
		return
	}

	xml, err := messageToTwiML(reply)
	if err != nil {
		logger := ctxt.FromOrFunc(r.Context(), slog.Default)
		logger.ErrorContext(r.Context(),
			"failed to encode reply TwiML, dropping request", l.group,
			"err", err)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Write(xml)
}

func messageToTwiML(msg SMS) ([]byte, error) {
	xmlDoc, rootElem := twiml.CreateDocument()
	if msg.body != "" {
		token := xmlDoc.CreateElement("Message")
		token.SetText(msg.body)
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
