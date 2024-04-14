package wsbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/twipi/cfgutil"
	"github.com/twipi/twipi/internal/catchupstorage"
	"github.com/twipi/twipi/internal/pubsub"
	"github.com/twipi/twipi/internal/xcontainer"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/proto/out/wsbridgeproto"
	"github.com/twipi/twipi/twid"
	"github.com/twipi/twipi/twisms"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	// MessageQueue is the message queue to use for catchup messages.
	// If nil, no catchup messages will be sent.
	MessageQueue *catchupstorage.MessageQueueConfig `json:"message_queue"`
	// AcknowledgementTimeout is the timeout for message acknowledgements.
	// If 0, then no acknowledgement is required.
	AcknowledgementTimeout cfgutil.Duration `json:"acknowledgement_timeout"`
}

// ServerService wraps a Websocket HTTP handler for the wsbridge server.
type ServerService struct {
	subs    pubsub.Subscriber[*twismsproto.Message]
	msgs    chan *twismsproto.Message
	ready   chan struct{}
	service *xcontainer.Signaled[*serverService]
	conns   *xsync.MapOf[string, *xsync.MapOf[*websocket.Conn, struct{}]] // phone number -> conn
	logger  *slog.Logger
	cfg     ServerServiceConfig
}

var (
	_ twid.Starter             = (*ServerService)(nil)
	_ http.Handler             = (*ServerService)(nil)
	_ twisms.MessageSender     = (*ServerService)(nil)
	_ twisms.MessageSubscriber = (*ServerService)(nil)
)

// NewServerService creates a new Service using the given Websocket address.
func NewServerService(cfg ServerServiceConfig, logger *slog.Logger) *ServerService {
	return &ServerService{
		msgs:    make(chan *twismsproto.Message),
		ready:   make(chan struct{}),
		service: xcontainer.NewSignaled[*serverService](),
		conns:   xsync.NewMapOf[string, *xsync.MapOf[*websocket.Conn, struct{}]](),
		logger:  logger,
		cfg:     cfg,
	}
}

// Start implements [twid.Starter].
func (s *ServerService) Start(ctx context.Context) error {
	errg, ctx := errgroup.WithContext(ctx)

	errg.Go(func() error {
		s.subs.Listen(ctx, s.msgs)
		return ctx.Err()
	})

	errg.Go(func() error {
		ss, err := newServerService(ctx, s)
		if err != nil {
			s.logger.Error(
				"could not finish creating the server service",
				"err", err)
			return fmt.Errorf("could not create server service: %w", err)
		}

		s.service.Signal(ss)

		<-ctx.Done()
		if err := ss.Close(); err != nil {
			return err
		}

		return ctx.Err()
	})

	return errg.Wait()
}

// ServeHTTP implements [http.Handler].
func (s *ServerService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hh, ok := s.service.Value()
	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	hh.wsHandler(w, r)
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

// SendMessage implements [twisms.MessageSender].
func (s *ServerService) SendMessage(ctx context.Context, msg *twismsproto.Message) error {
	if !slices.Contains(s.cfg.PhoneNumbers, msg.From) {
		return fmt.Errorf("unknown phone number %q to send from", msg.From)
	}

	ss, err := s.service.Wait(ctx)
	if err != nil {
		return err
	}

	return ss.SendMessage(ctx, msg)
}

type serverService struct {
	*ServerService
	queue *catchupstorage.MessageQueue
	acks  *messageAcks
}

func newServerService(ctx context.Context, h *ServerService) (*serverService, error) {
	var queue *catchupstorage.MessageQueue
	var err error

	if h.cfg.MessageQueue == nil {
		queue, err = catchupstorage.NewMessageQueue(
			ctx,
			h.cfg.MessageQueue,
			h.logger.With("module", "wsbridge_server.message_queue"))
		if err != nil {
			return nil, fmt.Errorf("could not create message queue: %w", err)
		}
	}

	return &serverService{
		ServerService: h,
		queue:         queue,
		acks:          newMessageAcks(h.cfg.AcknowledgementTimeout.AsDuration()),
	}, nil
}

func (s *serverService) Close() error {
	if err := s.queue.Close(); err != nil {
		return fmt.Errorf("could not close message queue: %w", err)
	}
	return nil
}

func (s *serverService) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode:    websocket.CompressionContextTakeover,
		InsecureSkipVerify: true,
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

	var phoneNumbers []string
	defer func() { s.unregisterConn(conn, phoneNumbers) }()

	ctx := r.Context()
	handleWS(ctx, conn, s.logger, func(msg *wsbridgeproto.WebsocketPacket) error {
		switch body := msg.Body.(type) {
		case *wsbridgeproto.WebsocketPacket_Introduction:
			// Set the slice that will be deferred later.
			phoneNumbers = body.Introduction.PhoneNumbers
			s.registerConn(conn, phoneNumbers)

			if body.Introduction.Since != nil {
				// TODO: Implement this
			}

			return nil

		case *wsbridgeproto.WebsocketPacket_Message:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case s.msgs <- body.Message.Message:
			}

			if ackID := body.Message.AcknowledgementId; ackID != nil {
				if err := sendMessageAcknowledgement(ctx, conn, *ackID); err != nil {
					return fmt.Errorf("could not send message acknowledgement: %w", err)
				}
			}

			return nil

		case *wsbridgeproto.WebsocketPacket_MessageAcknowledgement:
			if s.acks != nil {
				s.acks.acknowledge(body.MessageAcknowledgement.AcknowledgementId)
			}
			return nil

		default:
			return fmt.Errorf("unexpected message body: %T", body)
		}
	})
}

func (s *ServerService) registerConn(conn *websocket.Conn, phoneNumbers []string) {
	for _, number := range phoneNumbers {
		connMap, _ := s.conns.LoadOrCompute(number, func() *xsync.MapOf[*websocket.Conn, struct{}] {
			return xsync.NewMapOfPresized[*websocket.Conn, struct{}](3)
		})
		connMap.Store(conn, struct{}{})
	}
}

func (s *ServerService) unregisterConn(conn *websocket.Conn, phoneNumbers []string) {
	for _, number := range phoneNumbers {
		connMap, _ := s.conns.Load(number)
		if connMap != nil {
			connMap.Delete(conn)
		}
	}
}

func (s *serverService) SendMessage(ctx context.Context, msg *twismsproto.Message) error {
	msg = proto.Clone(msg).(*twismsproto.Message)
	if msg.Timestamp == nil {
		msg.Timestamp = timestamppb.Now()
	}

	if err := s.queue.StoreMessage(ctx, msg); err != nil {
		return fmt.Errorf("could not store message in queue: %w", err)
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// metrics for future use
	var (
		clients atomic.Uint64
		sent    atomic.Uint64
		acked   atomic.Uint64
	)

	connMap, _ := s.conns.Load(msg.To)
	connMap.Range(func(conn *websocket.Conn, _ struct{}) bool {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clients.Add(1)

			wsMsg := &wsbridgeproto.Message{
				Message: msg,
			}

			var ackCh <-chan struct{}
			if s.acks != nil {
				var ackID string
				ackID, ackCh = s.acks.generate()
				wsMsg.AcknowledgementId = &ackID
			}

			if err := SendMessage(ctx, conn, wsMsg); err != nil {
				s.logger.Info(
					"could not deliver message to wsbridge client",
					"to", msg.To,
					"err", err)
				return
			}

			sent.Add(1)

			if ackCh != nil {
				if err := s.acks.wait(ctx, ackCh); err != nil {
					s.logger.Info(
						"timed out waiting for message acknowledgement",
						"to", msg.To,
						"err", err)
					return
				}
				acked.Add(1)
			}
		}()
		return true
	})

	s.logger.Info(
		"sent message to wsbridge clients",
		"to", msg.To,
		"clients", clients.Load(),
		"sent", sent.Load(),
		"acked", acked.Load())

	return nil
}
