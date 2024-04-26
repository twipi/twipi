package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/twipi/twipi/internal/srvutil"
	"github.com/twipi/twipi/proto/out/twicmdcfgpb"
	"github.com/twipi/twipi/proto/out/twidpb"
	"github.com/twipi/twipi/twicmd"
	"github.com/twipi/twipi/twisms"
	"libdb.so/ctxt"
	"libdb.so/hrt"
	"libdb.so/hrtproto"
)

var errInternal = hrt.NewHTTPError(http.StatusInternalServerError, "internal error")

var hrtOpts = hrt.Opts{
	Encoder: hrt.CombinedEncoder{
		Encoder: hrtproto.ProtoJSONEncoder,
		Decoder: hrt.MethodDecoder{
			"GET": srvutil.ProtoJSONURLDecoder("params"),
			"*":   hrtproto.ProtoJSONEncoder,
		},
	},
	ErrorWriter: hrt.TextErrorWriter,
}

func writeError(w http.ResponseWriter, err error) {
	hrtOpts.ErrorWriter.WriteError(w, err)
}

// New returns an HTTP handler that serves the main API.
func New(sms twisms.MessageSender, cmd *twicmd.Manager, logger *slog.Logger) http.Handler {
	h := &handler{
		sms:    sms,
		cmd:    cmd,
		auth:   newAuthHandler(sms, logger),
		logger: logger,
	}

	r := chi.NewMux()
	r.Use(cors.AllowAll().Handler)
	r.Use(middleware.CleanPath)
	r.Use(hrt.Use(hrtOpts))

	r.Route("/login", func(r chi.Router) {
		r.Post("/phase1", hrt.Wrap(h.auth.loginPhase1))
		r.Post("/phase2", hrt.Wrap(h.auth.loginPhase2))
	})

	r.Get("/", hrt.Wrap(h.listServices))

	r.Route("/{name}", func(r chi.Router) {
		r.Get("/", hrt.Wrap(h.getService))

		r.Route("/cp", func(r chi.Router) {
			r.Use(h.auth.sessionMiddleware)
			r.Get("/", hrt.Wrap(h.getControlPanel))
		})
	})

	return r
}

type handler struct {
	sms    twisms.MessageSender
	cmd    *twicmd.Manager
	auth   *authHandler
	logger *slog.Logger
}

func (h *handler) listServices(ctx context.Context, req *twidpb.ListServicesRequest) (*twidpb.ListServicesResponse, error) {
	var services []*twidpb.ServiceListItem
	var err error

	iter := h.cmd.Services.AllServices(ctx)
	iter(func(service *twicmd.ResolvedService, e error) bool {
		if e != nil {
			err = e
			return false
		}

		services = append(services, &twidpb.ServiceListItem{
			Name:        service.Description.Name,
			Description: service.Description.Description,
			IconUrl:     service.Description.IconUrl,
			Color:       service.Description.Color,
		})
		return true
	})
	if err != nil {
		return nil, err
	}

	return &twidpb.ListServicesResponse{
		Services: services,
	}, nil
}

func (h *handler) getService(ctx context.Context, req *twidpb.GetServiceRequest) (*twidpb.GetServiceResponse, error) {
	serviceName := chi.URLParamFromCtx(ctx, "name")

	service, err := h.cmd.Services.Lookup(ctx, serviceName)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, hrt.NewHTTPError(http.StatusNotFound, "service not found")
	}

	return &twidpb.GetServiceResponse{
		Service: service.Description,
	}, nil
}

func (h *handler) getControlPanel(ctx context.Context, req *twidpb.GetControlPanelRequest) (*twidpb.GetControlPanelResponse, error) {
	serviceName := chi.URLParamFromCtx(ctx, "name")
	session, _ := ctxt.From[authSession](ctx)

	service, err := h.cmd.Services.Lookup(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	cp, ok := service.Service.(twicmd.ConfigurableService)
	if !ok {
		return nil, hrt.NewHTTPError(http.StatusNotAcceptable, "service does not support control panel")
	}

	options, err := cp.ConfigurationValues(ctx, &twicmdcfgpb.OptionsRequest{
		PhoneNumber: session.PhoneNumber,
	})
	if err != nil {
		return nil, err
	}

	return &twidpb.GetControlPanelResponse{
		Service: service.Description,
		Values:  options.Values,
	}, nil
}
