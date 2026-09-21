package catalog

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

const (
	hhAda = "66e5a1f2c3b4a5d6e7f80a01"
	hhBob = "66e5a1f2c3b4a5d6e7f80a02"
)

var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// fakeTaste serves one household's Autopilot profile.
type fakeTaste struct {
	profile recommendations.Profile
	err     error
}

func (f fakeTaste) Profile(context.Context, string) (recommendations.Profile, error) {
	return f.profile, f.err
}

func (f fakeTaste) TimeBands(context.Context, string) (autopilot.TimeBands, error) {
	return autopilot.TimeBands{}, f.err
}

type harness struct {
	service *Service
	store   *memoryStore
	library *fakeLibrary
}

func newHarness(t *testing.T, configure func(*ServiceOptions)) *harness {
	t.Helper()
	store, library := newMemoryStore(), newFakeLibrary()
	opts := ServiceOptions{Store: store, Library: library, Now: func() time.Time { return testNow }}
	if configure != nil {
		configure(&opts)
	}
	return &harness{service: NewService(opts), store: store, library: library}
}

// libraryRecipe builds a household recipe the way an import would.
func libraryRecipe(source, sourceID, name string, ingredientNames ...string) recipes.Recipe {
	r := recipes.Recipe{
		Source: source, SourceRecipeID: sourceID, Name: name,
		HouseholdID: hhAda, TimesOrdered: 7, LastOrderedWeek: "2026-W04", OrderWeeks: []string{"2026-W04"},
		Servings: []int{2}, TotalMinutes: 30,
	}
	for _, n := range ingredientNames {
		r.Ingredients = append(r.Ingredients, recipes.RecipeIngredient{
			IngredientID: "66e5a1f2c3b4a5d6e7f8ff01", Name: n,
			Amounts: []recipes.Amount{{Servings: 2, Quantity: "1", Unit: "cup"}},
		})
	}
	return r
}

func TestPublishRecipesKeepsHouseholdDataOut(t *testing.T) {
	h := newHarness(t, nil)
	in := libraryRecipe(recipes.SourceHelloFresh, "hf-1", "Harissa Chicken", "harissa", "chicken")
	in.SharedToCatalog = true

	n, err := h.service.PublishRecipes(t.Context(), []recipes.Recipe{in})
	if err != nil || n != 1 {
		t.Fatalf("PublishRecipes = %d, %v; want 1, nil", n, err)
	}
	entries, _, err := h.store.Search(t.Context(), Filter{Limit: 10})
	if err != nil || len(entries) != 1 {
		t.Fatalf("Search = %d entries, %v; want 1, nil", len(entries), err)
	}
	got := entries[0]
	if got.CatalogKey != "hellofresh:hf-1" {
		t.Errorf("catalogKey = %q; want hellofresh:hf-1", got.CatalogKey)
	}
	switch {
	case got.Content.HouseholdID != "":
		t.Error("catalog entry carries a householdId")
	case got.Content.TimesOrdered != 0, got.Content.LastOrderedWeek != "", len(got.Content.OrderWeeks) != 0:
		t.Errorf("catalog entry carries order history: %+v", got.Content)
	case got.Content.SharedToCatalog:
		t.Error("catalog entry carries the household's sharing flag")
	}
}

func TestPublishRecipesDropsPrivateSources(t *testing.T) {
	h := newHarness(t, nil)
	typed := libraryRecipe(recipes.SourceManual, "m-1", "Grandma's Chili", "beans")
	pasted := libraryRecipe(recipes.SourceUser, "u-1", "Blog Pasta", "pasta")
	shared := libraryRecipe(recipes.SourceManual, "m-2", "Shared Soup", "stock")
	shared.SharedToCatalog = true

	n, err := h.service.PublishRecipes(t.Context(), []recipes.Recipe{typed, pasted, shared})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("published %d recipes; want only the shared one", n)
	}
	entries, _, _ := h.store.Search(t.Context(), Filter{Limit: 10})
	if len(entries) != 1 || entries[0].Content.Name != "Shared Soup" {
		t.Fatalf("catalog holds %+v; want only Shared Soup", entries)
	}
}

func TestPublishRecipesCollapsesTheSameRecipeFromTwoHouseholds(t *testing.T) {
	h := newHarness(t, nil)
	ada := libraryRecipe(recipes.SourceHelloFresh, "hf-9", "Pork Tacos", "pork")
	bob := libraryRecipe(recipes.SourceHelloFresh, "hf-9", "Pork Tacos", "pork")
	bob.HouseholdID, bob.TimesOrdered = hhBob, 2

	if _, err := h.service.PublishRecipes(t.Context(), []recipes.Recipe{ada}); err != nil {
		t.Fatal(err)
	}
	n, err := h.service.PublishRecipes(t.Context(), []recipes.Recipe{bob})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("re-publishing the same content wrote %d entries; want 0", n)
	}
	entries, total, _ := h.store.Search(t.Context(), Filter{Limit: 10})
	if total != 1 || len(entries) != 1 {
		t.Fatalf("catalog holds %d entries; want 1", total)
	}
}

func TestPublishOneRejectsAPrivateRecipe(t *testing.T) {
	h := newHarness(t, nil)
	err := h.service.PublishOne(t.Context(), libraryRecipe(recipes.SourceManual, "m-3", "Private", "salt"))
	if err == nil {
		t.Fatal("PublishOne accepted a private recipe")
	}
}

func TestDiscoverExcludesTheHouseholdsOwnRecipes(t *testing.T) {
	h := newHarness(t, nil)
	publish(t, h, "hf-1", "Alpha Bowl")
	publish(t, h, "hf-2", "Beta Bowl")
	publish(t, h, "hf-3", "Gamma Bowl")
	h.library.have[hhAda] = map[string]string{"hellofresh:hf-2": "own-2"}

	page, err := h.service.Discover(t.Context(), hhAda, Query{})
	if err != nil {
		t.Fatal(err)
	}
	names := resultNames(page)
	if slices.Contains(names, "Beta Bowl") {
		t.Errorf("discover returned a recipe the household already has: %v", names)
	}
	if len(names) != 2 {
		t.Errorf("discover returned %v; want the two the household lacks", names)
	}
	for _, it := range page.Items {
		if it.LibraryRecipeID != "" {
			t.Errorf("%q is marked as in the library but was returned by discover", it.Content.Name)
		}
	}
}

func TestDiscoverRanksWithTheAutopilotProfile(t *testing.T) {
	profile := recommendations.Profile{
		Version: 1,
		Taste: recommendations.Taste{
			Likes:    recommendations.Choices{Cuisines: []string{"thai"}},
			Dislikes: recommendations.Choices{Cuisines: []string{"italian"}},
		},
	}
	h := newHarness(t, func(o *ServiceOptions) { o.Taste = fakeTaste{profile: profile} })
	publishWith(t, h, "hf-1", "Zucchini Pasta", func(r *recipes.Recipe) { r.Cuisines = []string{"Italian"} })
	publishWith(t, h, "hf-2", "Mango Salad", nil)
	publishWith(t, h, "hf-3", "Thai Green Curry", func(r *recipes.Recipe) { r.Cuisines = []string{"Thai"} })

	page, err := h.service.Discover(t.Context(), hhAda, Query{})
	if err != nil {
		t.Fatal(err)
	}
	names := resultNames(page)
	want := []string{"Thai Green Curry", "Mango Salad", "Zucchini Pasta"}
	if !slices.Equal(names, want) {
		t.Fatalf("discover order = %v; want %v", names, want)
	}
	if len(page.Items[0].Reasons) == 0 {
		t.Error("the top result has no reason")
	}
}

func TestDiscoverExcludesRestrictedRecipes(t *testing.T) {
	profile := recommendations.Profile{
		Version:      1,
		Restrictions: recommendations.Restrictions{ExcludedIngredients: []string{"Peanuts"}},
	}
	h := newHarness(t, func(o *ServiceOptions) { o.Taste = fakeTaste{profile: profile} })
	publishWith(t, h, "hf-1", "Peanut Noodles", func(r *recipes.Recipe) {
		r.Ingredients = append(r.Ingredients, recipes.RecipeIngredient{
			IngredientID: "66e5a1f2c3b4a5d6e7f8ff02", Name: "peanuts",
		})
	})
	publishWith(t, h, "hf-2", "Sesame Noodles", nil)

	page, err := h.service.Discover(t.Context(), hhAda, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if names := resultNames(page); !slices.Equal(names, []string{"Sesame Noodles"}) {
		t.Fatalf("discover returned %v; want only Sesame Noodles", names)
	}
}

func TestDiscoverIsAlphabeticalWithoutAProfile(t *testing.T) {
	h := newHarness(t, nil)
	publish(t, h, "hf-3", "cardamom buns")
	publish(t, h, "hf-1", "Apple Tart")
	publish(t, h, "hf-2", "Berry Crumble")

	page, err := h.service.Discover(t.Context(), hhAda, Query{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Apple Tart", "Berry Crumble", "cardamom buns"}
	if names := resultNames(page); !slices.Equal(names, want) {
		t.Fatalf("cold-start order = %v; want %v", names, want)
	}
}

func TestDiscoverPagesDeterministically(t *testing.T) {
	h := newHarness(t, nil)
	for _, name := range []string{"A", "B", "C", "D", "E"} {
		publish(t, h, "hf-"+name, name)
	}
	first, err := h.service.Discover(t.Context(), hhAda, Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == "" || first.Total != 5 {
		t.Fatalf("first page = %+v; want a cursor and total 5", first)
	}
	second, err := h.service.Discover(t.Context(), hhAda, Query{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if got := append(resultNames(first), resultNames(second)...); !slices.Equal(got, []string{"A", "B", "C", "D"}) {
		t.Fatalf("paged names = %v", got)
	}
}

func TestSearchMarksTheHouseholdsOwnRecipes(t *testing.T) {
	h := newHarness(t, nil)
	publish(t, h, "hf-1", "Tikka Masala")
	publish(t, h, "hf-2", "Pad Thai")
	h.library.have[hhAda] = map[string]string{"hellofresh:hf-1": "own-1"}

	page, err := h.service.Search(t.Context(), hhAda, Query{Text: "tikka"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("search returned %v; want one hit", resultNames(page))
	}
	if page.Items[0].LibraryRecipeID != "own-1" {
		t.Errorf("libraryRecipeId = %q; want own-1", page.Items[0].LibraryRecipeID)
	}
}

func TestSearchRejectsABadCursor(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.service.Search(t.Context(), hhAda, Query{Cursor: "not-a-cursor"}); err == nil {
		t.Fatal("a malformed cursor was accepted")
	}
}

func TestAddCopiesIntoTheLibraryAndIsIdempotent(t *testing.T) {
	h := newHarness(t, nil)
	id := publish(t, h, "hf-1", "Shakshuka")

	stored, created, err := h.service.Add(t.Context(), hhAda, id)
	if err != nil || !created {
		t.Fatalf("Add = %v, created %v, %v; want created", stored.ID, created, err)
	}
	again, created, err := h.service.Add(t.Context(), hhAda, id)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("adding the same recipe twice created a second copy")
	}
	if again.ID != stored.ID {
		t.Errorf("second add returned %q; want the first copy %q", again.ID, stored.ID)
	}
}

func TestAddReportsAMissingEntry(t *testing.T) {
	h := newHarness(t, nil)
	if _, _, err := h.service.Add(t.Context(), hhAda, "66e5a1f2c3b4a5d6e7f8dead"); err == nil {
		t.Fatal("Add accepted an unknown catalog ID")
	}
}

// --- helpers ------------------------------------------------------------------

func publish(t *testing.T, h *harness, sourceID, name string) string {
	t.Helper()
	return publishWith(t, h, sourceID, name, nil)
}

func publishWith(t *testing.T, h *harness, sourceID, name string, edit func(*recipes.Recipe)) string {
	t.Helper()
	r := libraryRecipe(recipes.SourceHelloFresh, sourceID, name, "olive oil")
	if edit != nil {
		edit(&r)
	}
	if _, err := h.service.PublishRecipes(t.Context(), []recipes.Recipe{r}); err != nil {
		t.Fatal(err)
	}
	entries, _, err := h.store.Search(t.Context(), Filter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.CatalogKey == recipes.CatalogKey(r) {
			return e.ID
		}
	}
	t.Fatalf("published recipe %q is not in the store", name)
	return ""
}

func resultNames(p Page) []string {
	out := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, it.Content.Name)
	}
	return out
}
