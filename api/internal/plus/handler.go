package plus

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
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
		r.Get("/households/{householdId}/plus/referral", h.referral)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermPlanEdit, h.logger))
		r.Post("/households/{householdId}/plus/referral/redeem", h.redeem)
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
	// FreeUntil is when free months from referrals end.
	FreeUntil *time.Time `json:"freeUntil,omitempty"`
	// Offer is the purchase screen's words, while Plus is on.
	Offer *Offer `json:"offer,omitempty"`
}

func statusResponse(st Status) StatusResponse {
	resp := StatusResponse{Enabled: st.Enabled, Active: st.Active, Trial: st.Trial, WillRenew: st.WillRenew, BillingIssue: st.BillingIssue}
	if !st.ExpiresAt.IsZero() {
		t := st.ExpiresAt
		resp.ExpiresAt = &t
	}
	if !st.FreeUntil.IsZero() {
		t := st.FreeUntil
		resp.FreeUntil = &t
	}
	if st.Enabled {
		o := DefaultOffer()
		resp.Offer = &o
	}
	return resp
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	st, err := h.opts.Service.Status(r.Context(), chi.URLParam(r, households.HouseholdIDParam))
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus status failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, statusResponse(st))
}

// ReferralResponse is a household's own invite code.
type ReferralResponse struct {
	Code string `json:"code"`
	// ShareText is the message the app's share sheet starts with.
	ShareText string `json:"shareText"`
	// Explainer sits above the share button.
	Explainer  string `json:"explainer"`
	Rewarded   int    `json:"rewarded"`
	MonthsLeft int    `json:"monthsLeft"`
}

// ReferralText returns the invite screen's words for code.
func ReferralText(code string, rewarded, monthsLeft int) (share, explainer string) {
	share = "I plan our dinners with DinnerOS. Use my code " + code + " and we both get a free month of DinnerOS Plus."
	switch {
	case monthsLeft == 0:
		explainer = "Thanks for sharing DinnerOS. You've earned every free month this code can give."
	case rewarded == 1:
		explainer = "Invite another household. You both get a free month of Plus. You've earned 1 month so far."
	case rewarded > 1:
		explainer = fmt.Sprintf("Invite another household. You both get a free month of Plus. You've earned %d months so far.", rewarded)
	default:
		explainer = "Invite another household. When they join with your code, you both get a free month of Plus."
	}
	return share, explainer
}

func (h *Handler) referral(w http.ResponseWriter, r *http.Request) {
	info, err := h.opts.Service.Referral(r.Context(), chi.URLParam(r, households.HouseholdIDParam))
	if errors.Is(err, ErrReferralsOff) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus referral failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	share, explainer := ReferralText(info.Code, info.Rewarded, info.MonthsLeft)
	httpx.WriteJSON(w, http.StatusOK, ReferralResponse{
		Code: info.Code, ShareText: share, Explainer: explainer, Rewarded: info.Rewarded, MonthsLeft: info.MonthsLeft,
	})
}

type redeemRequest struct {
	Code string `json:"code"`
}

func (h *Handler) redeem(w http.ResponseWriter, r *http.Request) {
	var req redeemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "Enter a code.")
		return
	}
	st, err := h.opts.Service.Redeem(r.Context(), chi.URLParam(r, households.HouseholdIDParam), req.Code)
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, statusResponse(st))
	case errors.Is(err, ErrReferralsOff):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, ErrCodeNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "code_not_found", RedeemMessage(err))
	case errors.Is(err, ErrOwnCode), errors.Is(err, ErrAlreadyRedeemed), errors.Is(err, ErrNotNewToPlus):
		httpx.WriteError(w, r, http.StatusConflict, "code_not_allowed", RedeemMessage(err))
	default:
		h.logger.ErrorContext(r.Context(), "plus redeem failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", RedeemMessage(err))
	}
}

// Require returns middleware that lets a request through only for a
// household with Plus, or while Plus is off. It reads the {householdId} URL
// parameter, so it belongs after the route's permission check. A household
// without Plus gets 402 plus_required, which the app answers with the
// purchase screen.
func Require(s *Service, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s == nil || !s.Enabled() {
				next.ServeHTTP(w, r)
				return
			}
			ok, err := s.HasPlus(r.Context(), chi.URLParam(r, households.HouseholdIDParam))
			if err != nil {
				logger.ErrorContext(r.Context(), "plus check failed", "error", err)
				httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
				return
			}
			if !ok {
				httpx.WriteError(w, r, http.StatusPaymentRequired, "plus_required", RequiredMessage)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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
