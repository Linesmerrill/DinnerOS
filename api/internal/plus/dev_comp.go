package plus

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// CompReasonQA is Plus a UI test gave its own new household on a local API.
const CompReasonQA = "qa"

// DevCompHandler serves POST /households/{householdId}/plus/dev-comp, which
// gives the caller's household Plus on the house so a UI test can start from a
// household with Plus (decision 738). Mount it only where development sign-in
// is on: it is never part of a deployed API.
type DevCompHandler struct {
	comps      CompStore
	authorizer households.Authorizer
	tokens     auth.AccessTokenValidator
	logger     *slog.Logger
}

// NewDevCompHandler returns a DevCompHandler.
func NewDevCompHandler(comps CompStore, authorizer households.Authorizer, tokens auth.AccessTokenValidator, logger *slog.Logger) *DevCompHandler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &DevCompHandler{comps: comps, authorizer: authorizer, tokens: tokens, logger: logger}
}

// Mount registers the route on the /api/v1 router. Only an admin of the
// household can give it Plus.
func (h *DevCompHandler) Mount(r chi.Router) {
	r.With(auth.RequireAuth(h.tokens), households.RequirePermission(h.authorizer, households.PermHouseholdUpdate, h.logger)).
		Post("/households/{householdId}/plus/dev-comp", h.grant)
}

func (h *DevCompHandler) grant(w http.ResponseWriter, r *http.Request) {
	m, _ := households.MembershipFromContext(r.Context())
	granted, err := h.comps.Grant(r.Context(), Comp{HouseholdID: m.HouseholdID, Reason: CompReasonQA, GrantedAt: time.Now()})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "dev comp failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"granted": granted})
}
