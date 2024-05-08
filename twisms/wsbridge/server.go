package wsbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/twipi/cfgutil"
	"github.com/twipi/pubsub"
	"github.com/twipi/twipi/internal/catchupstorage"
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

type (
	wsConnMap  = xsync.MapOf[*websocket.Conn, clientMetadata]
	wsPhoneMap = xsync.MapOf[string, *wsConnMap]
)

// ServerService wraps a Websocket HTTP handler for the wsbridge server.
type ServerService struct {
	subs    pubsub.Subscriber[*twismsproto.Message]
	msgs    chan *twismsproto.Message
	ready   chan struct{}
	service *xcontainer.Signaled[*serverService]
	conns   *wsPhoneMap // phone number -> conn
	logger  *slog.Logger
	cfg     ServerServiceConfig
}

type clientMetadata struct {
	phoneNumbers   []string
	canAcknowledge bool
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
		conns:   xsync.NewMapOf[string, *xsync.MapOf[*websocket.Conn, clientMetadata]](),
		logger:  logger,
		cfg:     cfg,
	}
}

// Start implements [twid.Starter].
func (s *ServerService) Start(ctx context.Context) error {
	if len(s.cfg.PhoneNumbers) < 1 {
		return errors.New("no phone numbers configured")
	}

	errg, ctx := errgroup.WithContext(ctx)

	errg.Go(func() error {
		return s.subs.Listen(ctx, s.msgs)
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

		return ss.Start(ctx)
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

func (s *ServerService) SendingNumber() (string, float64) {
	// not round robin but just the first number
	return s.cfg.PhoneNumbers[0], 0.0
}

type serverService struct {
	*ServerService
	queue *catchupstorage.MessageQueue
	acks  *messageAcks
}

func newServerService(ctx context.Context, h *ServerService) (*serverService, error) {
	var queue *catchupstorage.MessageQueue
	var err error

	if h.cfg.MessageQueue != nil {
		logger := h.logger.With(slog.Group(
			"wsbridge_server",
			"module", "message_queue"))

		queue, err = catchupstorage.NewMessageQueue(ctx, h.cfg.MessageQueue, logger)
		if err != nil {
			return nil, fmt.Errorf("could not create message queue: %w", err)
		}

		logger.Debug("using message queue")
	}

	return &serverService{
		ServerService: h,
		queue:         queue,
		acks:          newMessageAcks(h.cfg.AcknowledgementTimeout.AsDuration()),
	}, nil
}

func (s *serverService) Start(ctx context.Context) error {
	messages := make(chan *twismsproto.Message)

	s.subs.Subscribe(messages, nil)
	defer s.subs.Unsubscribe(messages)

eventLoop:
	for {
		select {
		case <-ctx.Done():
			break eventLoop
		case msg := <-messages:
			s.SendMessage(ctx, msg)
		}
	}

	s.logger.Info("stopped processing messages")

	if s.queue != nil {
		if err := s.queue.Close(); err != nil {
			return fmt.Errorf("could not close message queue: %w", err)
		}
	}

	return ctx.Err()
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

	var metadata clientMetadata
	defer func() { s.unregisterClient(conn, metadata) }()

	ctx := r.Context()
	handleWS(ctx, conn, logger, func(msg *wsbridgeproto.WebsocketPacket) error {
		switch body := msg.Body.(type) {
		case *wsbridgeproto.WebsocketPacket_Introduction:
			// Register the client for global use.
			metadata.phoneNumbers = body.Introduction.PhoneNumbers
			metadata.canAcknowledge = body.Introduction.CanAcknowledge
			s.registerClient(conn, metadata)

			// Bind the phone numbers to our logs.
			logger = logger.With("client_phone_numbers", metadata.phoneNumbers)

			if s.queue != nil && body.Introduction.Since != nil {
				logger.Debug(
					"catching client up to messages",
					"since", body.Introduction.Since.AsTime(),
					"since_unix", body.Introduction.Since.AsTime().Unix())

				var catchupErr error
				iter := s.queue.RetrieveMessages(ctx, body.Introduction.Since.AsTime(), metadata.phoneNumbers)
				iter(func(msg *twismsproto.Message, err error) bool {
					if err != nil {
						logger.Error(
							"could not retrieve all catchup message",
							"err", err)
						catchupErr = errors.New("could not retrieve all catchup messages")
						return false
					}

					err = SendMessage(ctx, conn, &wsbridgeproto.Message{
						Message: msg,
					})
					return err == nil
				})
				if catchupErr != nil {
					sendError(ctx, conn, catchupErr.Error())
				}
			}

			return nil

		case *wsbridgeproto.WebsocketPacket_Message:
			message := body.Message.Message

			// Overriding the message timestamp with the current time.
			message.Timestamp = timestamppb.Now()

			// Record the message.
			if s.queue != nil {
				if err := s.queue.StoreMessage(ctx, message); err != nil {
					return fmt.Errorf("could not store message in queue: %w", err)
				}
			}

			// TODO: filter out messages that aren't addressed to the server's
			// phone numbers.
			select {
			case <-ctx.Done():
				return ctx.Err()

			case s.msgs <- message:
				if logger.Enabled(ctx, slog.LevelDebug) {
					logger.Debug(
						"broadcasted message",
						"message", message.String(),
						"sent_at", message.Timestamp.AsTime())
				}
			}

			if ackID := body.Message.AcknowledgementId; ackID != nil {
				logger.Debug(
					"replying with message acknowledgement",
					"acknowledgement_id", *ackID)

				ack := &wsbridgeproto.MessageAcknowledgement{
					AcknowledgementId: *ackID,
					Timestamp:         message.Timestamp,
				}

				if err := sendMessageAcknowledgement(ctx, conn, ack); err != nil {
					return fmt.Errorf("could not send message acknowledgement: %w", err)
				}
			}

			return nil

		case *wsbridgeproto.WebsocketPacket_MessageAcknowledgement:
			if s.acks != nil {
				if !s.acks.acknowledge(body.MessageAcknowledgement.AcknowledgementId) {
					sendError(ctx, conn, "unknown acknowledgement ID")
				}
			}

			return nil

		default:
			return fmt.Errorf("unexpected message body: %T", body)
		}
	})
}

func (s *ServerService) registerClient(conn *websocket.Conn, metadata clientMetadata) {
	for _, number := range metadata.phoneNumbers {
		connMap, _ := s.conns.LoadOrCompute(number, func() *wsConnMap {
			return xsync.NewMapOfPresized[*websocket.Conn, clientMetadata](3)
		})
		connMap.Store(conn, metadata)
	}
}

func (s *ServerService) unregisterClient(conn *websocket.Conn, metadata clientMetadata) {
	for _, number := range metadata.phoneNumbers {
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

	if s.logger.Enabled(ctx, slog.LevelDebug) {
		s.logger.Debug(
			"wsbridge server broadcasting message",
			"message", msg.String())
	}

	if s.queue != nil {
		if err := s.queue.StoreMessage(ctx, msg); err != nil {
			return fmt.Errorf("could not store message in queue: %w", err)
		}
	}

	connMap, ok := s.conns.Load(msg.To)
	if !ok {
		return nil
	}

	s.broadcastToConns(ctx, msg, connMap)
	return nil
}

func (s *serverService) broadcastToConns(ctx context.Context, msg *twismsproto.Message, connMap *wsConnMap) {
	var wg sync.WaitGroup
	defer wg.Wait()

	var clients atomic.Uint64
	var delivered atomic.Uint64

	connMap.Range(func(conn *websocket.Conn, metadata clientMetadata) bool {
		clients.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

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

			delivered.Add(1)

			if ackCh != nil && metadata.canAcknowledge {
				if err := s.acks.wait(ctx, ackCh); err != nil {
					s.logger.Info(
						"timed out waiting for message acknowledgement",
						"to", msg.To,
						"err", err)
					return
				}
			}
		}()
		return true
	})

	wg.Wait()

	s.logger.Debug(
		"sent message to wsbridge clients",
		"to", msg.To,
		"clients", clients.Load(),
		"delivered", delivered.Load())
}
