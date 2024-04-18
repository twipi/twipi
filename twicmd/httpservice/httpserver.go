package httpservice

import (
	"context"
	"fmt"

	"github.com/go-chi/chi/v5"
	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twicmd"
	"libdb.so/hrt"
	"libdb.so/hrtproto"
)

// HTTPServer wraps a [twicmd.Service] and turns it into an HTTP server.
type HTTPServer struct {
	router  *chi.Mux
	service twicmd.Service
}

// NewHTTPServer creates a new HTTP server with the given service.
func NewHTTPServer(service twicmd.Service) *HTTPServer {
	s := &HTTPServer{
		router:  chi.NewRouter(),
		service: service,
	}
	s.router.Use(hrt.Use(hrt.Opts{
		Encoder:     hrtproto.ProtobufEncoder,
		ErrorWriter: hrt.TextErrorWriter,
	}))
	s.router.Get("/", hrt.Wrap(s.getService))
	s.router.Post("/execute", hrt.Wrap(s.execute))
	return s
}

func (s *HTTPServer) getService(ctx context.Context, _ hrt.None) (*twicmdproto.Service, error) {
	service, err := s.service.Service(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}
	return service, nil
}

func (s *HTTPServer) execute(ctx context.Context, cmd *twicmdproto.Command) (*twismsproto.MessageBody, error) {
	msg, err := s.service.Execute(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}
	return msg, nil
}
