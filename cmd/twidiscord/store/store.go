package store

import (
	"context"
	"io"
	"log"
	"net/url"

	"github.com/pkg/errors"
)

//go:generate sqlc generate

var ErrNotFound = errors.New("not found")

// Open opens a new Storer. The returned Storer must be closed after use.
//
// The following schemes are supported:
//
// 	- sqlite
//
func Open(ctx context.Context, urlStr string, ro bool) (any, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse database URL")
	}

	switch u.Scheme {
	case "sqlite":
		u.Scheme = "file"
		return OpenSQLite(ctx, u.String(), ro)
	default:
		return nil, errors.Errorf("unknown database scheme %q", u.Scheme)
	}
}

// Close closes v if it implements io.Closer.
func Close(v any) {
	if c, ok := v.(io.Closer); ok {
		if err := c.Close(); err != nil {
			log.Println("twidiscord: store: failed to close:", err)
		}
	}
}
