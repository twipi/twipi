package srvutil

import (
	"net/http"
)

// ParseForm is a middleware that calls ParseForm before the handler is called.
func ParseForm(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// TryHandlers returns a handler that tries each handler in order until one
// returns a non-404 status code.
func TryHandlers(handlers ...http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range handlers {
			hw := &headerWriter{w: w}
			h.ServeHTTP(hw, r)
			if !hw.notFound {
				return
			}
		}
		http.NotFound(w, r)
	})
}

type headerWriter struct {
	w        http.ResponseWriter
	notFound bool
}

func (h *headerWriter) Header() http.Header {
	return h.w.Header()
}

func (h *headerWriter) Write(b []byte) (int, error) {
	if h.notFound {
		// prevent writing a body for 404 responses
		return len(b), nil
	}
	return h.w.Write(b)
}

func (h *headerWriter) WriteHeader(status int) {
	if status == http.StatusNotFound {
		h.notFound = true
		return
	}
	h.w.WriteHeader(status)
}

// Respond200 writes a 200 OK response to the writer.
func Respond200(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
