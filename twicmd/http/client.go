// Package httpservice provides an HTTP service that accepts a [twicmd.Service]
// and provides it over HTTP as a Protobuf REST API.
//
// # API Specification
//
// For each service, the following endpoints are provided:
//   - GET / - returns the service description in Protobuf format.
//   - POST /execute - accepts a command in Protobuf format and returns the result
//     message body in Protobuf format.
package httpservice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/twipi/pubsub"
	"github.com/twipi/twipi/internal/xcontainer"
	"github.com/twipi/twipi/proto/out/twicmdcfgpb"
	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twicmd"
	"github.com/twipi/twipi/twid"
	"github.com/twipi/twipi/twisms"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protojson"
	"libdb.so/hrt"
	"libdb.so/hrtclient"
	"libdb.so/hrtproto/hrtclientproto"
)

// ClientConfig is the expected configuration for an HTTP service.
type ClientConfig struct {
	// Name is the name of the service.
	Name string `json:"name"`
	// BaseURL is the base BaseURL of the HTTP service.
	BaseURL string `json:"base_url"`
}

func init() {
	twid.RegisterTwicmdService(twid.TwicmdService{
		Name: "http",
		New: func(cfg json.RawMessage, logger *slog.Logger) (twicmd.Service, error) {
			var config ClientConfig
			if err := json.Unmarshal(cfg, &config); err != nil {
				return nil, fmt.Errorf("failed to unmarshal HTTP service config: %w", err)
			}
			return NewClient(config.Name, config.BaseURL, logger), nil
		},
	})
}

// Client wraps an HTTP API and implements [twicmd.Service].
type Client struct {
	httpClient *http.Client
	hrtClient  *hrtclient.Client
	logger     *slog.Logger
	subs       pubsub.Subscriber[*twismsproto.Message]
	msgs       chan *twismsproto.Message
	baseURL    string
	name       string

	cachedService xcontainer.Expirable[*twicmdproto.Service]
}

var (
	_ twicmd.Service           = (*Client)(nil)
	_ twisms.MessageSubscriber = (*Client)(nil)
	_ twid.Starter             = (*Client)(nil)
)

// NewClient creates a new HTTP service with the given URL.
func NewClient(serviceName, baseURL string, logger *slog.Logger) *Client {
	return &Client{
		httpClient: http.DefaultClient,
		hrtClient: hrtclient.NewClient(baseURL, hrtclient.CombinedCodec{
			Encoder: hrtclientproto.ProtoJSONCodec,
			Decoder: hrtclient.ErrorHandledDecoder{
				Success: hrtclientproto.ProtoJSONCodec,
				Error:   hrtclient.TextErrorDecoder,
			},
		}),
		logger: logger.With(
			"service", serviceName,
			"base_url", baseURL),
		msgs:    make(chan *twismsproto.Message),
		baseURL: baseURL,
		name:    serviceName,
	}
}

// Start implements [twid.Starter].
func (s *Client) Start(ctx context.Context) error {
	errg, ctx := errgroup.WithContext(ctx)

	errg.Go(func() error {
		return s.subs.Listen(ctx, s.msgs)
	})

	errg.Go(func() error {
		const retryDelay = 2 * time.Second
		const retryJitter = 1 * time.Second

		var delay time.Duration
		for {
			if err := s.runMessagesSSE(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				delay = retryDelay + time.Duration(rand.IntN(int(retryJitter)))
				s.logger.Error(
					"failed to start messages SSE, retrying...",
					"delay", delay,
					"err", err)
			}

			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	})

	return errg.Wait()
}

func (s *Client) runMessagesSSE(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/messages", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if body != nil {
			return fmt.Errorf(
				"unexpected status code %d: %s",
				resp.StatusCode, string(body))
		}
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		// TODO: consider long polling as a fallback.
		return fmt.Errorf("unexpected content type %q", resp.Header.Get("Content-Type"))
	}

	var event string

	s.logger.Debug(
		"connected to SSE stream for messages",
		"url", req.URL.String())

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		b := scanner.Bytes()

		switch {
		case len(b) == 0:
			event = ""
			continue

		case bytes.HasPrefix(b, []byte("event: ")):
			event = string(bytes.TrimPrefix(b, []byte("event: ")))
			continue

		case bytes.HasPrefix(b, []byte("data: ")):
			switch event {
			case "message":
				jsonData := bytes.TrimPrefix(b, []byte("data: "))
				s.logger.Debug(
					"received message over SSE",
					"json_data", string(jsonData),
					"event", "message")

				message := new(twismsproto.Message)
				if err := protojson.Unmarshal(jsonData, message); err != nil {
					s.logger.Error(
						"failed to unmarshal message, dropping it",
						"err", err)
					continue
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case s.msgs <- message:
					// ok
				}

			case "error":
				s.logger.Error(
					"received error message",
					"message", string(b))

			default:
				s.logger.Warn(
					"unexpected event in SSE stream",
					"event", event)
			}

		default:
			s.logger.Warn(
				"unexpected line in SSE stream",
				"line", string(b))
			return fmt.Errorf("unexpected line in SSE stream: %q", b)
		}
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read SSE stream: %w", err)
	}

	// The stream is never expected to end.
	return io.ErrUnexpectedEOF
}

// SubscribeMessages implements [twisms.MessageSubscriber].
func (s *Client) SubscribeMessages(ch chan<- *twismsproto.Message, filters *twismsproto.MessageFilters) {
	s.subs.Subscribe(ch, func(msg *twismsproto.Message) bool {
		return twisms.FilterMessage(filters, msg)
	})
}

// UnsubscribeMessages implements [twisms.MessageSubscriber].
func (s *Client) UnsubscribeMessages(ch chan<- *twismsproto.Message) {
	s.subs.Unsubscribe(ch)
}

// Name implements [twicmd.Service].
func (s *Client) Name() string {
	return s.name
}

var (
	routeService = hrtclient.GET[hrt.None, *twicmdproto.Service]("/")
	routeExecute = hrtclient.POST[*twicmdproto.ExecuteRequest, *twicmdproto.ExecuteResponse]("/execute")
)

// Service implements [twicmd.Service].
func (s *Client) Service(ctx context.Context) (*twicmdproto.Service, error) {
	return s.cachedService.RenewableValue(5*time.Minute, func() (*twicmdproto.Service, error) {
		return routeService(ctx, s.hrtClient, hrt.Empty)
	})
}

// Execute implements [twicmd.Service].
func (s *Client) Execute(ctx context.Context, req *twicmdproto.ExecuteRequest) (*twicmdproto.ExecuteResponse, error) {
	return routeExecute(ctx, s.hrtClient, req)
}

var (
	routeConfigurationValues      = hrtclient.GET[*twicmdcfgpb.OptionsRequest, *twicmdcfgpb.OptionsResponse]("/configuration")
	routeApplyConfigurationValues = hrtclient.PATCH[*twicmdcfgpb.ApplyRequest, *twicmdcfgpb.ApplyResponse]("/configuration")
)

func (s *Client) ConfigurationValues(ctx context.Context, req *twicmdcfgpb.OptionsRequest) (*twicmdcfgpb.OptionsResponse, error) {
	return routeConfigurationValues(ctx, s.hrtClient, req)
}

func (s *Client) ApplyConfigurationValues(ctx context.Context, req *twicmdcfgpb.ApplyRequest) (*twicmdcfgpb.ApplyResponse, error) {
	return routeApplyConfigurationValues(ctx, s.hrtClient, req)
}
