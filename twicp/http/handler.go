package http

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/twipi/twipi/proto/out/twicppb"
	"github.com/twipi/twipi/twicp"
	"libdb.so/hrt"
	"libdb.so/hrtproto"
)

// Handler wraps an existing [twicp.OptionController] and provides an HTTP
// API handler for it.
type Handler struct {
	router     chi.Router
	controller twicp.OptionController
}

var _ http.Handler = (*Handler)(nil)

// NewHandler creates a new [Handler].
func NewHandler(controller twicp.OptionController) *Handler {
	h := &Handler{
		router:     chi.NewRouter(),
		controller: controller,
	}

	r := h.router
	r.Use(hrt.Use(hrt.Opts{
		Encoder:     hrtproto.ProtoJSONEncoder,
		ErrorWriter: hrt.TextErrorWriter,
	}))

	r.Get("/schema", hrt.Wrap(h.handleSchema))
	r.Get("/", hrt.Wrap(h.handleValues))
	r.Patch("/", hrt.Wrap(h.handleApplyValues))

	return h
}

// ServeHTTP implements the [http.Handler] interface.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *Handler) handleSchema(ctx context.Context, _ hrt.None) (*twicppb.Schema, error) {
	return h.controller.Schema(ctx)
}

func (h *Handler) handleValues(ctx context.Context, _ hrt.None) (*twicppb.OptionValues, error) {
	return h.controller.Values(ctx)
}

func (h *Handler) handleApplyValues(ctx context.Context, req *twicppb.ApplyRequest) (*twicppb.ApplyResponse, error) {
	return h.controller.ApplyValues(ctx, req)
}
