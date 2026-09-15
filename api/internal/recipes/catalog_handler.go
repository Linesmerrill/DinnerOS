package recipes

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// IngredientResponse is a catalog ingredient.
type IngredientResponse struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	Category string `json:"category"`
	// CategoryConfident is false when no rule matched and the category is a
	// placeholder ("other") awaiting review.
	CategoryConfident bool   `json:"categoryConfident"`
	ImageURL          string `json:"imageUrl,omitempty"`
}

// IngredientSearchResponse is returned by GET /ingredients.
type IngredientSearchResponse struct {
	Items []IngredientResponse `json:"items"`
}

// searchIngredients serves GET /ingredients?q=&limit=. Any signed-in user may
// search: the catalog is global and holds no household data.
func (h *Handler) searchIngredients(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	limit := 0
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			validationFailed(w, r, "limit must be a positive integer")
			return
		}
		limit = n
	}
	found, err := h.opts.Service.SearchIngredients(r.Context(), v.Get("q"), limit)
	switch {
	case errors.Is(err, ErrInvalidQuery):
		validationFailed(w, r, publicMessage(err))
		return
	case err != nil:
		h.internalError(w, r, "search ingredients failed", err)
		return
	}
	resp := IngredientSearchResponse{Items: make([]IngredientResponse, 0, len(found))}
	for _, ing := range found {
		resp.Items = append(resp.Items, IngredientResponse{
			ID: ing.ID, Key: ing.Key, Name: ing.Name, Category: ing.Category,
			CategoryConfident: ing.CategoryConfident, ImageURL: ing.ImageURL,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
