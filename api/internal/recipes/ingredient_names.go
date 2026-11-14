package recipes

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Ingredient name suggestions (decision 692): while a member types an
// ingredient, the app suggests names. The household's own come first, most
// used first, then a bundled list of common ingredients. The app keeps the
// response, so suggestions work offline.

// MaxHouseholdIngredientNames caps the household's own names in a response.
const MaxHouseholdIngredientNames = 2000

//go:embed common_ingredients.txt
var commonIngredientsFile string

// IngredientName is one of the household's ingredient names and how many of
// its recipes use it.
type IngredientName struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// IngredientNames is the body of GET .../ingredient-names.
type IngredientNames struct {
	Household []IngredientName `json:"household"`
	// Common is the bundled list, without names the household already has.
	Common []string `json:"common"`
}

// nameKey is the key two spellings of one ingredient share: case, accents,
// punctuation, and a plural last word don't matter ("Tomatoes", "tomato").
func nameKey(name string) string {
	n := ingredients.NormalizeName(name)
	if n == "" {
		return ""
	}
	return singularize(n)
}

// CommonIngredients is the bundled list, in file order, each ingredient once.
var CommonIngredients = sync.OnceValue(func() []string {
	var out []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(commonIngredientsFile))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key := nameKey(line)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	return out
})

// IngredientNames returns the household's ingredient names, most used first,
// and the common list without them.
func (s *Service) IngredientNames(ctx context.Context, householdID string) (IngredientNames, error) {
	tallies := map[string]*nameTally{}
	after := ""
	for {
		page, err := s.store.ListRecipesAfter(ctx, householdID, after, 200)
		if err != nil {
			return IngredientNames{}, err
		}
		for _, r := range page {
			inRecipe := map[string]bool{}
			for _, line := range r.Ingredients {
				spelling := strings.Join(strings.Fields(line.Name), " ")
				key := nameKey(spelling)
				if key == "" {
					continue
				}
				t := tallies[key]
				if t == nil {
					t = &nameTally{key: key, spellings: map[string]int{}}
					tallies[key] = t
				}
				t.spellings[spelling]++
				if !inRecipe[key] {
					inRecipe[key] = true
					t.recipes++
				}
			}
		}
		if len(page) < 200 {
			break
		}
		after = page[len(page)-1].ID
	}
	list := make([]nameTally, 0, len(tallies))
	for _, t := range tallies {
		list = append(list, *t)
	}
	return rankIngredientNames(list), nil
}

// nameTally is one ingredient across the household's recipes: how many use
// it, and how often each spelling appears.
type nameTally struct {
	key       string
	recipes   int
	spellings map[string]int
}

// rankIngredientNames orders the household's names, most used first, and
// leaves out of the common list what the household already has.
func rankIngredientNames(tallies []nameTally) IngredientNames {
	slices.SortFunc(tallies, func(a, b nameTally) int {
		if a.recipes != b.recipes {
			return b.recipes - a.recipes
		}
		return strings.Compare(a.key, b.key)
	})
	if len(tallies) > MaxHouseholdIngredientNames {
		tallies = tallies[:MaxHouseholdIngredientNames]
	}
	out := IngredientNames{Household: make([]IngredientName, 0, len(tallies))}
	have := map[string]bool{}
	for _, t := range tallies {
		have[t.key] = true
		out.Household = append(out.Household, IngredientName{Name: displaySpelling(t.spellings), Count: t.recipes})
	}
	out.Common = make([]string, 0, len(CommonIngredients()))
	for _, name := range CommonIngredients() {
		if !have[nameKey(name)] {
			out.Common = append(out.Common, name)
		}
	}
	return out
}

// displaySpelling is the most used spelling, capitalized word by word when
// it was typed all in lowercase ("ground beef" is "Ground Beef").
func displaySpelling(spellings map[string]int) string {
	best, bestCount := "", 0
	for spelling, n := range spellings {
		if n > bestCount || (n == bestCount && spelling < best) {
			best, bestCount = spelling, n
		}
	}
	if best != strings.ToLower(best) {
		return best
	}
	words := strings.Fields(best)
	for i, w := range words {
		words[i] = capitalizeWord(w)
	}
	return strings.Join(words, " ")
}

func capitalizeWord(w string) string {
	for i, r := range w {
		return strings.ToUpper(string(r)) + w[i+len(string(r)):]
	}
	return w
}

// --- HTTP -----------------------------------------------------------------------

func (h *Handler) mountIngredientNames(r chi.Router, view func(http.Handler) http.Handler) {
	r.With(view).Get("/households/{householdId}/ingredient-names", h.ingredientNames)
}

// ingredientNames serves GET .../ingredient-names. It can be cached for a
// few minutes and answers 304 when the app's copy is current.
func (h *Handler) ingredientNames(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	names, err := h.opts.Service.IngredientNames(r.Context(), actor.HouseholdID)
	if err != nil {
		h.internalError(w, r, "list ingredient names failed", err)
		return
	}
	body, err := json.Marshal(names)
	if err != nil {
		h.internalError(w, r, "encode ingredient names failed", err)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, json.RawMessage(body))
}
