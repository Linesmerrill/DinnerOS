package ratings

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

// HandlerOptions configures the rating HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the rating endpoints.
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
// Ratings are personal, so household.view is enough to manage your own.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger))
		r.Put("/households/{householdId}/recipes/{recipeId}/rating", h.rate)
		r.Delete("/households/{householdId}/recipes/{recipeId}/rating", h.remove)
		r.Get("/households/{householdId}/recipes/{recipeId}/ratings", h.list)
	})
}

// --- Wire types ---------------------------------------------------------------

// RateRequest is the body of PUT .../recipes/{recipeId}/rating.
type RateRequest struct {
	Score   int      `json:"score"`
	Comment string   `json:"comment,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// RatingResponse is one member's rating.
type RatingResponse struct {
	RecipeID  string    `json:"recipeId"`
	UserID    string    `json:"userId"`
	Score     int       `json:"score"`
	Comment   string    `json:"comment"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// HouseholdRatingResponse is a recipe's aggregate rating in a household.
// Average is null when nobody has rated it.
type HouseholdRatingResponse struct {
	Average *float64 `json:"average"`
	Count   int      `json:"count"`
}

// MemberRatingResponse is a rating with the rater's display name.
type MemberRatingResponse struct {
	RatingResponse
	DisplayName string `json:"displayName"`
}

// RatingListResponse is returned by GET .../recipes/{recipeId}/ratings.
type RatingListResponse struct {
	HouseholdRating HouseholdRatingResponse `json:"householdRating"`
	Items           []MemberRatingResponse  `json:"items"`
}

// NewRatingResponse converts a rating to its wire form.
func NewRatingResponse(r Rating) RatingResponse {
	resp := RatingResponse{
		RecipeID: r.RecipeID, UserID: r.UserID, Score: r.Score, Comment: r.Comment,
		Tags: make([]string, 0, len(r.Tags)), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
	for _, t := range r.Tags {
		resp.Tags = append(resp.Tags, string(t))
	}
	return resp
}

// NewHouseholdRatingResponse converts a summary's aggregate to its wire form.
func NewHouseholdRatingResponse(s Summary) HouseholdRatingResponse {
	resp := HouseholdRatingResponse{Count: s.Count}
	if avg, ok := s.Average(); ok {
		resp.Average = &avg
	}
	return resp
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) rate(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req RateRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	saved, err := h.opts.Service.Rate(r.Context(), actor, chi.URLParam(r, "recipeId"), RateInput(req))
	if h.writeServiceError(w, r, "rate recipe failed", err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, NewRatingResponse(saved))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	err := h.opts.Service.Remove(r.Context(), actor, chi.URLParam(r, "recipeId"))
	if h.writeServiceError(w, r, "remove rating failed", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	list, summary, err := h.opts.Service.List(r.Context(), actor, chi.URLParam(r, "recipeId"))
	if h.writeServiceError(w, r, "list ratings failed", err) {
		return
	}
	resp := RatingListResponse{HouseholdRating: NewHouseholdRatingResponse(summary), Items: make([]MemberRatingResponse, 0, len(list))}
	for _, mr := range list {
		resp.Items = append(resp.Items, MemberRatingResponse{RatingResponse: NewRatingResponse(mr.Rating), DisplayName: mr.DisplayName})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// writeServiceError writes the response for a non-nil service error and
// reports whether it did.
func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, msg string, err error) bool {
	var ve *ValidationError
	switch {
	case err == nil:
		return false
	case errors.As(err, &ve):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", ve.Message)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "recipe not found")
	case errors.Is(err, households.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
	return true
}
