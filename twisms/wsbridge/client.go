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
	"time"

	"github.com/twipi/cfgutil"
	"github.com/twipi/pubsub"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/proto/out/wsbridgeproto"
	"github.com/twipi/twipi/twid"
	"github.com/twipi/twipi/twisms"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"nhooyr.io/websocket"
)

func init() {
	twid.RegisterTwismsModule(twid.TwismsModule{
		Name: "wsbridge_client",
		Desc: "Proxy message sends and receives over a Websocket client",
		New: func(raw json.RawMessage, logger *slog.Logger) (twisms.MessageService, error) {
			var cfg ClientServiceConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("failed to unmarshal config: %w", err)
			}
			return NewClientService(cfg, logger), nil
		},
	})
}

// ClientServiceConfig is the configuration for [ClientService].
type ClientServiceConfig struct {
	// PhoneNumbers is the list of phone numbers managed by this service.
	PhoneNumbers []string `json:"phone_numbers"`
	// WSAddress is the Websocket address of the wsbridge server.
	WSAddress string `json:"ws_address"`
	// Headers is the headers to send when connecting to the wsbridge server.
	// By default, this is empty.
	Headers http.Header `json:"headers"`
	// AcknowledgementTimeout is the timeout for message acknowledgements.
	// If 0, then no acknowledgement is required.
	AcknowledgementTimeout cfgutil.Duration `json:"acknowledgement_timeout"`
}

// ClientService wraps a Websocket connection to a wsbridge service.
type ClientService struct {
	conn   atomic.Pointer[websocket.Conn]
	subs   pubsub.Subscriber[*twismsproto.Message]
	acks   *messageAcks
	logger *slog.Logger
	cfg    ClientServiceConfig
}

var (
	_ twisms.MessageSender     = (*ClientService)(nil)
	_ twisms.MessageSubscriber = (*ClientService)(nil)
)

// NewClientService creates a new Service using the given Websocket address.
// Using this function, the Service acts as a Websocket client.
// The connection is not established until [Start] is called.
func NewClientService(cfg ClientServiceConfig, logger *slog.Logger) *ClientService {
	return &ClientService{
		acks:   newMessageAcks(cfg.AcknowledgementTimeout.AsDuration()),
		logger: logger,
		cfg:    cfg,
	}
}

// ClientStartOpts are options for starting the client service.
// All properties are optional.
type ClientStartOpts struct {
	// LastSeen is the last time the client was seen.
	// If not zero, the client may receive old messages from the server.
	LastSeen time.Time
}

// Start starts the service.
func (s *ClientService) Start(ctx context.Context, opts ClientStartOpts) error {
	const retryBackoff = 2 * time.Second
	const maxBackoff = 30 * time.Second
	retries := 0

	var wg sync.WaitGroup
	defer wg.Wait()

	incomingMsgs := make(chan *twismsproto.Message)
	lastSeen := opts.LastSeen

	wg.Add(1)
	go func() {
		s.subs.Listen(ctx, incomingMsgs)
		wg.Done()
	}()

	for ctx.Err() == nil {
		s.logger.Info(
			"reconnecting wsbridge service connection",
			"retries", retries)

		if retries > 0 {
			backoff := time.Duration(retries) * retryBackoff
			s.logger.Debug(
				"backing off wsbridge",
				"backoff", backoff)

			if err := sleep(ctx, backoff); err != nil {
				return err
			}
		}
		retries++

		conn, _, err := websocket.Dial(ctx, s.cfg.WSAddress, &websocket.DialOptions{
			HTTPHeader:      s.cfg.Headers,
			CompressionMode: websocket.CompressionContextTakeover,
		})
		if err != nil {
			s.logger.Error(
				"could not dial wsbridge service, retrying",
				"err", err)
			continue
		}

		var sincepb *timestamppb.Timestamp
		if !lastSeen.IsZero() {
			sincepb = timestamppb.New(lastSeen)
		}

		if err := sendPacket(ctx, conn, &wsbridgeproto.WebsocketPacket{
			Body: &wsbridgeproto.WebsocketPacket_Introduction{
				Introduction: &wsbridgeproto.Introduction{
					PhoneNumbers:   s.cfg.PhoneNumbers,
					Since:          sincepb,
					CanAcknowledge: true,
				},
			},
		}); err != nil {
			s.logger.Error(
				"could not send introduction to wsbridge service",
				"err", err)
			continue
		}

		// Replace the current connection.
		s.conn.Store(conn)

		// Reset the retries counter.
		retries = 0

		s.logger.Info("connected to wsbridge service")
		handleWS(ctx, conn, s.logger, func(msg *wsbridgeproto.WebsocketPacket) error {
			switch body := msg.Body.(type) {
			case *wsbridgeproto.WebsocketPacket_Message:
				message := body.Message.Message

				select {
				case <-ctx.Done():
					return ctx.Err()

				case incomingMsgs <- message:
					lastSeen = message.Timestamp.AsTime()

					if s.logger.Enabled(ctx, slog.LevelDebug) {
						s.logger.Debug(
							"broadcasted message",
							"message", message.String(),
							"sent_at", message.Timestamp.AsTime())
					}
				}

				if ackID := body.Message.AcknowledgementId; ackID != nil {
					s.logger.Debug(
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

		// Drop the connection.
		s.conn.Store(nil)
	}

	return ctx.Err()
}

// SendMessage implements [twisms.MessageSender].
func (s *ClientService) SendMessage(ctx context.Context, msg *twismsproto.Message) error {
	if !slices.Contains(s.cfg.PhoneNumbers, msg.From) {
		return fmt.Errorf("unknown phone number %q to send from", msg.From)
	}

	conn := s.conn.Load()
	if conn == nil {
		return fmt.Errorf("websocket connection not established")
	}

	msg = proto.Clone(msg).(*twismsproto.Message)
	if msg.Timestamp == nil {
		msg.Timestamp = timestamppb.Now()
	}

	if s.logger.Enabled(ctx, slog.LevelDebug) {
		s.logger.Debug(
			"wsbridge sending message",
			"message", msg.String())
	}

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
		return fmt.Errorf("could not send message: %w", err)
	}

	if ackCh != nil {
		if err := s.acks.wait(ctx, ackCh); err != nil {
			return fmt.Errorf("could not wait for message acknowledgement: %w", err)
		}
	}

	return nil
}

func (s *ClientService) SendingNumber() (string, float64) {
	// not round robin but just the first number
	return s.cfg.PhoneNumbers[0], 0.0
}

// SubscribeMessages implements [twisms.MessageSubscriber].
func (s *ClientService) SubscribeMessages(ch chan<- *twismsproto.Message, filters *twismsproto.MessageFilters) {
	s.subs.Subscribe(ch, func(msg *twismsproto.Message) bool {
		return twisms.FilterMessage(filters, msg)
	})
}

// UnsubscribeMessages implements [twisms.MessageSubscriber].
func (s *ClientService) UnsubscribeMessages(ch chan<- *twismsproto.Message) {
	s.subs.Unsubscribe(ch)
}
