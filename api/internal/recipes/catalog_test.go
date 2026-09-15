package recipes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// seedCatalog adds synthetic catalog ingredients by name.
func seedCatalog(t *testing.T, store Store, names ...string) {
	t.Helper()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	list := make([]Ingredient, 0, len(names))
	for _, name := range names {
		category, confident := ingredients.Categorize(name)
		list = append(list, Ingredient{Key: ingredients.NormalizeName(name), Name: name, Category: category, CategoryConfident: confident, CreatedAt: now, UpdatedAt: now})
	}
	if _, err := store.UpsertIngredients(context.Background(), list); err != nil {
		t.Fatalf("UpsertIngredients() error = %v", err)
	}
}

var catalogFixture = []string{"Sesame Oil", "Olive Oil", "Oil Spray", "Oil", "Boiled Egg", "Jalapeño", "Garlic", "Garlic Powder", "Black Garlic"}

func keysOf(list []Ingredient) []string {
	out := make([]string, 0, len(list))
	for _, ing := range list {
		out = append(out, ing.Key)
	}
	return out
}

// runIngredientSearchContract checks search behavior shared by every Store.
func runIngredientSearchContract(t *testing.T, svc *Service) {
	t.Helper()
	ctx := context.Background()
	tests := []struct {
		query string
		limit int
		want  []string
	}{
		// Exact, then prefix, then later-word matches; "boiled egg" is not a word match.
		{"oil", 0, []string{"oil", "oil spray", "olive oil", "sesame oil"}},
		{"  OIL ", 2, []string{"oil", "oil spray"}},
		{"oil", 3, []string{"oil", "oil spray", "olive oil"}},
		{"garl", 0, []string{"garlic", "garlic powder", "black garlic"}},
		{"jalapeno", 0, []string{"jalapeno"}},
		{"Jalapeño", 0, []string{"jalapeno"}},
		{"sesame-oil", 0, []string{"sesame oil"}},
		{"zorblax", 0, []string{}},
		{"o.l", 0, []string{}}, // punctuation becomes a space, never a regex wildcard
	}
	for _, tt := range tests {
		got, err := svc.SearchIngredients(ctx, tt.query, tt.limit)
		if err != nil {
			t.Fatalf("SearchIngredients(%q) error = %v", tt.query, err)
		}
		if keys := keysOf(got); !slices.Equal(keys, tt.want) {
			t.Errorf("SearchIngredients(%q, %d) = %v, want %v", tt.query, tt.limit, keys, tt.want)
		}
	}
	got, err := svc.SearchIngredients(ctx, "garlic powder", 0)
	if err != nil || len(got) != 1 || got[0].Name != "Garlic Powder" || got[0].Category != "spices" || !got[0].CategoryConfident || got[0].ID == "" {
		t.Errorf("garlic powder = %+v, %v", got, err)
	}

	for _, bad := range []struct {
		query string
		limit int
	}{{"", 0}, {"  ", 0}, {"!!!", 0}, {strings.Repeat("a", 101), 0}, {"oil", -1}} {
		if _, err := svc.SearchIngredients(ctx, bad.query, bad.limit); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("SearchIngredients(%q, %d) error = %v, want ErrInvalidQuery", bad.query, bad.limit, err)
		}
	}
}

func TestSearchIngredients(t *testing.T) {
	store := newMemoryStore()
	seedCatalog(t, store, catalogFixture...)
	runIngredientSearchContract(t, NewService(store))
}

func TestSearchIngredientsCapsLimit(t *testing.T) {
	store := newMemoryStore()
	var names []string
	for i := range MaxIngredientSearchLimit + 10 {
		names = append(names, "Pepper "+strings.Repeat("x", i+1))
	}
	seedCatalog(t, store, names...)
	got, err := NewService(store).SearchIngredients(context.Background(), "pepper", 1000)
	if err != nil || len(got) != MaxIngredientSearchLimit {
		t.Fatalf("len = %d, %v; want %d", len(got), err, MaxIngredientSearchLimit)
	}
}

func TestIngredientLookups(t *testing.T) {
	store := newMemoryStore()
	seedCatalog(t, store, "Olive Oil", "Salt")
	svc := NewService(store)
	ctx := context.Background()

	byKey, err := svc.IngredientsByKey(ctx, []string{"olive oil", "missing"})
	if err != nil || len(byKey) != 1 || byKey[0].Name != "Olive Oil" {
		t.Fatalf("IngredientsByKey = %+v, %v", byKey, err)
	}
	byID, err := svc.IngredientsByID(ctx, []string{byKey[0].ID, "not-an-id"})
	if err != nil || len(byID) != 1 || byID[0].Key != "olive oil" {
		t.Errorf("IngredientsByID = %+v, %v", byID, err)
	}
	if none, err := svc.IngredientsByKey(ctx, nil); err != nil || none != nil {
		t.Errorf("IngredientsByKey(nil) = %+v, %v", none, err)
	}
}

func TestIngredientSearchHandler(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	seedCatalog(t, srv.store, catalogFixture...)

	// Household membership is not required: userCat belongs to none.
	rec := srv.do(t, http.MethodGet, "/api/v1/ingredients?q=oil&limit=2", "", userCat)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[IngredientSearchResponse](t, rec)
	if len(got.Items) != 2 || got.Items[0].Key != "oil" || got.Items[0].Name != "Oil" || got.Items[0].Category != "pantry" || !got.Items[0].CategoryConfident || got.Items[1].Key != "oil spray" {
		t.Errorf("items = %+v", got.Items)
	}
	if empty := srv.do(t, http.MethodGet, "/api/v1/ingredients?q=zorblax", "", userAda); strings.TrimSpace(empty.Body.String()) != `{"items":[]}` {
		t.Errorf("no matches = %s", empty.Body.String())
	}

	wantError(t, srv.do(t, http.MethodGet, "/api/v1/ingredients?q=oil", "", ""), 401, "unauthenticated")
	wantError(t, srv.do(t, http.MethodGet, "/api/v1/ingredients", "", userAda), 400, "validation_failed")
	wantError(t, srv.do(t, http.MethodGet, "/api/v1/ingredients?q=oil&limit=0", "", userAda), 400, "validation_failed")
	wantError(t, srv.do(t, http.MethodGet, "/api/v1/ingredients?q=oil&limit=many", "", userAda), 400, "validation_failed")
}

func TestIntegrationIngredientSearch(t *testing.T) {
	svc, store, _ := newTestMongoService(t)
	seedCatalog(t, store, catalogFixture...)
	runIngredientSearchContract(t, svc)
}
