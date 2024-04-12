// Package wsbridge implements Twisms via a Websocket Protobuf RPC bridge.
// This is used for proxying and testing.
package wsbridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/proto/out/wsbridgeproto"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"nhooyr.io/websocket"
)

// SendMessage sends a message to the wsbridge Websocket connection in Protobuf
// format. It sends the WebsocketPacket.send frame.
func SendMessage(ctx context.Context, conn *websocket.Conn, msg *twismsproto.Message) error {
	b, err := protojson.Marshal(&wsbridgeproto.WebsocketPacket{
		Body: &wsbridgeproto.WebsocketPacket_Message{
			Message: msg,
		},
	})
	if err != nil {
		return fmt.Errorf("could not marshal message as protojson: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, b)
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
