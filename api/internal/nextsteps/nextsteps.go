// Package nextsteps says what a household should do next, so the app can put a
// red dot on the tab, the row that leads there, and the item itself (decisions
// 736-739). It is a registry of rules (rules.go), each a condition read on
// request, the path of rows it badges, and the permission needed to act on it.
// Nothing is stored and nothing is pushed: a step clears as soon as its
// condition stops holding.
package nextsteps

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Rule is one next step.
type Rule struct {
	// ID is stable; the app clears a step by it.
	ID string
	// Path is the row keys it badges, from the tab down to the item.
	Path []string
	// Permission is what a member needs to act on it; members without it
	// never see the step.
	Permission households.Permission
	// Pending reports whether the step is still to do.
	Pending func(ctx context.Context, householdID string) (bool, error)
}

// Step is a pending rule, as the app reads it.
type Step struct {
	ID   string   `json:"id"`
	Path []string `json:"path"`
}

// Service evaluates the rules for one member of a household.
type Service struct {
	rules  []Rule
	logger *slog.Logger
}

// NewService returns a Service over rules, usually Registry's.
func NewService(rules []Rule, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{rules: rules, logger: logger}
}

// Pending returns the steps m's household has left that m's role can act on,
// in registry order. A rule that fails to read is left out and logged: a
// missing dot is better than no dots at all.
func (s *Service) Pending(ctx context.Context, m households.Membership) []Step {
	out := []Step{}
	for _, r := range s.rules {
		if r.Permission != "" && !m.Role.Can(r.Permission) {
			continue
		}
		ok, err := r.Pending(ctx, m.HouseholdID)
		if err != nil {
			s.logger.WarnContext(ctx, "next step check failed", "step", r.ID, "error", err)
			continue
		}
		if ok {
			out = append(out, Step{ID: r.ID, Path: slices.Clone(r.Path)})
		}
	}
	return out
}

// HandlerOptions configures the handler.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves GET /households/{householdId}/next-steps.
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

// Mount registers the route on the /api/v1 router. Any member may read it;
// each step is filtered by the member's own role.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger))
		r.Get("/households/{householdId}/next-steps", h.list)
	})
}

// ListResponse is the household's pending next steps.
type ListResponse struct {
	Items []Step `json:"items"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	m, _ := households.MembershipFromContext(r.Context())
	httpx.WriteJSON(w, http.StatusOK, ListResponse{Items: h.opts.Service.Pending(r.Context(), m)})
}
