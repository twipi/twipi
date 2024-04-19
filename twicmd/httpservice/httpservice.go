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

	"github.com/twipi/twipi/internal/pubsub"
	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twicmd"
	"github.com/twipi/twipi/twid"
	"github.com/twipi/twipi/twisms"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"libdb.so/hrtproto"
)

// HTTPServiceConfig is the expected configuration for an HTTP service.
type HTTPServiceConfig struct {
	// Name is the name of the service.
	Name string `json:"name"`
	// URL is the base URL of the HTTP service.
	URL string `json:"url"`
}

func init() {
	twid.RegisterTwicmdService(twid.TwicmdService{
		Name: "http",
		New: func(cfg json.RawMessage, logger *slog.Logger) (twicmd.Service, error) {
			var config HTTPServiceConfig
			if err := json.Unmarshal(cfg, &config); err != nil {
				return nil, fmt.Errorf("failed to unmarshal HTTP service config: %w", err)
			}
			return NewHTTPService(config.Name, config.URL, logger), nil
		},
	})
}

// HTTPService wraps an HTTP API and implements [twicmd.Service].
type HTTPService struct {
	client *http.Client
	logger *slog.Logger
	subs   pubsub.Subscriber[*twismsproto.Message]
	msgs   chan *twismsproto.Message
	url    string
	name   string
}

var (
	_ twicmd.Service           = (*HTTPService)(nil)
	_ twisms.MessageSubscriber = (*HTTPService)(nil)
	_ twid.Starter             = (*HTTPService)(nil)
)

// NewHTTPService creates a new HTTP service with the given URL.
func NewHTTPService(serviceName, url string, logger *slog.Logger) *HTTPService {
	return &HTTPService{
		client: http.DefaultClient,
		logger: logger,
		msgs:   make(chan *twismsproto.Message),
		url:    url,
		name:   serviceName,
	}
}

// Start implements [twid.Starter].
func (s *HTTPService) Start(ctx context.Context) error {
	errg, ctx := errgroup.WithContext(ctx)

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

func (s *HTTPService) runMessagesSSE(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.url+"/messages", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
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
func (s *HTTPService) SubscribeMessages(ch chan<- *twismsproto.Message, filters *twismsproto.MessageFilters) {
	s.subs.Subscribe(ch, func(msg *twismsproto.Message) bool {
		return twisms.FilterMessage(filters, msg)
	})
}

// UnsubscribeMessages implements [twisms.MessageSubscriber].
func (s *HTTPService) UnsubscribeMessages(ch chan<- *twismsproto.Message) {
	s.subs.Unsubscribe(ch)
}

// Name implements [twicmd.Service].
func (s *HTTPService) Name() string {
	return s.name
}

// Service implements [twicmd.Service].
func (s *HTTPService) Service(ctx context.Context) (*twicmdproto.Service, error) {
	service := new(twicmdproto.Service)
	if err := s.do(ctx, "GET", "/", nil, service); err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}
	return service, nil
}

// Execute implements [twicmd.Service].
func (s *HTTPService) Execute(ctx context.Context, req *twicmdproto.ExecuteRequest) (*twicmdproto.ExecuteResponse, error) {
	resp := new(twicmdproto.ExecuteResponse)
	if err := s.do(ctx, "POST", "/execute", req, resp); err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}
	return resp, nil
}

func (s *HTTPService) do(ctx context.Context, method, path string, body, dst proto.Message) error {
	var reqBody io.Reader
	if body != nil {
		b, err := proto.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body as Protobuf: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.url+path, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/protobuf")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/protobuf")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if len(respBody) > 0 {
			return fmt.Errorf(
				"unexpected status code %d: %s",
				resp.StatusCode, string(respBody))
		}
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	if dst != nil {
		protoMessage := resp.Header.Get(hrtproto.MessageNameHeader)
		if protoMessage != "" && protoMessage != string(proto.MessageName(dst)) {
			return fmt.Errorf(
				"unexpected response message type %q, wanted %q",
				protoMessage, proto.MessageName(dst))
		}

		if err := proto.Unmarshal(respBody, dst); err != nil {
			return fmt.Errorf("failed to unmarshal response as Protobuf: %w", err)
		}
	}

	return nil
}
