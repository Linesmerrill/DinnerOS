package recipes

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// --- Wire types ---------------------------------------------------------------

// ParseRecipeRequest is the body of POST .../recipes/parse. Exactly one of
// Text and URL is given.
type ParseRecipeRequest struct {
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
}

// DraftResponse is a parsed recipe for review, and the body of
// POST .../recipes when the member saves it.
type DraftResponse struct {
	Name        string `json:"name"`
	Headline    string `json:"headline,omitempty"`
	Description string `json:"description,omitempty"`
	SourceURL   string `json:"sourceUrl,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	// Servings is what the amounts are for; 0 means the source didn't say.
	Servings     int                   `json:"servings"`
	PrepMinutes  int                   `json:"prepMinutes,omitempty"`
	TotalMinutes int                   `json:"totalMinutes,omitempty"`
	Cuisines     []string              `json:"cuisines"`
	Tags         []string              `json:"tags"`
	Ingredients  []DraftIngredientWire `json:"ingredients"`
	Steps        []string              `json:"steps"`
	// Warnings are what the parser could not read. They are advice, not
	// errors, and are ignored when a draft is saved.
	Warnings []string `json:"warnings"`
	// FromURL marks a draft that came from a web page. The client sends it
	// back unchanged; it decides only whether the saved recipe records the
	// page as its source.
	FromURL bool `json:"fromUrl"`
}

// DraftIngredientWire is one ingredient line of a draft.
type DraftIngredientWire struct {
	Name string `json:"name"`
	// Quantity is the amount as text ("1 1/2"), empty when there is none.
	Quantity string `json:"quantity,omitempty"`
	// Unit is a DinnerOS unit code, empty when the line is counted or the
	// parser could not map the word it found.
	Unit         string `json:"unit,omitempty"`
	RawText      string `json:"rawText,omitempty"`
	PantryStaple bool   `json:"pantryStaple"`
}

// SharingRequest is the body of PUT .../recipes/{recipeId}/sharing.
type SharingRequest struct {
	SharedToCatalog bool `json:"sharedToCatalog"`
}

// SharingResponse reports a recipe's catalog opt-in.
type SharingResponse struct {
	RecipeID        string `json:"recipeId"`
	SharedToCatalog bool   `json:"sharedToCatalog"`
	// InCatalog is true when this recipe is in the global catalog, either
	// because it is shared or because it came from a public source.
	InCatalog bool `json:"inCatalog"`
}

func newDraftResponse(d Draft, fromURL bool) DraftResponse {
	out := DraftResponse{
		Name: d.Name, Headline: d.Headline, Description: d.Description, SourceURL: d.SourceURL, ImageURL: d.ImageURL,
		Servings: d.Servings, PrepMinutes: d.PrepMinutes, TotalMinutes: d.TotalMinutes,
		Cuisines: orEmpty(d.Cuisines), Tags: orEmpty(d.Tags),
		Ingredients: make([]DraftIngredientWire, 0, len(d.Ingredients)),
		Steps:       orEmpty(d.Steps), Warnings: orEmpty(d.Warnings), FromURL: fromURL,
	}
	for _, line := range d.Ingredients {
		out.Ingredients = append(out.Ingredients, DraftIngredientWire(line))
	}
	return out
}

func (r DraftResponse) draft() Draft {
	d := Draft{
		Name: r.Name, Headline: r.Headline, Description: r.Description, SourceURL: r.SourceURL, ImageURL: r.ImageURL,
		Servings: r.Servings, PrepMinutes: r.PrepMinutes, TotalMinutes: r.TotalMinutes,
		Cuisines: r.Cuisines, Tags: r.Tags, Steps: r.Steps,
	}
	for _, line := range r.Ingredients {
		d.Ingredients = append(d.Ingredients, DraftIngredient(line))
	}
	return d
}

// --- Handlers -----------------------------------------------------------------

// parseRecipe serves POST .../recipes/parse. It stores nothing: it reads
// pasted text or a page and hands back a draft for a person to review. The
// two-step shape is the point — a parser is never the last word on what goes
// into a household's library.
func (h *Handler) parseRecipe(w http.ResponseWriter, r *http.Request) {
	var req ParseRecipeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	text, link := strings.TrimSpace(req.Text), strings.TrimSpace(req.URL)
	switch {
	case text == "" && link == "":
		validationFailed(w, r, "give either text or url")
		return
	case text != "" && link != "":
		validationFailed(w, r, "give either text or url, not both")
		return
	case len(text) > MaxDraftTextBytes:
		httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "that recipe is too long to read")
		return
	}
	if text != "" {
		httpx.WriteJSON(w, http.StatusOK, newDraftResponse(ParseText(text), false))
		return
	}
	d, err := h.fetcher().Fetch(r.Context(), link)
	if err != nil {
		if errors.Is(err, ErrFetch) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "fetch_failed", publicMessage(err))
			return
		}
		h.internalError(w, r, "fetch recipe page failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newDraftResponse(d, true))
}

// createRecipe serves POST .../recipes: the reviewed draft becomes one of the
// household's own recipes. It never enters the global catalog.
func (h *Handler) createRecipe(w http.ResponseWriter, r *http.Request) {
	var req DraftResponse
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	actor, _ := households.MembershipFromContext(r.Context())
	stored, err := h.opts.Service.CreateFromDraft(r.Context(), actor.HouseholdID, req.draft(), req.FromURL)
	switch {
	case errors.Is(err, ErrInvalidDraft), errors.Is(err, ErrInvalidImport):
		validationFailed(w, r, publicMessage(err))
		return
	case err != nil:
		h.internalError(w, r, "create recipe failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, newRecipeResponse(stored, h.timeBands(r.Context(), actor.HouseholdID)))
}

// setSharing serves PUT .../recipes/{recipeId}/sharing: the household's
// opt-in for putting one of its own recipes in the global catalog. It is off
// until a member turns it on, and it has no effect on a recipe that came from
// a public source, which is publishable anyway.
func (h *Handler) setSharing(w http.ResponseWriter, r *http.Request) {
	var req SharingRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	actor, _ := households.MembershipFromContext(r.Context())
	stored, err := h.opts.Service.Share(r.Context(), actor.HouseholdID, chi.URLParam(r, "recipeId"), req.SharedToCatalog)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "recipe not found")
		return
	case err != nil:
		h.internalError(w, r, "set recipe sharing failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SharingResponse{
		RecipeID: stored.ID, SharedToCatalog: stored.SharedToCatalog, InCatalog: Publishable(stored),
	})
}

func (h *Handler) fetcher() *WebFetcher {
	if h.opts.Fetcher != nil {
		return h.opts.Fetcher
	}
	return &WebFetcher{}
}
