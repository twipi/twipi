package wsbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/twipi/twipi/internal/pubsub"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/proto/out/wsbridgeproto"
	"github.com/twipi/twipi/twid"
	"github.com/twipi/twipi/twisms"
	"nhooyr.io/websocket"
)

func init() {
	twid.RegisterTwismsModule(twid.TwismsModule{
		Name: "wsbridge_server",
		Desc: "Proxy message sends and receives over a Websocket server",
		New: func(raw json.RawMessage, logger *slog.Logger) (twisms.MessageService, error) {
			var cfg ServerServiceConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("failed to unmarshal config: %w", err)
			}
			return NewServerService(cfg, logger), nil
		},
	})
}

// ServerServiceConfig is the configuration for [ClientService].
type ServerServiceConfig struct {
	// PhoneNumbers is the list of phone numbers managed by this service.
	PhoneNumbers []string `json:"phone_numbers"`
	// WSPath is the path that the WS handler will be mounted on.
	WSPath string `json:"ws_path"`
}

// ServerService wraps a Websocket HTTP handler for the wsbridge server.
type ServerService struct {
	conn   atomic.Pointer[websocket.Conn]
	subs   pubsub.Subscriber[*twismsproto.Message]
	msgs   chan *twismsproto.Message
	logger *slog.Logger
	router *chi.Mux
	cfg    ServerServiceConfig
}

var (
	_ twid.Starter             = (*ServerService)(nil)
	_ http.Handler             = (*ServerService)(nil)
	_ twisms.MessageSender     = (*ServerService)(nil)
	_ twisms.MessageSubscriber = (*ServerService)(nil)
)

// NewServerService creates a new Service using the given Websocket address.
func NewServerService(cfg ServerServiceConfig, logger *slog.Logger) *ServerService {
	s := &ServerService{
		msgs:   make(chan *twismsproto.Message),
		logger: logger,
		router: chi.NewMux(),
		cfg:    cfg,
	}
	s.router.Get(cfg.WSPath, s.wsHandler)
	return s
}

func newWSHandler(logger *slog.Logger) http.Handler {
	return &ServerService{logger: logger}
}

// Start implements [twid.Starter].
func (h *ServerService) Start(ctx context.Context) error {
	h.subs.Listen(ctx, h.msgs)
	return ctx.Err()
}

// ServeHTTP implements [http.Handler].
func (h *ServerService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (s *ServerService) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		s.logger.Error(
			"failed to accept websocket connection",
			"headers", r.Header,
			"err", err)
		return
	}
	defer conn.CloseNow()

	logger := s.logger.With()
	logger.Info(
		"accepted new websocket connection",
		"headers", r.Header)

	ctx := r.Context()
	handleWS(ctx, conn, s.logger, func(msg *wsbridgeproto.WebsocketPacket) error {
		switch body := msg.Body.(type) {
		case *wsbridgeproto.WebsocketPacket_Message:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case s.msgs <- body.Message:
				return nil
			}
		default:
			return fmt.Errorf("unexpected message body: %T", body)
		}
	})
}

// SendMessage implements [twisms.MessageSender].
func (s *ServerService) SendMessage(ctx context.Context, msg *twismsproto.Message) error {
	if !slices.Contains(s.cfg.PhoneNumbers, msg.From) {
		return fmt.Errorf("unknown phone number %q to send from", msg.From)
	}

	conn := s.conn.Load()
	if conn == nil {
		return fmt.Errorf("service not started")
	}
	return SendMessage(ctx, conn, msg)
}

// SubscribeMessages implements [twisms.MessageSubscriber].
func (s *ServerService) SubscribeMessages(ch chan<- *twismsproto.Message, filters *twismsproto.MessageFilters) {
	s.subs.Subscribe(ch, func(msg *twismsproto.Message) bool {
		return twisms.FilterMessage(filters, msg)
	})
}

// UnsubscribeMessages implements [twisms.MessageSubscriber].
func (s *ServerService) UnsubscribeMessages(ch chan<- *twismsproto.Message) {
	s.subs.Unsubscribe(ch)
}
