package srvutil

import (
	"fmt"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"libdb.so/hrt"
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

type protoJSONURLDecoder struct {
	urlParam string
}

// ProtoJSONURLDecoder returns a decoder that decodes a ProtoJSON blob from the
// URL parameter with the given name.
func ProtoJSONURLDecoder(urlParam string) hrt.Decoder {
	return protoJSONURLDecoder{urlParam}
}

func (d protoJSONURLDecoder) Decode(r *http.Request, v any) error {
	msg, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("protoJSONURLDecoder: Decode: %T is not a proto.Message", v)
	}

	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("failed to parse form: %w", err)
	}

	data := r.FormValue(d.urlParam)
	if err := protojson.Unmarshal([]byte(data), msg); err != nil {
		return fmt.Errorf("protoJSONURLDecoder: Decode: %w", err)
	}

	return nil
}
