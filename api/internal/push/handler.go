package push

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the device token HTTP handlers.
type HandlerOptions struct {
	Service *Service
	Tokens  auth.AccessTokenValidator
	Logger  *slog.Logger
}

// Handler serves the device token endpoints.
type Handler struct {
	opts   HandlerOptions
	logger *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(opts HandlerOptions) *Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{opts: opts, logger: logger}
}

// Mount registers the routes on r, the /api/v1 router. The token travels in
// the body rather than the path so it never appears in request logs.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Put("/me/device-tokens", h.register)
		r.Delete("/me/device-tokens", h.unregister)
	})
}

// RegisterRequest is the body of PUT /api/v1/me/device-tokens.
type RegisterRequest struct {
	Token       string      `json:"token"`
	Environment Environment `json:"environment"`
	// Platform defaults to "ios".
	Platform string `json:"platform,omitempty"`
}

// DeleteRequest is the body of DELETE /api/v1/me/device-tokens.
type DeleteRequest struct {
	Token string `json:"token"`
}

// DeviceTokenResponse is a registered device token.
type DeviceTokenResponse struct {
	Token       string      `json:"token"`
	Environment Environment `json:"environment"`
	Platform    string      `json:"platform"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	var req RegisterRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	t, err := h.opts.Service.Register(r.Context(), userID, req.Token, req.Environment, req.Platform)
	if err != nil {
		h.writeError(w, r, "register device token failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, DeviceTokenResponse{
		Token: t.Token, Environment: t.Environment, Platform: t.Platform, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	})
}

func (h *Handler) unregister(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	var req DeleteRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := h.opts.Service.Unregister(r.Context(), userID, req.Token); err != nil {
		h.writeError(w, r, "delete device token failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	if errors.As(err, &validation) {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
		return
	}
	h.logger.ErrorContext(r.Context(), msg, "error", err)
	httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
}
