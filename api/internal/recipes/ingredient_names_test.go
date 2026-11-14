package recipes

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestCommonIngredientsAreEachListedOnce(t *testing.T) {
	seen := map[string]string{}
	count := 0
	for _, line := range strings.Split(commonIngredientsFile, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
		key := nameKey(line)
		if prev, ok := seen[key]; ok {
			t.Errorf("%q and %q are the same ingredient", prev, line)
		}
		seen[key] = line
	}
	if count < 600 || count > 1000 {
		t.Errorf("the common list has %d names; want 600 to 1000", count)
	}
	common := CommonIngredients()
	for _, want := range []string{"Garlic", "Ground Beef", "Ground Turkey", "Garlic Powder", "Ginger", "Green Bell Pepper"} {
		if !slices.Contains(common, want) {
			t.Errorf("common list is missing %q", want)
		}
	}
}

func TestNameKeyMergesCaseAndPlurals(t *testing.T) {
	for _, pair := range [][2]string{
		{"Tomatoes", "tomato"}, {"Green Onions", "green onion"}, {"CHERRIES", "Cherry"},
		{"Jalapeños", "jalapeno"}, {"  Ground   Beef ", "ground beef"},
	} {
		if a, b := nameKey(pair[0]), nameKey(pair[1]); a != b {
			t.Errorf("nameKey(%q) = %q, nameKey(%q) = %q; want the same", pair[0], a, pair[1], b)
		}
	}
	if nameKey("Garlic") == nameKey("Garlic Powder") {
		t.Error("garlic and garlic powder share a key")
	}
}

func TestRankIngredientNamesPutsTheMostUsedFirst(t *testing.T) {
	got := rankIngredientNames([]nameTally{
		{key: "onion", recipes: 3, spellings: map[string]int{"Onion": 1, "onions": 3}},
		{key: "garlic", recipes: 9, spellings: map[string]int{"garlic": 9}},
		{key: "saffron thread", recipes: 1, spellings: map[string]int{"Saffron Threads": 1}},
	})
	want := []IngredientName{{"Garlic", 9}, {"Onions", 3}, {"Saffron Threads", 1}}
	if !slices.Equal(got.Household, want) {
		t.Errorf("household = %v; want %v", got.Household, want)
	}
	// The common list leaves out what the household already has.
	if slices.Contains(got.Common, "Garlic") || slices.Contains(got.Common, "Onion") {
		t.Error("common repeats the household's own names")
	}
	if !slices.Contains(got.Common, "Ground Beef") {
		t.Error("common lost Ground Beef")
	}
}

func TestIngredientNamesRoute(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	for _, recipe := range []DraftResponse{
		{Name: "Tacos", Ingredients: []DraftIngredientWire{{Name: "ground beef"}, {Name: "Tomatoes"}, {Name: "tomato"}}},
		{Name: "Chili", Ingredients: []DraftIngredientWire{{Name: "Ground Beef"}, {Name: "Tomato"}}},
		{Name: "Salad", Ingredients: []DraftIngredientWire{{Name: "Tomato"}, {Name: "Feta"}}},
	} {
		recipe.Steps = []string{"Cook."}
		if rec := srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, recipe), userAda); rec.Code != http.StatusCreated {
			t.Fatalf("create %s = %d %s", recipe.Name, rec.Code, rec.Body.String())
		}
	}
	path := "/api/v1/households/" + hhAda + "/ingredient-names"
	rec := srv.do(t, http.MethodGet, path, "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[IngredientNames](t, rec)
	want := []IngredientName{{"Tomato", 3}, {"Ground Beef", 2}, {"Feta", 1}}
	if !slices.Equal(got.Household, want) {
		t.Errorf("household = %v; want %v", got.Household, want)
	}
	if len(got.Common) < 500 || slices.Contains(got.Common, "Feta") {
		t.Errorf("common has %d names, feta included: %v", len(got.Common), slices.Contains(got.Common, "Feta"))
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=300" {
		t.Errorf("Cache-Control = %q", cc)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer token-"+userAda)
	req.Header.Set("If-None-Match", etag)
	again := httptest.NewRecorder()
	srv.router.ServeHTTP(again, req)
	if again.Code != http.StatusNotModified {
		t.Errorf("current copy = %d; want 304", again.Code)
	}
	// Another household doesn't see these names.
	if rec := srv.do(t, http.MethodGet, path, "", userBob); rec.Code != http.StatusNotFound {
		t.Errorf("other household = %d; want 404", rec.Code)
	}
}
