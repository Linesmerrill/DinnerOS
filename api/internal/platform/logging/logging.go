// Package logging builds the API's structured slog logger.
//
// Log with the *Context methods (InfoContext, LogAttrs, ...) inside request
// handling so the request ID is attached automatically.
package logging

import (
	"context"
	"io"
	"log/slog"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/requestid"
)

// New returns a logger writing to w. format is "json" or "text".
func New(w io.Writer, level slog.Level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(contextHandler{handler})
}

// contextHandler adds request-scoped attributes found in the context.
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := requestid.From(ctx); id != "" {
		record.AddAttrs(slog.String("requestId", id))
	}
	return h.Handler.Handle(ctx, record)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
