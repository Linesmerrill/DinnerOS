package sameitems

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

// HandlerOptions configures the same-item HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the same-item endpoints.
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

// Mount registers the routes on r, the /api/v1 router. A link changes what
// the week's grocery list asks anyone to buy, so writes need plan.edit, like
// skipping an ingredient.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		edit := households.RequirePermission(h.opts.Authorizer, households.PermPlanEdit, h.logger)
		const base = "/households/{householdId}/same-items"
		r.With(view).Get(base, h.list)
		r.With(edit).Post(base, h.link)
		r.With(edit).Delete(base+"/{linkId}", h.unlink)
	})
}

// LinkResponse is one link.
type LinkResponse struct {
	ID          string    `json:"id"`
	VariantKey  string    `json:"variantKey"`
	VariantName string    `json:"variantName"`
	KeptKey     string    `json:"keptKey"`
	KeptName    string    `json:"keptName"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

// LinkListResponse is returned by GET .../same-items.
type LinkListResponse struct {
	Items []LinkResponse `json:"items"`
}

type linkRequest struct {
	VariantKey  string `json:"variantKey"`
	VariantName string `json:"variantName"`
	KeptKey     string `json:"keptKey"`
	KeptName    string `json:"keptName"`
}

func newLinkResponse(l Link) LinkResponse {
	return LinkResponse{
		ID: l.ID, VariantKey: l.VariantKey, VariantName: l.VariantName, KeptKey: l.KeptKey, KeptName: l.KeptName,
		CreatedBy: l.CreatedBy, CreatedAt: l.CreatedAt.UTC(),
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	links, err := h.opts.Service.List(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "list same items failed", err)
		return
	}
	resp := LinkListResponse{Items: make([]LinkResponse, 0, len(links))}
	for _, l := range links {
		resp.Items = append(resp.Items, newLinkResponse(l))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req linkRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	link, created, err := h.opts.Service.Link(r.Context(), actor, Input(req))
	if err != nil {
		h.writeError(w, r, "link same item failed", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, newLinkResponse(link))
}

func (h *Handler) unlink(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.Unlink(r.Context(), actor, chi.URLParam(r, "linkId")); err != nil {
		h.writeError(w, r, "unlink same item failed", err)
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
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "Those items aren't linked anymore.")
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
