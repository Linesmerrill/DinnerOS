package catalog

import (
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func newTestMongoStore(t *testing.T) *MongoStore {
	t.Helper()
	client := mongotest.Client(t)
	// Twice, to prove applying the indexes is idempotent.
	for range 2 {
		if err := client.EnsureIndexes(t.Context(), Indexes()...); err != nil {
			t.Fatalf("ensure indexes: %v", err)
		}
	}
	return NewMongoStore(client.Database())
}

func TestIntegrationIndexesAreNamed(t *testing.T) {
	store := newTestMongoStore(t)
	specs, err := store.entries.Indexes().ListSpecifications(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	have := make([]string, 0, len(specs))
	for _, s := range specs {
		have = append(have, s.Name)
	}
	for _, set := range Indexes() {
		for _, model := range set.Indexes {
			// Every declared index is named explicitly.
			var opts options.IndexOptions
			for _, apply := range model.Options.List() {
				if err := apply(&opts); err != nil {
					t.Fatal(err)
				}
			}
			if opts.Name == nil {
				t.Fatalf("index %v has no explicit name", model.Keys)
			}
			if !slices.Contains(have, *opts.Name) {
				t.Errorf("index %q is declared but not created; have %v", *opts.Name, have)
			}
		}
	}
}

func TestIntegrationUpsertIsIdempotentAndKeepsFirstPublishedAt(t *testing.T) {
	store := newTestMongoStore(t)
	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	later := first.Add(72 * time.Hour)
	entry := Recipe{CatalogKey: "hellofresh:hf-1", Content: mongoContent("Harissa Chicken", "harissa", "chicken thighs")}

	n, err := store.Upsert(t.Context(), []Recipe{entry}, first)
	if err != nil || n != 1 {
		t.Fatalf("first Upsert = %d, %v; want 1, nil", n, err)
	}
	if n, err = store.Upsert(t.Context(), []Recipe{entry}, later); err != nil || n != 0 {
		t.Fatalf("unchanged Upsert = %d, %v; want 0, nil", n, err)
	}

	changed := entry
	changed.Content.Headline = "Now with more harissa"
	if n, err = store.Upsert(t.Context(), []Recipe{changed}, later); err != nil || n != 1 {
		t.Fatalf("changed Upsert = %d, %v; want 1, nil", n, err)
	}
	found, total, err := store.Search(t.Context(), Filter{Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("Search = %d total, %v; want 1, nil", total, err)
	}
	got := found[0]
	if !got.FirstPublishedAt.Equal(first) {
		t.Errorf("firstPublishedAt = %v; want the original %v", got.FirstPublishedAt, first)
	}
	if !got.UpdatedAt.Equal(later) {
		t.Errorf("updatedAt = %v; want %v", got.UpdatedAt, later)
	}
}

func TestIntegrationSearchByNameIngredientAndTag(t *testing.T) {
	store := newTestMongoStore(t)
	now := time.Now().UTC()
	entries := []Recipe{
		{CatalogKey: "hellofresh:a", Content: withLabels(mongoContent("Harissa Chicken", "harissa paste", "chicken"), "Moroccan", "Spicy")},
		{CatalogKey: "hellofresh:b", Content: withLabels(mongoContent("Lemon Salmon", "salmon", "lemon"), "Nordic", "Quick")},
		{CatalogKey: "hellofresh:c", Content: withLabels(mongoContent("Chicken Tacos", "chicken", "tortillas"), "Tex-Mex", "Quick")},
	}
	if _, err := store.Upsert(t.Context(), entries, now); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		filter Filter
		want   []string
	}{
		{"by name", Filter{Text: "salmon", Limit: 10}, []string{"Lemon Salmon"}},
		{"by ingredient text", Filter{Text: "tortillas", Limit: 10}, []string{"Chicken Tacos"}},
		{"by exact ingredient", Filter{Ingredient: "Chicken", Limit: 10}, []string{"Chicken Tacos", "Harissa Chicken"}},
		{"by tag, case-insensitively", Filter{Tag: "quick", Limit: 10}, []string{"Chicken Tacos", "Lemon Salmon"}},
		{"by cuisine, case-insensitively", Filter{Cuisine: "tex-mex", Limit: 10}, []string{"Chicken Tacos"}},
		{"everything, alphabetically", Filter{Limit: 10}, []string{"Chicken Tacos", "Harissa Chicken", "Lemon Salmon"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, _, err := store.Search(t.Context(), tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(found))
			for _, e := range found {
				got = append(got, e.Content.Name)
			}
			if tc.filter.Text != "" {
				// Relevance order, so compare as a set.
				slices.Sort(got)
				want := slices.Clone(tc.want)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Fatalf("hits = %v; want %v", got, want)
				}
				return
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("hits = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestIntegrationSearchPagesByOffset(t *testing.T) {
	store := newTestMongoStore(t)
	now := time.Now().UTC()
	var entries []Recipe
	for _, name := range []string{"A", "B", "C", "D"} {
		entries = append(entries, Recipe{CatalogKey: "hellofresh:" + name, Content: mongoContent(name, "olive oil")})
	}
	if _, err := store.Upsert(t.Context(), entries, now); err != nil {
		t.Fatal(err)
	}
	found, total, err := store.Search(t.Context(), Filter{Offset: 2, Limit: 2})
	if err != nil || total != 4 || len(found) != 2 {
		t.Fatalf("Search = %d hits of %d, %v; want 2 of 4", len(found), total, err)
	}
	if found[0].Content.Name != "C" || found[1].Content.Name != "D" {
		t.Fatalf("page = %q, %q; want C, D", found[0].Content.Name, found[1].Content.Name)
	}
}

func TestIntegrationScanDropsStepsAndDescription(t *testing.T) {
	store := newTestMongoStore(t)
	content := mongoContent("Shakshuka", "eggs")
	content.Description = "A long description"
	content.Steps = []recipes.Step{{Index: 1, Text: "Crack the eggs"}}
	if _, err := store.Upsert(t.Context(), []Recipe{{CatalogKey: "hellofresh:s", Content: content}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	found, err := store.Scan(t.Context(), 10)
	if err != nil || len(found) != 1 {
		t.Fatalf("Scan = %d entries, %v; want 1", len(found), err)
	}
	if found[0].Content.Description != "" || len(found[0].Content.Steps) != 0 {
		t.Errorf("Scan returned steps or a description: %+v", found[0].Content)
	}
	if len(found[0].Content.Ingredients) == 0 {
		t.Error("Scan dropped the ingredients, which discovery filters on")
	}
}

func TestIntegrationStoredDocumentHasNoHouseholdFields(t *testing.T) {
	store := newTestMongoStore(t)
	content := mongoContent("Pork Tacos", "pork")
	content.HouseholdID = hhAda
	content.TimesOrdered, content.LastOrderedWeek = 9, "2026-W07"
	content.OrderWeeks = []string{"2026-W07"}
	content.SharedToCatalog = true
	if _, err := store.Upsert(t.Context(), []Recipe{{CatalogKey: "hellofresh:p", Content: content}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := store.entries.FindOne(t.Context(), map[string]any{"catalogKey": "hellofresh:p"}).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"householdId", "timesOrdered", "lastOrderedWeek", "orderWeeks", "sharedToCatalog"} {
		if _, ok := raw[forbidden]; ok {
			t.Errorf("catalog document stores %q", forbidden)
		}
	}
}

// --- helpers ------------------------------------------------------------------

func mongoContent(name string, ingredientNames ...string) recipes.Recipe {
	r := recipes.Recipe{Source: recipes.SourceHelloFresh, Name: name, Servings: []int{2}, TotalMinutes: 30}
	for _, n := range ingredientNames {
		r.Ingredients = append(r.Ingredients, recipes.RecipeIngredient{
			IngredientID: "66e5a1f2c3b4a5d6e7f8ff01", Name: n,
			Amounts: []recipes.Amount{{Servings: 2, Quantity: "1", Unit: "cup"}},
		})
	}
	return r
}

func withLabels(r recipes.Recipe, cuisine, tag string) recipes.Recipe {
	r.Cuisines, r.Tags = []string{cuisine}, []string{tag}
	return r
}
