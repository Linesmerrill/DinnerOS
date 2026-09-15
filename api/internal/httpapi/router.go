// Package httpapi assembles the API's HTTP handler: middleware, operational
// endpoints, and the versioned route tree that domain handlers mount into.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

const (
	defaultMaxBodyBytes = 1 << 20
	readinessTimeout    = 2 * time.Second
)

// ReadinessCheck reports whether a dependency the API needs is available.
type ReadinessCheck struct {
	Name  string
	Check func(context.Context) error
}

// Options holds the dependencies needed to build the router.
type Options struct {
	Logger          *slog.Logger
	AppName         string
	Version         string
	MaxBodyBytes    int64
	ReadinessChecks []ReadinessCheck
	// APIRoutes mounts domain handlers on the /api/v1 router. The global
	// middleware stack (request ID, logging, recovery, body limit) already
	// applies; route-specific middleware (auth, rate limits) is added by the
	// domain handlers themselves.
	APIRoutes func(chi.Router)
	// WebRoutes mounts unversioned routes at the root, for browsers and Apple's
	// CDN rather than API clients: the invitation landing page and
	// /.well-known files. The global middleware stack applies.
	WebRoutes func(chi.Router)
}

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// ReadyResponse is returned by GET /ready. Check values are "ok" or
// "unavailable"; failure details are logged, never returned.
type ReadyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// NewRouter builds the API's root HTTP handler.
func NewRouter(opts Options) http.Handler {
	return newMux(opts)
}

func newMux(opts Options) *chi.Mux {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}

	r := chi.NewRouter()
	r.Use(
		RequestID,
		RequestLogger(logger),
		Recoverer(logger),
		MaxBodyBytes(maxBody),
	)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	// Liveness: the process is up and serving HTTP. Never touches dependencies.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, HealthResponse{Status: "ok", Service: opts.AppName, Version: opts.Version})
	})

	// Readiness: dependencies are reachable.
	r.Get("/ready", readyHandler(logger, opts.ReadinessChecks))

	if opts.WebRoutes != nil {
		opts.WebRoutes(r)
	}

	r.Route("/api/v1", func(api chi.Router) {
		if opts.APIRoutes != nil {
			opts.APIRoutes(api)
		}
	})

	return r
}

func readyHandler(logger *slog.Logger, checks []ReadinessCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := ReadyResponse{Status: "ready", Checks: make(map[string]string, len(checks))}
		status := http.StatusOK

		for _, check := range checks {
			ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
			err := check.Check(ctx)
			cancel()

			if err != nil {
				logger.WarnContext(r.Context(), "readiness check failed", "check", check.Name, "error", err)
				resp.Checks[check.Name] = "unavailable"
				resp.Status = "unavailable"
				status = http.StatusServiceUnavailable
				continue
			}
			resp.Checks[check.Name] = "ok"
		}

		httpx.WriteJSON(w, status, resp)
	}
}
