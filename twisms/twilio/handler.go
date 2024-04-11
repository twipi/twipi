package twilio

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pkg/errors"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/twilio/twilio-go/twiml"
	"github.com/twipi/twipi/internal/pubsub"
	"github.com/twipi/twipi/internal/srvutil"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twisms"
	"libdb.so/ctxt"
)

// MessageHandler is a handler for incoming messages.
type MessageHandler struct {
	subs pubsub.Subscriber[*twismsproto.Message]
	msgs chan *twismsproto.Message
	stop chan struct{}
	wg   sync.WaitGroup

	group  slog.Attr
	router *chi.Mux

	// replies tracks known incoming messages and map them to the right send
	// channel for replying.
	replies *xsync.MapOf[*twismsproto.Message, chan<- *twismsproto.Message]

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
		msgs: make(chan *twismsproto.Message),
		stop: make(chan struct{}),
		group: slog.Group(
			"twilio.component", "message_handler",
			"message_handler",
			"incoming_path", incomingPath,
			"delivery_path", deliveryPath),
		router:       chi.NewMux(),
		replies:      xsync.NewMapOf[*twismsproto.Message, chan<- *twismsproto.Message](),
		incomingPath: incomingPath,
		deliveryPath: deliveryPath,
	}

	l.wg.Add(1)
	go func() {
		l.subs.Listen(l.msgs)
		l.wg.Done()
	}()

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
func (l *MessageHandler) SubscribeMessages(ch chan<- *twismsproto.Message, filters *twismsproto.MessageFilters) {
	l.subs.Subscribe(ch, func(msg *twismsproto.Message) bool {
		return twisms.FilterMessage(filters, msg)
	})
}

// UnsubscribeMessages unsubscribes the given channel from incoming messages.
func (l *MessageHandler) UnsubscribeMessages(ch chan<- *twismsproto.Message) {
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
	l.wg.Wait()
	l.msgs = nil
	return nil
}

func (l *MessageHandler) tryReply(msg *twismsproto.Message, body *twismsproto.MessageBody) bool {
	// Try stealing the reply channel from the map.
	replyCh, ok := l.replies.LoadAndDelete(msg)
	if !ok {
		return false
	}

	select {
	case replyCh <- twisms.NewReplyingMessage(msg, body):
		return true
	default:
		return false
	}
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

	replyCh := make(chan *twismsproto.Message, 1)
	msg := &twismsproto.Message{
		From: incoming.From,
		To:   incoming.To,
		Body: &twismsproto.MessageBody{
			Body: &twismsproto.MessageBody_Text{
				Text: &twismsproto.TextBody{Text: incoming.Body},
			},
		},
	}

	l.replies.Store(msg, replyCh)
	defer l.replies.Delete(msg)

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()

		select {
		case l.msgs <- msg:
		case <-l.stop:
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var reply *twismsproto.Message
	select {
	case reply = <-replyCh:
		// ok, do the reply
	case <-ctx.Done():
		return
	}

	xml, err := messageToTwiML(reply)
	if err != nil {
		logger := ctxt.FromOrFunc(ctx, slog.Default)
		logger.ErrorContext(ctx,
			"failed to encode reply TwiML, dropping request", l.group,
			"err", err)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Write(xml)
}

func messageToTwiML(msg *twismsproto.Message) ([]byte, error) {
	xmlDoc, rootElem := twiml.CreateDocument()

	switch body := msg.Body.Body.(type) {
	case *twismsproto.MessageBody_Text:
		token := xmlDoc.CreateElement("Message")
		token.SetText(body.Text.Text)
		rootElem.AddChild(token)
	default:
		return nil, ErrUnsupportedMessageBody
	}

	xml, err := twiml.ToXML(xmlDoc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal TwiML")
	}

	return []byte(xml), nil
}

func (l *MessageHandler) postDelivery(w http.ResponseWriter, r *http.Request) {
}
