package shelflife

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Handler serves GET .../pantry/shelf-life.
type Handler struct {
	service    *Service
	authorizer households.Authorizer
	tokens     auth.AccessTokenValidator
	logger     *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(service *Service, authorizer households.Authorizer, tokens auth.AccessTokenValidator, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{service: service, authorizer: authorizer, tokens: tokens, logger: logger}
}

// Mount registers the route on the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.tokens))
		r.Use(households.RequirePermission(h.authorizer, households.PermHouseholdView, h.logger))
		r.Get("/households/{householdId}/pantry/shelf-life", h.lookup)
	})
}

// LookupResponse is a recommended best-by date.
type LookupResponse struct {
	BestBy   string  `json:"bestBy"`
	StoredOn string  `json:"storedOn"`
	Storage  Storage `json:"storage"`
	// Text is the recommended time: "2–3 weeks".
	Text    string `json:"text"`
	MinDays int    `json:"minDays"`
	MaxDays int    `json:"maxDays"`
	// Matched is the library food the time is for; absent for an estimate.
	Matched  string `json:"matched,omitempty"`
	Estimate bool   `json:"estimate"`
	Source   string `json:"source"`
	// UsualStorage is where the food is usually kept, for a form's default.
	UsualStorage Storage `json:"usualStorage"`
}

func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	q := r.URL.Query()
	res, err := h.service.Lookup(r.Context(), actor.HouseholdID, q.Get("name"), q.Get("category"), Storage(q.Get("storage")), q.Get("storedOn"))
	if errors.Is(err, ErrInvalid) {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "shelf-life lookup failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, LookupResponse{
		BestBy: res.BestBy, StoredOn: res.StoredOn, Storage: res.Storage, Text: res.Text,
		MinDays: res.Life[0], MaxDays: res.Life[1], Matched: res.Matched, Estimate: res.Estimate, Source: res.Source,
		UsualStorage: res.Usual,
	})
}
