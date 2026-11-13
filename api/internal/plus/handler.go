package plus

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the Plus routes.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	// WebhookSecret is the Authorization header RevenueCat sends with each
	// webhook (REVENUECAT_WEBHOOK_SECRET). Empty turns the webhook off.
	WebhookSecret string
	Logger        *slog.Logger
}

// Handler serves the Plus routes.
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

// Mount registers the routes on the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/webhooks/revenuecat", h.webhook)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger))
		r.Get("/households/{householdId}/plus", h.status)
	})
}

// StatusResponse is a household's DinnerOS Plus.
type StatusResponse struct {
	// Enabled is false while Plus is off: show no paywall, open everything.
	Enabled      bool       `json:"enabled"`
	Active       bool       `json:"active"`
	Trial        bool       `json:"trial"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	WillRenew    bool       `json:"willRenew"`
	BillingIssue bool       `json:"billingIssue"`
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	st, err := h.opts.Service.Status(r.Context(), chi.URLParam(r, households.HouseholdIDParam))
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus status failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	resp := StatusResponse{Enabled: st.Enabled, Active: st.Active, Trial: st.Trial, WillRenew: st.WillRenew, BillingIssue: st.BillingIssue}
	if !st.ExpiresAt.IsZero() {
		t := st.ExpiresAt
		resp.ExpiresAt = &t
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

type webhookBody struct {
	Event *Event `json:"event"`
}

// webhook serves POST /webhooks/revenuecat. A wrong or missing secret is
// 401; an event this package doesn't use is 200, so RevenueCat stops
// retrying it.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	secret := h.opts.WebhookSecret
	if secret == "" {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	got := r.Header.Get("Authorization")
	if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 &&
		subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+secret)) != 1 {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		return
	}
	var body webhookBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Event == nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "not a RevenueCat event")
		return
	}
	outcome, err := h.opts.Service.Apply(r.Context(), *body.Event)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus webhook failed", "eventType", body.Event.Type, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "try again")
		return
	}
	// The app user ID is a household ID; it's safe to log, the event body isn't kept.
	h.logger.InfoContext(r.Context(), "plus webhook", "eventType", body.Event.Type, "outcome", outcome,
		"environment", body.Event.Environment)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"outcome": string(outcome)})
}
