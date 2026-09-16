package account

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// deleteTimeout bounds a deletion, which runs detached from the request so a
// client that disconnects part-way doesn't leave it half done.
const deleteTimeout = 25 * time.Second

// Deleter is the Service method the handler calls.
type Deleter interface {
	Delete(ctx context.Context, userID string) (Result, error)
}

// HandlerOptions configures the account HTTP handler.
type HandlerOptions struct {
	Service Deleter
	Tokens  auth.AccessTokenValidator
	Logger  *slog.Logger
}

// Handler serves DELETE /api/v1/me.
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

// Mount registers the route on r, the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.With(auth.RequireAuth(h.opts.Tokens)).Delete("/me", h.deleteAccount)
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), deleteTimeout)
	defer cancel()
	if _, err := h.opts.Service.Delete(ctx, userID); err != nil {
		h.logger.ErrorContext(r.Context(), "delete account failed", "userId", userID, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
