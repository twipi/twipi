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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twicmd"
	"google.golang.org/protobuf/proto"
	"libdb.so/hrtproto"
)

// HTTPService wraps an HTTP API and implements [twicmd.Service].
type HTTPService struct {
	client *http.Client
	url    string
	name   string
}

var _ twicmd.Service = (*HTTPService)(nil)

// NewHTTPService creates a new HTTP service with the given URL.
func NewHTTPService(serviceName, url string) *HTTPService {
	return &HTTPService{
		client: http.DefaultClient,
		url:    url,
		name:   serviceName,
	}
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
func (s *HTTPService) Execute(ctx context.Context, cmd *twicmdproto.Command) (*twismsproto.MessageBody, error) {
	messageBody := new(twismsproto.MessageBody)
	if err := s.do(ctx, "POST", "/execute", cmd, messageBody); err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}
	return messageBody, nil
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
