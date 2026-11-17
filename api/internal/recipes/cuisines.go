package recipes

import (
	"net/http"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/cuisine"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Every recipe has a cuisine, so Autopilot can learn "Mexican on Tuesdays"
// and Shared Recipes can filter by it. A source's own cuisines are kept as
// they came; the household's pick is stored canonical and marked chosen; and
// a recipe with neither shows the cuisine its title names, read fresh each
// time rather than written over stored recipes (decisions 720 and 721).

// ShownCuisines is the recipe's cuisines as canonical values: its own, or
// a guess from its title and tags when it has none and the household never
// picked.
func (r Recipe) ShownCuisines() []string {
	return cuisine.Fill(r.Cuisines, r.CuisinesChosen, r.Name, r.Tags)
}

// editedCuisines cleans the cuisines a member picked.
func editedCuisines(values []string) ([]string, error) {
	distinct := map[string]bool{}
	for _, v := range values {
		if len(strings.TrimSpace(v)) > cuisine.MaxLength {
			return nil, invalidEdit("that cuisine name is too long")
		}
		if c := cuisine.Canonical(v); c != "" {
			distinct[c] = true
		}
	}
	if len(distinct) > cuisine.MaxPerRecipe {
		return nil, invalidEdit("pick up to 5 cuisines")
	}
	return cuisine.Clean(values), nil
}

// CuisineGuessRequest is the body of POST .../recipes/cuisine-guess: a
// recipe's title, and its tags if it has any.
type CuisineGuessRequest struct {
	Name string   `json:"name"`
	Tags []string `json:"tags,omitempty"`
}

// CuisineGuessResponse is the cuisine the title names, as values and as the
// app shows them; both empty when nothing says.
type CuisineGuessResponse struct {
	Cuisines []string `json:"cuisines"`
	Labels   []string `json:"labels"`
}

// guessCuisine serves POST .../recipes/cuisine-guess, so Add Recipe can
// fill in the cuisine while the member types a title (decision 721). It
// stores nothing. The title goes in the body, not the address, so it never
// lands in a log or the app's saved answers.
func (h *Handler) guessCuisine(w http.ResponseWriter, r *http.Request) {
	var req CuisineGuessRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	name := clip(req.Name, maxDraftName)
	resp := CuisineGuessResponse{Cuisines: orEmpty(cuisine.Guess(name, labels(req.Tags))), Labels: []string{}}
	for _, v := range resp.Cuisines {
		resp.Labels = append(resp.Labels, cuisine.DisplayLabel(v))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// draftCuisines are the cuisines a draft shows for review: the member's pick
// when they made one, else the page's own, canonical, or a guess.
func draftCuisines(d Draft) []string {
	if d.CuisinesChosen {
		return cuisine.Clean(d.Cuisines)
	}
	return cuisine.Fill(d.Cuisines, false, d.Name, d.Tags)
}
