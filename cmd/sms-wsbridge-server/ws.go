package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/twipi/twipi/proto/out/wsbridgeproto"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"nhooyr.io/websocket"
)

type wsHandler struct {
	logger *slog.Logger
	serial atomic.Uint64
}

func newWSHandler(logger *slog.Logger) http.Handler {
	return &wsHandler{logger: logger}
}

func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		h.logger.Error(
			"failed to accept websocket connection",
			"err", err)
		return
	}
	defer conn.CloseNow()

	wsID := h.serial.Add(1)

	logger := h.logger.With("ws_id", wsID)
	logger.Info("accepted new websocket connection")

	var closeErr error
	var closeCode websocket.StatusCode
readLoop:
	for {
		msgType, b, err := conn.Read(r.Context())
		if err != nil {
			closeErr = fmt.Errorf("failed to read message: %w", err)
			closeCode = websocket.StatusInternalError
			break readLoop
		}

		msg := &wsbridgeproto.WebsocketPacket{}
		switch msgType {
		case websocket.MessageText:
			if err := protojson.Unmarshal(b, msg); err != nil {
				closeErr = fmt.Errorf("failed to unmarshal protojson message: %w", err)
				closeCode = websocket.StatusUnsupportedData
				break readLoop
			}
		case websocket.MessageBinary:
			if err := proto.Unmarshal(b, msg); err != nil {
				closeErr = fmt.Errorf("failed to unmarshal protobuf message: %w", err)
				closeCode = websocket.StatusUnsupportedData
				break readLoop
			}
		default:
			closeErr = fmt.Errorf("unexpected message type: %v", msgType)
			closeCode = websocket.StatusUnsupportedData
			break readLoop
		}

		handled, err := h.handleMsg(r.Context(), logger, msg)
		if err != nil {
			closeErr = err
			closeCode = websocket.StatusInternalError
			break readLoop
		}

		if !handled {
			closeErr = fmt.Errorf("unexpected message type: %T", msg.Body)
			closeCode = websocket.StatusUnsupportedData
			break readLoop
		}
	}

	var closeReason string
	if closeErr != nil && !errors.Is(closeErr, io.EOF) {
		closeReason = err.Error()
	}

	if err := conn.Close(closeCode, closeReason); err != nil {
		logger.Error(
			"failed to close websocket connection gracefully",
			"err", err)
	}
}

func (h *wsHandler) handleMsg(ctx context.Context, logger *slog.Logger, msg *wsbridgeproto.WebsocketPacket) (bool, error) {
	switch body := msg.Body.(type) {
	case *wsbridgeproto.WebsocketPacket_Error:
		logger.WarnContext(ctx,
			"received error message from client",
			"err", body.Error.Message)

		return true, nil

	case *wsbridgeproto.WebsocketPacket_Incoming:
		msg := body.Incoming.Message
		logger.Log(ctx, slog.LevelInfo+1,
			"received incoming message from client",
			"from", msg.From,
			"to", msg.To,
			"body", prototext.Format(msg.Body))

		return true, nil
	}

	return false, nil
}
