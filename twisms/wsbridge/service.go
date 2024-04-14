// Package wsbridge implements Twisms via a Websocket Protobuf RPC bridge.
// This is used for proxying and testing.
package wsbridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/twipi/twipi/proto/out/wsbridgeproto"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"nhooyr.io/websocket"
)

// SendMessage sends a message to the wsbridge Websocket connection in Protobuf
// format. It sends the WebsocketPacket.send frame.
func SendMessage(ctx context.Context, conn *websocket.Conn, msg *wsbridgeproto.Message) error {
	return sendPacket(ctx, conn, &wsbridgeproto.WebsocketPacket{
		Body: &wsbridgeproto.WebsocketPacket_Message{
			Message: msg,
		},
	})
}

func sendMessageAcknowledgement(ctx context.Context, conn *websocket.Conn, acknowledgement *wsbridgeproto.MessageAcknowledgement) error {
	return sendPacket(ctx, conn, &wsbridgeproto.WebsocketPacket{
		Body: &wsbridgeproto.WebsocketPacket_MessageAcknowledgement{
			MessageAcknowledgement: acknowledgement,
		},
	})
}

func sendError(ctx context.Context, conn *websocket.Conn, message string) error {
	return sendPacket(ctx, conn, &wsbridgeproto.WebsocketPacket{
		Body: &wsbridgeproto.WebsocketPacket_Error{
			Error: &wsbridgeproto.Error{
				Message: message,
			},
		},
	})
}

func sendPacket(ctx context.Context, conn *websocket.Conn, packet *wsbridgeproto.WebsocketPacket) error {
	b, err := proto.Marshal(packet)
	if err != nil {
		return fmt.Errorf("could not marshal message as protojson: %w", err)
	}
	return conn.Write(ctx, websocket.MessageBinary, b)
}

func handleWS(ctx context.Context, conn *websocket.Conn, logger *slog.Logger, onEvent func(*wsbridgeproto.WebsocketPacket) error) {
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
		case websocket.MessageBinary:
			if err := proto.Unmarshal(b, msg); err != nil {
				closeErr = fmt.Errorf("could not unmarshal message: %w", err)
				break readLoop
			}
		default:
			closeErr = fmt.Errorf("unexpected message type: %v", msgType)
			break readLoop
		}

		if logger.Enabled(ctx, slog.LevelDebug) {
			logger.Debug(
				"wsbridge received message",
				"message", msg.String())
		}

		switch body := msg.Body.(type) {
		case *wsbridgeproto.WebsocketPacket_Error:
			logger.Warn(
				"received error message from server",
				"message", body.Error.Message)

		default:
			if closeErr = onEvent(msg); closeErr != nil {
				break readLoop
			}
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
		logger.Error(
			"could not close connection",
			"err", err,
			"code", closeCode,
			"reason", closeReason)
	}
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

type messageAcks struct {
	acks    *xsync.MapOf[string, chan struct{}]
	ackID   atomic.Uint64
	timeout time.Duration
}

func newMessageAcks(timeout time.Duration) *messageAcks {
	if timeout == 0 {
		return nil
	}
	return &messageAcks{
		acks:    xsync.NewMapOf[string, chan struct{}](),
		timeout: timeout,
	}
}

func (s *messageAcks) generate() (string, <-chan struct{}) {
	a := s.ackID.Add(1)
	aID := fmt.Sprintf("ack-%d", a)
	aCh := make(chan struct{})
	s.acks.Store(aID, aCh)
	return aID, aCh
}

func (s *messageAcks) wait(ctx context.Context, ch <-chan struct{}) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

func (s *messageAcks) cancel(aID string) {
	s.acks.Delete(aID)
}

func (s *messageAcks) acknowledge(aID string) bool {
	ch, ok := s.acks.LoadAndDelete(aID)
	if !ok {
		return false
	}
	close(ch)
	return true
}
