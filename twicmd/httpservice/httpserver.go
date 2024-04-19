package httpservice

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

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
	service twicmd.Service
	router  *chi.Mux
	logger  *slog.Logger

	msgQueue       []*twismsproto.Message
	msgQueueMutex  sync.Mutex
	msgQueueSignal chan struct{}
}

// NewHTTPServer creates a new HTTP server with the given service.
func NewHTTPServer(service twicmd.Service, logger *slog.Logger) *HTTPServer {
	s := &HTTPServer{
		router:         chi.NewRouter(),
		service:        service,
		msgQueueSignal: make(chan struct{}, 1),
	}
	s.router.Use(hrt.Use(hrt.Opts{
		Encoder:     hrtproto.ProtoJSONEncoder,
		ErrorWriter: hrt.TextErrorWriter,
	}))
	s.router.Get("/", hrt.Wrap(s.getService))
	s.router.Get("/messages", s.sseMessages)
	s.router.Post("/execute", hrt.Wrap(s.execute))
	return s
}

// EnqueueMessage enqueues a message to be processed by the server.
// The message will be processed eventually as the server connects to the SSE
// endpoint.
func (s *HTTPServer) EnqueueMessage(msg *twismsproto.Message) {
	s.msgQueueMutex.Lock()
	s.msgQueue = append(s.msgQueue, msg)
	s.msgQueueMutex.Unlock()

	select {
	case s.msgQueueSignal <- struct{}{}:
	default:
	}

	s.logger.Debug(
		"signaled message queue, awaiting processing",
		"message.from", msg.From,
		"message.to", msg.To,
		"batch_size", len(s.msgQueue))
}

func (s *HTTPServer) getService(ctx context.Context, _ hrt.None) (*twicmdproto.Service, error) {
	service, err := s.service.Service(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}
	return service, nil
}

func (s *HTTPServer) execute(ctx context.Context, req *twicmdproto.ExecuteCommandRequest) (*twicmdproto.ExecuteCommandResponse, error) {
	body, err := s.service.Execute(ctx, req.Message, req.Command)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}
	return &twicmdproto.ExecuteCommandResponse{
		Body: body,
	}, nil
}

func (s *HTTPServer) sseMessages(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Flusher); !ok {
		// TODO: consider long polling as a fallback.
		http.Error(w, "streaming not supported", http.StatusNotImplemented)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	for {
		select {
		case <-r.Context().Done():
			writeSSE(w, "error", "client disconnected")
			return
		case <-s.msgQueueSignal:
		}

		s.msgQueueMutex.Lock()
		batch := s.msgQueue
		s.msgQueue = nil
		s.msgQueueMutex.Unlock()

		for _, msg := range batch {
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
