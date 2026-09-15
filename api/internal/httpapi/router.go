package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Options holds the dependencies needed to build the router.
type Options struct {
	Logger  *slog.Logger
	AppName string
	Version string
}

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// NewRouter builds the API's root HTTP handler.
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "resource not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	// Liveness: the process is up and serving HTTP. Never touches dependencies.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, HealthResponse{Status: "ok", Service: opts.AppName, Version: opts.Version})
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Domain routes are mounted here as each phase lands.
	})

	return r
}
