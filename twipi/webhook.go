package twipi

import (
	"context"
	"io"
	"net/http"

	"github.com/diamondburned/listener"
	"github.com/go-chi/chi/v5"
)

// WebhookRegisterer is a type that can register a webhook handler into a
// router.
type WebhookRegisterer interface {
	Mount(r chi.Router)
	io.Closer
}

// WebhookRouter is a router that can register a webhook handler.
type WebhookRouter struct {
	chi.Mux
	closers []io.Closer
}

// NewWebhookRouter creates a new WebhookRouter.
func NewWebhookRouter() *WebhookRouter {
	return &WebhookRouter{Mux: *chi.NewMux()}
}

// RegisterWebhook registers a webhook handler into the server.
func (r *WebhookRouter) RegisterWebhook(registerer WebhookRegisterer) {
	r.Mux.Group(registerer.Mount)
	r.closers = append(r.closers, registerer)
}

// Close closes all the registered webhook registers.
func (r *WebhookRouter) Close() error {
	for _, closer := range r.closers {
		closer.Close()
	}
	return nil
}

// WebhookServer composes an HTTP server that handles various Twilio callbacks
// and dispatches.
type WebhookServer struct {
	s *http.Server
	r *WebhookRouter
}

// NewWebhookServer creates a new WebhookServer.
func NewWebhookServer(listenAddr string) *WebhookServer {
	r := NewWebhookRouter()
	return &WebhookServer{
		s: &http.Server{
			Addr:    listenAddr,
			Handler: r,
		},
		r: r,
	}
}

// RegisterWebhook registers a webhook handler into the server.
func (s *WebhookServer) RegisterWebhook(registerer WebhookRegisterer) {
	s.r.RegisterWebhook(registerer)
}

// Close closes all the registered webhook registers.
func (s *WebhookServer) Close() error {
	return s.r.Close()
}

// ListenAndServe listens and serves the server.
func (s *WebhookServer) ListenAndServe(ctx context.Context) error {
	err := listener.HTTPListenAndServeCtx(ctx, s.s)
	s.r.Close()
	return err
}
