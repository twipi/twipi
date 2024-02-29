package slogctx

import (
	"context"
	"log/slog"

	"libdb.so/ctxt"
)

// From returns a slog.Logger from the context. If no logger is found, the
// default logger is returned.
func From(ctx context.Context) *slog.Logger {
	logger, ok := ctxt.From[*slog.Logger](ctx)
	if ok {
		return logger
	}
	return slog.Default()
}
