package events

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the event HTTP handler.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
	// RateLimit, when set, wraps ingestion. It runs after authentication and
	// before the membership lookup, so it can key on the user (RateLimitKey).
	RateLimit func(http.Handler) http.Handler
}

// Handler serves the event ingestion endpoint.
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

// RateLimitKey keys a rate limiter by the authenticated user, so each member
// has their own ingestion budget wherever they connect from. Use it with
// ratelimit.Limiter.MiddlewareBy after auth.RequireAuth.
func RateLimitKey(r *http.Request) string {
	userID, _ := auth.UserIDFromContext(r.Context())
	return "user:" + userID
}

// Mount registers the routes on r, which is expected to be the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		if h.opts.RateLimit != nil {
			r.Use(h.opts.RateLimit)
		}
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger))
		r.Post("/households/{householdId}/events", h.ingest)
	})
}

// IngestRequest is the body of POST /households/{householdId}/events.
type IngestRequest struct {
	Events []ClientEventRequest `json:"events"`
}

// ClientEventRequest is one client-observed event.
type ClientEventRequest struct {
	ClientEventID string          `json:"clientEventId,omitempty"`
	Type          string          `json:"type"`
	RecipeID      string          `json:"recipeId,omitempty"`
	Week          string          `json:"week,omitempty"`
	OccurredAt    string          `json:"occurredAt"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// IngestResponse reports the outcome for every event in the batch.
type IngestResponse struct {
	Accepted   int                 `json:"accepted"`
	Duplicates int                 `json:"duplicates"`
	Rejected   []RejectionResponse `json:"rejected"`
}

// RejectionResponse explains why one event was not stored.
type RejectionResponse struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req IngestRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	batch := make([]ClientEvent, 0, len(req.Events))
	for _, e := range req.Events {
		batch = append(batch, ClientEvent{
			ClientEventID: e.ClientEventID, Type: e.Type, RecipeID: e.RecipeID, Week: e.Week,
			OccurredAt: e.OccurredAt, Payload: e.Payload,
		})
	}
	res, err := h.opts.Service.Ingest(r.Context(), actor, batch)
	switch {
	case errors.Is(err, ErrInvalidBatch):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", publicMessage(err))
		return
	case errors.Is(err, households.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "ingest events failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	resp := IngestResponse{Accepted: res.Accepted, Duplicates: res.Duplicates, Rejected: make([]RejectionResponse, 0, len(res.Rejected))}
	for _, rej := range res.Rejected {
		resp.Rejected = append(resp.Rejected, RejectionResponse(rej))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
