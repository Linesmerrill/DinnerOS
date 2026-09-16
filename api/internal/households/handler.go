package households

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the household HTTP handlers.
type HandlerOptions struct {
	Service *Service
	Tokens  auth.AccessTokenValidator
	Logger  *slog.Logger
}

// Handler serves the household and membership endpoints.
type Handler struct {
	svc    *Service
	tokens auth.AccessTokenValidator
	logger *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(opts HandlerOptions) *Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{svc: opts.Service, tokens: opts.Tokens, logger: logger}
}

// Mount registers the routes on r, which is expected to be the /api/v1 router.
// Routes are registered with full patterns (not r.Route) so other modules can
// add routes under /households/{householdId}/... on the same router.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.tokens))
		r.Post("/households", h.create)
		r.Get("/households", h.list)

		// Membership (household.view) is checked for every route below; each
		// service method checks the specific permission it needs, because
		// leaving a household needs no permission beyond membership.
		r.Group(func(r chi.Router) {
			r.Use(RequirePermission(h.svc, PermHouseholdView, h.logger))
			r.Get("/households/{householdId}", h.get)
			r.Patch("/households/{householdId}", h.update)
			r.Patch("/households/{householdId}/members/{userId}", h.changeRole)
			r.Delete("/households/{householdId}/members/{userId}", h.removeMember)
		})
	})
}

// --- Wire types ---------------------------------------------------------------

// HouseholdResponse is the public representation of a household.
type HouseholdResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	DefaultServings int    `json:"defaultServings"`
	TimeZone        string `json:"timeZone"`
	// OrderDay is null until the household picks a grocery order day.
	OrderDay  *string   `json:"orderDay"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// orderDayOrNil renders an unset order day as JSON null.
func orderDayOrNil(day string) *string {
	if day == "" {
		return nil
	}
	return &day
}

// NewHouseholdResponse converts a Household for the API.
func NewHouseholdResponse(hh Household) HouseholdResponse {
	return HouseholdResponse{
		ID:              hh.ID,
		Name:            hh.Name,
		DefaultServings: hh.DefaultServings,
		TimeZone:        hh.TimeZone,
		OrderDay:        orderDayOrNil(hh.OrderDay),
		CreatedBy:       hh.CreatedBy,
		CreatedAt:       hh.CreatedAt.UTC(),
		UpdatedAt:       hh.UpdatedAt.UTC(),
	}
}

// MembershipResponse is the public representation of a membership.
type MembershipResponse struct {
	ID          string       `json:"id"`
	HouseholdID string       `json:"householdId"`
	UserID      string       `json:"userId"`
	Role        Role         `json:"role"`
	Permissions []Permission `json:"permissions"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// NewMembershipResponse converts a Membership for the API.
func NewMembershipResponse(m Membership) MembershipResponse {
	return MembershipResponse{
		ID:          m.ID,
		HouseholdID: m.HouseholdID,
		UserID:      m.UserID,
		Role:        m.Role,
		Permissions: m.Role.Permissions(),
		CreatedAt:   m.CreatedAt.UTC(),
		UpdatedAt:   m.UpdatedAt.UTC(),
	}
}

// MemberResponse is one entry of a household's member list.
type MemberResponse struct {
	UserID      string    `json:"userId"`
	DisplayName string    `json:"displayName"`
	Role        Role      `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
}

func newMemberResponse(m Member) MemberResponse {
	return MemberResponse{UserID: m.UserID, DisplayName: m.DisplayName, Role: m.Role, JoinedAt: m.JoinedAt.UTC()}
}

// CreateHouseholdResponse is returned by POST /households.
type CreateHouseholdResponse struct {
	Household  HouseholdResponse  `json:"household"`
	Membership MembershipResponse `json:"membership"`
}

// HouseholdListItem is one entry of GET /households.
type HouseholdListItem struct {
	Household   HouseholdResponse `json:"household"`
	Role        Role              `json:"role"`
	Permissions []Permission      `json:"permissions"`
}

// HouseholdListResponse is returned by GET /households.
type HouseholdListResponse struct {
	Items []HouseholdListItem `json:"items"`
}

// HouseholdDetailResponse is returned by GET /households/{householdId}.
type HouseholdDetailResponse struct {
	Household HouseholdResponse `json:"household"`
	// Members is omitted when the caller's role lacks members.view.
	Members     []MemberResponse `json:"members,omitempty"`
	Role        Role             `json:"role"`
	Permissions []Permission     `json:"permissions"`
}

type createHouseholdRequest struct {
	Name            string `json:"name"`
	TimeZone        string `json:"timeZone"`
	DefaultServings *int   `json:"defaultServings"`
}

type updateHouseholdRequest struct {
	Name            *string `json:"name"`
	TimeZone        *string `json:"timeZone"`
	DefaultServings *int    `json:"defaultServings"`
	// OrderDay set to "" clears the household's order day.
	OrderDay *string `json:"orderDay"`
}

type changeRoleRequest struct {
	Role string `json:"role"`
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	var req createHouseholdRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	hh, m, err := h.svc.Create(r.Context(), userID, CreateInput(req))
	if err != nil {
		h.writeError(w, r, "create household failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, CreateHouseholdResponse{
		Household:  NewHouseholdResponse(hh),
		Membership: NewMembershipResponse(m),
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	list, err := h.svc.ListForUser(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, "list households failed", err)
		return
	}
	resp := HouseholdListResponse{Items: make([]HouseholdListItem, 0, len(list))}
	for _, uh := range list {
		resp.Items = append(resp.Items, HouseholdListItem{
			Household:   NewHouseholdResponse(uh.Household),
			Role:        uh.Membership.Role,
			Permissions: uh.Membership.Role.Permissions(),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := MembershipFromContext(r.Context())
	hh, members, err := h.svc.Details(r.Context(), actor)
	if err != nil {
		h.writeError(w, r, "get household failed", err)
		return
	}
	resp := HouseholdDetailResponse{
		Household:   NewHouseholdResponse(hh),
		Role:        actor.Role,
		Permissions: actor.Role.Permissions(),
	}
	if members != nil {
		resp.Members = make([]MemberResponse, 0, len(members))
		for _, m := range members {
			resp.Members = append(resp.Members, newMemberResponse(m))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	actor, _ := MembershipFromContext(r.Context())
	var req updateHouseholdRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	hh, err := h.svc.Update(r.Context(), actor, UpdateInput(req))
	if err != nil {
		h.writeError(w, r, "update household failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, NewHouseholdResponse(hh))
}

func (h *Handler) changeRole(w http.ResponseWriter, r *http.Request) {
	actor, _ := MembershipFromContext(r.Context())
	var req changeRoleRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Role == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "role is required")
		return
	}
	member, err := h.svc.ChangeRole(r.Context(), actor, chi.URLParam(r, "userId"), Role(req.Role))
	if err != nil {
		h.writeError(w, r, "change member role failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newMemberResponse(member))
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	actor, _ := MembershipFromContext(r.Context())
	if err := h.svc.RemoveMember(r.Context(), actor, chi.URLParam(r, "userId")); err != nil {
		h.writeError(w, r, "remove member failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeError maps service errors to responses.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, ErrForbidden):
		writeForbidden(w, r)
	case errors.Is(err, ErrLastAdmin):
		httpx.WriteError(w, r, http.StatusConflict, "last_admin",
			"a household must keep at least one admin; make another member an admin first")
	case errors.Is(err, ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "the membership changed concurrently; reload and try again")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
