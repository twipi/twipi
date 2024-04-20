package http

import (
	"context"

	"github.com/twipi/twipi/proto/out/twicppb"
	"libdb.so/hrt"
	"libdb.so/hrtclient"
	"libdb.so/hrtproto/hrtclientproto"
)

type ClientConfig struct {
	BaseURL string `json:"base_url"`
}

// Client calls an existing httpcp.Handler API server and implements
// [twicp.OptionController].
type Client struct {
	client *hrtclient.Client
}

// NewClient creates a new [Client] with the given base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		client: hrtclient.NewClient(baseURL, hrtclient.CombinedCodec{
			Encoder: hrtclientproto.ProtoJSONCodec,
			Decoder: hrtclient.ErrorHandledDecoder{
				Success: hrtclientproto.ProtoJSONCodec,
				Error:   hrtclient.TextErrorDecoder,
			},
		}),
	}
}

var (
	endpointSchema      = hrtclient.GET[hrt.None, *twicppb.Schema]("/schema")
	endpointValues      = hrtclient.GET[hrt.None, *twicppb.OptionValues]("/")
	endpointApplyValues = hrtclient.PATCH[*twicppb.ApplyRequest, *twicppb.ApplyResponse]("/")
)

func (c *Client) Schema(ctx context.Context) (*twicppb.Schema, error) {
	return endpointSchema(ctx, c.client, hrt.Empty)
}

func (c *Client) Values(ctx context.Context) (*twicppb.OptionValues, error) {
	return endpointValues(ctx, c.client, hrt.Empty)
}

func (c *Client) ApplyValues(ctx context.Context, req *twicppb.ApplyRequest) (*twicppb.ApplyResponse, error) {
	return endpointApplyValues(ctx, c.client, req)
}
