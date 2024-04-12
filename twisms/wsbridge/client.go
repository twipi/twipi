package wsbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twipi/twipi/internal/pubsub"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/proto/out/wsbridgeproto"
	"github.com/twipi/twipi/twid"
	"github.com/twipi/twipi/twisms"
	"google.golang.org/protobuf/encoding/protojson"
	"nhooyr.io/websocket"
)

func init() {
	twid.RegisterTwismsModule(twid.TwismsModule{
		Name: "wsbridge",
		Desc: "Proxy message sends and receives over a Websocket",
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
}

// ClientService wraps a Websocket connection to a wsbridge service.
type ClientService struct {
	conn   atomic.Pointer[websocket.Conn]
	subs   pubsub.Subscriber[*twismsproto.Message]
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
		logger: logger,
		cfg:    cfg,
	}
}

// Start starts the service.
func (s *ClientService) Start(ctx context.Context) error {
	const retryBackoff = 2 * time.Second
	const maxBackoff = 30 * time.Second
	retries := 0

	var wg sync.WaitGroup
	defer wg.Wait()

	incomingMsgs := make(chan *twismsproto.Message)

	wg.Add(1)
	go func() {
		s.subs.Listen(ctx, incomingMsgs)
		wg.Done()
	}()

	for ctx.Err() == nil {
		s.logger.Info("reconnecting wsbridge service connection")

		conn, _, err := websocket.Dial(ctx, s.cfg.WSAddress, &websocket.DialOptions{
			HTTPHeader: s.cfg.Headers,
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			retries++
			backoff := time.Duration(retries) * retryBackoff

			s.logger.Error(
				"could not dial wsbridge service, retrying",
				"err", err,
				"retries", retries,
				"backoff", backoff)

			if err := sleep(ctx, backoff); err != nil {
				return err
			}
			continue
		}

		// Replace the current connection.
		s.conn.Store(conn)

		s.logger.Info("connected to wsbridge service")

		var closeErr error
	readLoop:
		for {
			msgType, b, err := conn.Read(ctx)
			if err != nil {
				closeErr = fmt.Errorf("could not read message: %w", err)
				break
			}

			msg := &wsbridgeproto.WebsocketPacket{}
			switch msgType {
			case websocket.MessageText:
				if err := protojson.Unmarshal(b, msg); err != nil {
					closeErr = fmt.Errorf("could not unmarshal message: %w", err)
					break readLoop
				}
			default:
				closeErr = fmt.Errorf("unexpected message type: %v", msgType)
				break readLoop
			}

			switch body := msg.Body.(type) {
			case *wsbridgeproto.WebsocketPacket_Error:
				s.logger.Warn(
					"received error message from server",
					"message", body.Error.Message)

			case *wsbridgeproto.WebsocketPacket_Incoming:
				select {
				case <-ctx.Done():
					closeErr = fmt.Errorf("server going away")
					break readLoop
				case incomingMsgs <- body.Incoming.Message:
				}

			default:
				closeErr = fmt.Errorf("unexpected message body: %T", body)
				break readLoop
			}
		}

		var closeCode websocket.StatusCode
		var closeReason string
		switch {
		case errors.Is(closeErr, io.EOF):
			closeCode = websocket.StatusNormalClosure
		case errors.Is(closeErr, ctx.Err()):
			closeCode = websocket.StatusGoingAway
			closeReason = "context cancelled"
		default:
			closeCode = websocket.StatusProtocolError
			if closeErr != nil {
				closeReason = closeErr.Error()
			}
		}

		if err := conn.Close(closeCode, closeReason); err != nil {
			s.logger.Error(
				"could not close connection",
				"err", err,
				"code", closeCode,
				"reason", closeReason)
		}
	}

	return ctx.Err()
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendMessage implements [twisms.MessageSender].
func (s *ClientService) SendMessage(ctx context.Context, msg *twismsproto.Message) error {
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
func (s *ClientService) SubscribeMessages(ch chan<- *twismsproto.Message, filters *twismsproto.MessageFilters) {
	s.subs.Subscribe(ch, func(msg *twismsproto.Message) bool {
		return twisms.FilterMessage(filters, msg)
	})
}

// UnsubscribeMessages implements [twisms.MessageSubscriber].
func (s *ClientService) UnsubscribeMessages(ch chan<- *twismsproto.Message) {
	s.subs.Unsubscribe(ch)
}
