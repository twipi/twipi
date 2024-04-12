// Package wsbridge implements Twisms via a Websocket Protobuf RPC bridge.
// This is used for proxying and testing.
package wsbridge

import (
	"context"
	"fmt"

	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/proto/out/wsbridgeproto"
	"google.golang.org/protobuf/encoding/protojson"
	"nhooyr.io/websocket"
)

// SendMessage sends a message to the wsbridge Websocket connection in Protobuf
// format. It sends the WebsocketPacket.send frame.
func SendMessage(ctx context.Context, conn *websocket.Conn, msg *twismsproto.Message) error {
	b, err := protojson.Marshal(&wsbridgeproto.WebsocketPacket{
		Body: &wsbridgeproto.WebsocketPacket_Send{
			Send: &wsbridgeproto.SendMessage{
				Message: msg,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("could not marshal message as protojson: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, b)
}
