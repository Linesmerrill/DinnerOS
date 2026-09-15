package invitations

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

// HandlerOptions configures the invitation HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
	// AcceptRateLimit, when set, wraps POST /invitations/accept. Invite codes
	// are short enough to guess at scale, so production must set it.
	AcceptRateLimit func(http.Handler) http.Handler
	// CreateRateLimit, when set, wraps invitation creation, which sends email.
	CreateRateLimit func(http.Handler) http.Handler
}

// Handler serves the invitation endpoints.
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
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermMembersInvite, h.logger))
		create := http.Handler(http.HandlerFunc(h.create))
		if h.opts.CreateRateLimit != nil {
			create = h.opts.CreateRateLimit(create)
		}
		r.Method(http.MethodPost, "/households/{householdId}/invitations", create)
		r.Get("/households/{householdId}/invitations", h.list)
		r.Delete("/households/{householdId}/invitations/{invitationId}", h.revoke)
	})
	r.Group(func(r chi.Router) {
		// Rate limiting runs first so unauthenticated guessing is limited too.
		if h.opts.AcceptRateLimit != nil {
			r.Use(h.opts.AcceptRateLimit)
		}
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Post("/invitations/accept", h.accept)
	})
}

// --- Wire types ---------------------------------------------------------------

// InvitationResponse is the public representation of an invitation. It never
// includes the token or code.
type InvitationResponse struct {
	ID        string          `json:"id"`
	Email     string          `json:"email"`
	Role      households.Role `json:"role"`
	ExpiresAt time.Time       `json:"expiresAt"`
	CreatedAt time.Time       `json:"createdAt"`
}

func newInvitationResponse(inv Invitation) InvitationResponse {
	return InvitationResponse{
		ID: inv.ID, Email: inv.Email, Role: inv.Role, ExpiresAt: inv.ExpiresAt.UTC(), CreatedAt: inv.CreatedAt.UTC(),
	}
}

// CreateInvitationResponse is returned by POST /households/{householdId}/invitations.
type CreateInvitationResponse struct {
	Invitation InvitationResponse `json:"invitation"`
	// Code is shown once so the admin can share it directly.
	Code           string `json:"code"`
	EmailDelivered bool   `json:"emailDelivered"`
}

// InvitationListResponse is returned by GET /households/{householdId}/invitations.
type InvitationListResponse struct {
	Items []InvitationResponse `json:"items"`
}

// AcceptInvitationResponse is returned by POST /invitations/accept.
type AcceptInvitationResponse struct {
	Household   households.HouseholdResponse `json:"household"`
	Role        households.Role              `json:"role"`
	Permissions []households.Permission      `json:"permissions"`
}

type createInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type acceptInvitationRequest struct {
	Token string `json:"token"`
	Code  string `json:"code"`
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req createInvitationRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Role == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "role is required")
		return
	}
	res, err := h.opts.Service.Create(r.Context(), actor, req.Email, households.Role(req.Role))
	if err != nil {
		h.writeError(w, r, "create invitation failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, CreateInvitationResponse{
		Invitation:     newInvitationResponse(res.Invitation),
		Code:           res.Code,
		EmailDelivered: res.EmailDelivered,
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	pending, err := h.opts.Service.ListPending(r.Context(), actor)
	if err != nil {
		h.writeError(w, r, "list invitations failed", err)
		return
	}
	resp := InvitationListResponse{Items: make([]InvitationResponse, 0, len(pending))}
	for _, inv := range pending {
		resp.Items = append(resp.Items, newInvitationResponse(inv))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.Revoke(r.Context(), actor, chi.URLParam(r, "invitationId")); err != nil {
		h.writeError(w, r, "revoke invitation failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	var req acceptInvitationRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Token) > 256 || len(req.Code) > 64 {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "token or code is too long")
		return
	}
	res, err := h.opts.Service.Accept(r.Context(), userID, req.Token, req.Code)
	if err != nil {
		h.writeError(w, r, "accept invitation failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, AcceptInvitationResponse{
		Household:   households.NewHouseholdResponse(res.Household),
		Role:        res.Membership.Role,
		Permissions: res.Membership.Role.Permissions(),
	})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, r, http.StatusNotFound, "invitation_invalid",
			"this invitation is invalid, expired, or has already been used")
	case errors.Is(err, households.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
	case errors.Is(err, ErrNotFound), errors.Is(err, households.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
