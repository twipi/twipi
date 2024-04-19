package httpservice

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twicmd"
	"google.golang.org/protobuf/encoding/protojson"
	"libdb.so/hrt"
	"libdb.so/hrtproto"
)

// HTTPServer wraps a [twicmd.Service] and turns it into an HTTP server.
type HTTPServer struct {
	service  twicmd.Service
	router   *chi.Mux
	logger   *slog.Logger
	msgQueue chan *twismsproto.Message
}

var (
	_ http.Handler = (*HTTPServer)(nil)
	_ io.Closer    = (*HTTPServer)(nil)
)

// NewHTTPServer creates a new HTTP server with the given service.
func NewHTTPServer(service twicmd.Service, logger *slog.Logger) *HTTPServer {
	msgQueue := make(chan *twismsproto.Message)
	service.SubscribeMessages(msgQueue, nil)

	s := &HTTPServer{
		router:   chi.NewRouter(),
		service:  service,
		msgQueue: msgQueue,
	}

	r := s.router
	r.Use(hrt.Use(hrt.Opts{
		Encoder:     hrtproto.ProtoJSONEncoder,
		ErrorWriter: hrt.TextErrorWriter,
	}))
	r.Get("/", hrt.Wrap(s.getService))
	r.Get("/messages", s.sseMessages)
	r.Post("/execute", hrt.Wrap(s.execute))

	return s
}

// ServeHTTP implements the [http.Handler] interface.
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Close frees resources used by the server.
func (s *HTTPServer) Close() error {
	s.service.UnsubscribeMessages(s.msgQueue)
	return nil
}

func (s *HTTPServer) getService(ctx context.Context, _ hrt.None) (*twicmdproto.Service, error) {
	return s.service.Service(ctx)
}

func (s *HTTPServer) execute(ctx context.Context, req *twicmdproto.ExecuteRequest) (*twicmdproto.ExecuteResponse, error) {
	return s.service.Execute(ctx, req)
}

func (s *HTTPServer) sseMessages(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Flusher); !ok {
		// TODO: consider long polling as a fallback.
		http.Error(w, "streaming not supported", http.StatusNotImplemented)
		return
	}

	s.logger.Debug(
		"SSE message stream connected",
		"remote_addr", r.RemoteAddr)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	for {
		select {
		case <-r.Context().Done():
			writeSSE(w, "error", "client disconnected")

			s.logger.Debug(
				"SSE message stream disconnected",
				"remote_addr", r.RemoteAddr,
				"err", r.Context().Err())
			return

		case msg := <-s.msgQueue:
			b, err := protojson.Marshal(msg)
			if err != nil {
				s.logger.Error(
					"failed to marshal message, dropping it",
					"message.from", msg.From,
					"message.to", msg.To,
					"err", err)
				continue
			}
			writeSSE(w, "message", string(b))
		}
	}
}

func writeSSE(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", data)
	w.(http.Flusher).Flush()
}
