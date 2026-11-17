package catalog

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// CollectionRequest is the body of POST and PUT
// /api/v1/admin/discover/collections. On PUT the path's ID wins.
type CollectionRequest struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Subtitle  string           `json:"subtitle,omitempty"`
	Type      string           `json:"type"`
	RecipeIDs []string         `json:"recipeIds,omitempty"`
	Rule      string           `json:"rule,omitempty"`
	StartsAt  *time.Time       `json:"startsAt,omitempty"`
	EndsAt    *time.Time       `json:"endsAt,omitempty"`
	Position  int              `json:"position"`
	Sponsor   *SponsorResponse `json:"sponsor,omitempty"`
}

// SponsorResponse is who paid for a sponsored row.
type SponsorResponse struct {
	Name  string `json:"name"`
	URL   string `json:"url,omitempty"`
	Label string `json:"label,omitempty"`
}

// CollectionResponse is one row as an operator sees it.
type CollectionResponse struct {
	CollectionRequest
	// BuiltIn is true for a row that ships in the server and has not been
	// overridden. Deleting an override brings the built-in back.
	BuiltIn bool `json:"builtIn"`
	// Active is true when today is inside the row's dates.
	Active    bool       `json:"active"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// CollectionListResponse is returned by GET /api/v1/admin/discover/collections.
type CollectionListResponse struct {
	Collections []CollectionResponse `json:"collections"`
	// Rules are the rules a row can be computed by.
	Rules []string `json:"rules"`
}

// operator reports whether the caller runs DinnerOS, and answers 403 if not.
func (h *Handler) operator(w http.ResponseWriter, r *http.Request) bool {
	userID, _ := auth.UserIDFromContext(r.Context())
	if userID == "" || !slices.Contains(h.opts.Operators, userID) {
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "Only the people who run DinnerOS can change this.")
		return false
	}
	return true
}

func (h *Handler) listCollections(w http.ResponseWriter, r *http.Request) {
	if !h.operator(w, r) {
		return
	}
	cols, err := h.opts.Service.Collections(r.Context())
	if err != nil {
		h.fail(w, r, "list collections failed", err)
		return
	}
	now := h.opts.Service.now().UTC()
	out := CollectionListResponse{Collections: make([]CollectionResponse, 0, len(cols)), Rules: slices.Clone(Rules)}
	for _, c := range cols {
		out.Collections = append(out.Collections, collectionResponse(c, now))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) putCollection(w http.ResponseWriter, r *http.Request) {
	if !h.operator(w, r) {
		return
	}
	var req CollectionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if id := chi.URLParam(r, "collectionId"); id != "" {
		req.ID = id
	}
	c := Collection{
		ID: req.ID, Title: req.Title, Subtitle: req.Subtitle, Type: req.Type, RecipeIDs: req.RecipeIDs,
		Rule: req.Rule, Position: req.Position,
	}
	if req.StartsAt != nil {
		c.StartsAt = req.StartsAt.UTC()
	}
	if req.EndsAt != nil {
		c.EndsAt = req.EndsAt.UTC()
	}
	if req.Sponsor != nil {
		c.Sponsor = &Sponsor{Name: req.Sponsor.Name, URL: req.Sponsor.URL, Label: req.Sponsor.Label}
	}
	stored, err := h.opts.Service.PutCollection(r.Context(), c)
	if err != nil {
		h.fail(w, r, "put collection failed", err)
		return
	}
	h.logger.InfoContext(r.Context(), "shared recipes row saved", "collection", stored.ID, "type", stored.Type)
	httpx.WriteJSON(w, http.StatusOK, collectionResponse(stored, h.opts.Service.now().UTC()))
}

func (h *Handler) deleteCollection(w http.ResponseWriter, r *http.Request) {
	if !h.operator(w, r) {
		return
	}
	err := h.opts.Service.DeleteCollection(r.Context(), chi.URLParam(r, "collectionId"))
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "There's no saved row with that ID.")
		return
	}
	if err != nil {
		h.fail(w, r, "delete collection failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func collectionResponse(c Collection, now time.Time) CollectionResponse {
	out := CollectionResponse{
		CollectionRequest: CollectionRequest{
			ID: c.ID, Title: c.Title, Subtitle: c.Subtitle, Type: c.Type, RecipeIDs: c.RecipeIDs, Rule: c.Rule,
			Position: c.Position,
		},
		BuiltIn: c.BuiltIn, Active: c.Active(now),
	}
	if !c.StartsAt.IsZero() {
		t := c.StartsAt
		out.StartsAt = &t
	}
	if !c.EndsAt.IsZero() {
		t := c.EndsAt
		out.EndsAt = &t
	}
	if !c.UpdatedAt.IsZero() {
		t := c.UpdatedAt
		out.UpdatedAt = &t
	}
	if c.Sponsor != nil {
		out.Sponsor = &SponsorResponse{Name: c.Sponsor.Name, URL: c.Sponsor.URL, Label: c.Sponsor.Label}
	}
	return out
}

// collectionMessage strips the package prefix an ErrInvalidCollection
// carries, leaving the part written for the operator.
func collectionMessage(err error) string {
	if _, rest, ok := strings.Cut(err.Error(), "invalid collection: "); ok {
		return rest
	}
	return "That row isn't valid."
}
