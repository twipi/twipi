package logger

import (
	"context"
	"io"
	"log"
	"os"
)

type ctxKey int

const (
	_ ctxKey = iota
	loggerCtx
)

// Logger is a wrapper around the stdlib Logger.
type Logger struct {
	*log.Logger
}

var (
	silentLogger  = New(io.Discard, 0)
	defaultLogger = New(os.Stderr, log.LstdFlags|log.Lmsgprefix)
)

// Default returns the default logger.
func Default() *Logger {
	return defaultLogger
}

// Silent returns a no-op logger.
func Silent() *Logger {
	return silentLogger
}

// New creates a new Logger with the given writer and flags.
func New(w io.Writer, flags int) *Logger {
	return NewWithPrefix(w, "", flags)
}

// NewWithPrefix creates a new Logger with the given prefix appended.
func NewWithPrefix(w io.Writer, prefix string, flags int) *Logger {
	return &Logger{log.New(w, prefix, flags)}
}

func (l *Logger) WithPrefix(prefix string) *Logger {
	prefix += ": "
	if oldPrefix := log.Prefix(); oldPrefix != "" {
		prefix = oldPrefix + ": " + prefix
	}
	return NewWithPrefix(l.Writer(), prefix, l.Flags())
}

// FromContext returns the Logger instance from the ctx, or log.Default() if
// none.
func FromContext(ctx context.Context, prefixes ...string) *Logger {
	logger, ok := ctx.Value(loggerCtx).(*Logger)
	if !ok {
		logger = Default()
	}

	for _, prefix := range prefixes {
		logger = logger.WithPrefix(prefix)
	}

	return logger
}

// WithLogger injects an additional logger into ctx.
func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerCtx, logger)
}

// WithLogPrefix returns a new context with a Logger that has the given prefix
// using LoggerWithPrefix.
func WithLogPrefix(ctx context.Context, prefix string) context.Context {
	logger := FromContext(ctx).WithPrefix(prefix)
	return WithLogger(ctx, logger)
}

// WithSilent returns a context.Context with a no-op logger.
func WithSilent(ctx context.Context) context.Context {
	return WithLogger(ctx, silentLogger)
}
