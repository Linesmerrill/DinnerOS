package skips

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the skipped ingredient HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the skipped ingredient endpoints.
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

// Mount registers the routes on r, which is expected to be the /api/v1 router.
// Skipping changes what the week's grocery list asks anyone to buy, so writes
// need plan.edit, like changing the week's meals.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		edit := households.RequirePermission(h.opts.Authorizer, households.PermPlanEdit, h.logger)
		const base = "/households/{householdId}/grocery-skips"
		r.With(view).Get(base, h.list)
		r.With(edit).Post(base, h.set)
		r.With(edit).Delete(base+"/{skipId}", h.remove)
	})
}

// --- Wire types ---------------------------------------------------------------

// SkipResponse is one skipped ingredient.
type SkipResponse struct {
	ID string `json:"id"`
	// IngredientKey is the grocery line key the skip was made from.
	IngredientKey string `json:"ingredientKey"`
	// Key is the normalized name the skip also matches.
	Key   string `json:"key"`
	Name  string `json:"name"`
	Scope Scope  `json:"scope"`
	// Week is the ISO week a week-scoped skip covers, and null for always.
	Week *string `json:"week"`
	// Text says what the skip does, in the words the app shows.
	Text      string    `json:"text"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedBy string    `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SkipListResponse is returned by GET .../grocery-skips.
type SkipListResponse struct {
	Items []SkipResponse `json:"items"`
}

type setSkipRequest struct {
	IngredientKey string `json:"ingredientKey"`
	Name          string `json:"name"`
	Scope         string `json:"scope"`
	Week          string `json:"week"`
}

func newSkipResponse(s Skip) SkipResponse {
	resp := SkipResponse{
		ID: s.ID, IngredientKey: s.IngredientKey, Key: s.Key, Name: s.Name, Scope: s.Scope,
		CreatedBy: s.CreatedBy, CreatedAt: s.CreatedAt.UTC(),
		UpdatedBy: s.UpdatedBy, UpdatedAt: s.UpdatedAt.UTC(),
	}
	if s.Week != "" {
		week := s.Week
		resp.Week = &week
	}
	if s.Scope == ScopeAlways {
		resp.Text = "Never buying this"
	} else {
		resp.Text = "Skipped this week"
	}
	return resp
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	items, err := h.opts.Service.List(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "list skipped ingredients failed", err)
		return
	}
	resp := SkipListResponse{Items: make([]SkipResponse, 0, len(items))}
	for _, s := range items {
		resp.Items = append(resp.Items, newSkipResponse(s))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req setSkipRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	skip, created, err := h.opts.Service.Set(r.Context(), actor, Input{
		IngredientKey: req.IngredientKey, Name: req.Name, Scope: Scope(req.Scope), Week: req.Week,
	})
	if err != nil {
		h.writeError(w, r, "skip ingredient failed", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, newSkipResponse(skip))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.Remove(r.Context(), actor, chi.URLParam(r, "skipId")); err != nil {
		h.writeError(w, r, "resume ingredient failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "skipped ingredient not found")
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
