package account

import (
	"bytes"
	"context"
	"errors"
	"io"
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

// appleRevokeTimeout is the part of deleteTimeout Apple may use. The
// deletion goes ahead when it runs out.
const appleRevokeTimeout = 8 * time.Second

// Deleter is the Service method the handler calls.
type Deleter interface {
	Delete(ctx context.Context, userID string) (Result, error)
}

// AppleRevoker ends the user's Sign in with Apple authorization before the
// account is deleted (api/internal/siwa). It never fails the deletion: it
// logs its own errors. code is a fresh authorization code from the app, or "".
type AppleRevoker interface {
	RevokeForDeletion(ctx context.Context, userID, code string)
}

// HandlerOptions configures the account HTTP handler.
type HandlerOptions struct {
	Service Deleter
	Tokens  auth.AccessTokenValidator
	Logger  *slog.Logger
	// Apple, when set, revokes Sign in with Apple before each deletion.
	Apple AppleRevoker
}

// deleteRequest is the optional body of DELETE /me.
type deleteRequest struct {
	// AppleAuthorizationCode is a code from Sign in with Apple at deletion
	// time, for users whose Apple refresh token the server doesn't have.
	AppleAuthorizationCode string `json:"appleAuthorizationCode"`
}

// maxAppleCodeLength bounds the code in the body; a longer one is ignored.
const maxAppleCodeLength = 1024

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
	var req deleteRequest
	if !decodeOptionalJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), deleteTimeout)
	defer cancel()
	if h.opts.Apple != nil {
		code := req.AppleAuthorizationCode
		if len(code) > maxAppleCodeLength {
			code = ""
		}
		appleCtx, appleCancel := context.WithTimeout(ctx, appleRevokeTimeout)
		h.opts.Apple.RevokeForDeletion(appleCtx, userID, code)
		appleCancel()
	}
	if _, err := h.opts.Service.Delete(ctx, userID); err != nil {
		h.logger.ErrorContext(r.Context(), "delete account failed", "userId", userID, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeOptionalJSON decodes the body into dst when there is one. An empty
// body is fine: the app sent none before build 116.
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return true
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "request body is too large")
			return false
		}
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "request body could not be read")
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return httpx.DecodeJSON(w, r, dst)
}
