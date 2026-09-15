package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/requestid"
)

// RequestIDHeader carries the request correlation ID in both directions.
const RequestIDHeader = "X-Request-ID"

// Incoming IDs (e.g. from Heroku's router) are reused only if they are
// well-formed, so arbitrary client input never reaches the logs.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)

// RequestID assigns every request an ID, echoes it in the response, and stores
// it in the request context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
	})
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// RequestLogger logs one structured line per request: method, route pattern,
// status, bytes, and latency. It logs the matched route pattern, never the raw
// path or query string, which may contain tokens (e.g. invitation links).
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			route := "unmatched"
			if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
				route = rctx.RoutePattern()
			}

			level := slog.LevelInfo
			switch {
			case status >= http.StatusInternalServerError:
				level = slog.LevelError
			case route == "/health" || route == "/ready":
				level = slog.LevelDebug // probes are frequent; keep them out of info logs
			}

			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Float64("latencyMs", float64(time.Since(start).Microseconds())/1000),
			)
		})
	}
}

// Recoverer converts a handler panic into a logged 500 JSON error.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel comparison is the documented contract
					panic(rec)
				}
				logger.ErrorContext(r.Context(), "panic recovered",
					"panic", fmt.Sprint(rec),
					"stack", string(debug.Stack()),
				)
				httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBodyBytes limits how much of a request body handlers can read.
func MaxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
