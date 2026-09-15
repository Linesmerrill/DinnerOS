package notifications

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the notification HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the notification endpoints.
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
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger))
		const base = "/households/{householdId}/notifications"
		r.Get(base, h.list)
		r.Get(base+"/unread-count", h.unreadCount)
		r.Post(base+"/read", h.markRead)
	})
}

// NotificationResponse is one notification as the caller sees it.
type NotificationResponse struct {
	ID          string          `json:"id"`
	HouseholdID string          `json:"householdId"`
	Type        Type            `json:"type"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	Subject     SubjectResponse `json:"subject"`
	// Read is whether the caller has read it.
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"createdAt"`
}

// SubjectResponse is the record a notification is about.
type SubjectResponse struct {
	Kind SubjectKind `json:"kind"`
	ID   string      `json:"id"`
}

// ListResponse is returned by GET .../notifications.
type ListResponse struct {
	Items      []NotificationResponse `json:"items"`
	NextCursor *string                `json:"nextCursor"`
}

// UnreadCountResponse is returned by GET .../notifications/unread-count and
// POST .../notifications/read.
type UnreadCountResponse struct {
	UnreadCount int `json:"unreadCount"`
}

// MarkReadRequest is the body of POST .../notifications/read. Send ids, or
// all: true.
type MarkReadRequest struct {
	IDs []string `json:"ids"`
	All bool     `json:"all"`
}

func newResponse(n Notification, userID string) NotificationResponse {
	return NotificationResponse{
		ID: n.ID, HouseholdID: n.HouseholdID, Type: n.Type, Title: n.Title, Body: n.Body,
		Subject: SubjectResponse{Kind: n.Subject.Kind, ID: n.Subject.ID},
		Read:    n.ReadByUser(userID), CreatedAt: n.CreatedAt.UTC(),
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	v := r.URL.Query()
	q := ListQuery{Before: v.Get("before")}
	if s := v.Get("unread"); s != "" {
		b, err := strconv.ParseBool(s)
		if err != nil {
			h.writeError(w, r, "", invalid("unread must be true or false"))
			return
		}
		q.UnreadOnly = b
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			h.writeError(w, r, "", invalid("limit must be between 1 and %d", MaxListLimit))
			return
		}
		q.Limit = n
		if n == 0 {
			q.Limit = -1 // an explicit 0 is invalid, not the default
		}
	}
	page, err := h.opts.Service.List(r.Context(), actor, q)
	if err != nil {
		h.writeError(w, r, "list notifications failed", err)
		return
	}
	resp := ListResponse{Items: make([]NotificationResponse, 0, len(page.Items))}
	for _, n := range page.Items {
		resp.Items = append(resp.Items, newResponse(n, actor.UserID))
	}
	if page.NextCursor != "" {
		cursor := page.NextCursor
		resp.NextCursor = &cursor
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) unreadCount(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	n, err := h.opts.Service.UnreadCount(r.Context(), actor)
	if err != nil {
		h.writeError(w, r, "count unread notifications failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, UnreadCountResponse{UnreadCount: n})
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req MarkReadRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	n, err := h.opts.Service.MarkRead(r.Context(), actor, req.IDs, req.All)
	if err != nil {
		h.writeError(w, r, "mark notifications read failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, UnreadCountResponse{UnreadCount: n})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.Is(err, households.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
