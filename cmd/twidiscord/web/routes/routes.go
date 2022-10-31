package routes

import (
	"net/http"

	"github.com/diamondburned/tmplutil"
	"github.com/diamondburned/twikit/cmd/twidiscord/twidiscord"
	"github.com/diamondburned/twikit/cmd/twidiscord/web"
	"github.com/diamondburned/twikit/twipi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Mount mounts the routes into the router.
func Mount(
	twipi *twipi.ConfiguredServer,
	cfg *twidiscord.Config,
	accountAdder AccountAdder) http.Handler {

	web.Templates.Preload()

	r := chi.NewMux()
	r.Use(middleware.CleanPath)
	r.Use(tmplutil.AlwaysFlush)
	r.Mount("/register", newRegisterHandler(twipi, cfg, accountAdder))

	return r
}

func renderError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	web.Templates.Subtemplate("error").Execute(w, err)
}
