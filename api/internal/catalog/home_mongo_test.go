package catalog

import (
	"errors"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func sortedNames(entries []Recipe) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Content.Name)
	}
	slices.Sort(out)
	return out
}

func TestIntegrationFiltersAndAddOns(t *testing.T) {
	store := newTestMongoStore(t)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	quick := mongoContent("Quick Tacos", "tortillas")
	quick.TotalMinutes, quick.Cuisines = 20, []string{"Mexican"}
	long := mongoContent("Slow Pork", "pork shoulder")
	long.PrepMinutes, long.TotalMinutes, long.Servings, long.Cuisines = 15, 240, []int{4, 8}, []string{"mexican "}
	unknown := mongoContent("Mystery Stew", "beans")
	unknown.TotalMinutes = 0
	addon := mongoContent("Quick Garlic Bread", "bread")
	addon.IsAddon, addon.TotalMinutes, addon.Cuisines = true, 10, []string{"Italian"}
	if _, err := store.Upsert(t.Context(), []Recipe{
		{CatalogKey: "import:q", Content: quick}, {CatalogKey: "import:l", Content: long},
		{CatalogKey: "import:u", Content: unknown}, {CatalogKey: "import:a", Content: addon},
	}, now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		f    Filter
		want []string
	}{
		{"everything but the add-on", Filter{}, []string{"Mystery Stew", "Quick Tacos", "Slow Pork"}},
		{"30 min or less leaves unknown times out", Filter{MaxMinutes: 30}, []string{"Quick Tacos"}},
		{"total time is the larger one", Filter{MaxMinutes: 200}, []string{"Quick Tacos"}},
		{"6+", Filter{MinServings: 6}, []string{"Slow Pork"}},
		{"cuisine spellings", Filter{Cuisine: "mexican", CuisineAny: []string{"mexican"}}, []string{"Quick Tacos", "Slow Pork"}},
		{"text never finds an add-on", Filter{Text: "garlic"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.f.Limit = 50
			got, total, err := store.Search(t.Context(), tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if n := sortedNames(got); !slices.Equal(n, tc.want) || total != len(tc.want) {
				t.Errorf("Search = %v (%d); want %v", n, total, tc.want)
			}
		})
	}
	scanned, err := store.Scan(t.Context(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if n := sortedNames(scanned); slices.Contains(n, "Quick Garlic Bread") || len(n) != 3 {
		t.Errorf("Scan = %v", n)
	}
	cuisines, err := store.Cuisines(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(cuisines)
	if !slices.Equal(cuisines, []string{"Mexican", "mexican "}) {
		t.Errorf("Cuisines = %q; want the stored spellings, no add-on's", cuisines)
	}
}

func TestIntegrationSavesCountHouseholdsInAWindow(t *testing.T) {
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(t.Context(), SaveIndexes()...); err != nil {
		t.Fatal(err)
	}
	saves := NewMongoSaves(client.Database())
	now := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	const cat = "66e5a1f2c3b4a5d6e7f80c01"
	for _, s := range []Save{
		{CatalogID: cat, Group: "g1", HouseholdID: hhAda, At: now},
		{CatalogID: cat, Group: "g1", HouseholdID: hhAda, At: now}, // saved again after a delete
		{CatalogID: cat, Group: "g1", HouseholdID: hhBob, At: now.Add(-10 * 24 * time.Hour)},
		{CatalogID: cat, Group: "g2", HouseholdID: hhBob, At: now},
	} {
		if err := saves.Record(t.Context(), s); err != nil {
			t.Fatal(err)
		}
	}
	all, err := saves.Counts(t.Context(), time.Time{})
	if err != nil || all["g1"] != 2 || all["g2"] != 1 {
		t.Errorf("all = %v, %v", all, err)
	}
	week, err := saves.Counts(t.Context(), now.Add(-7*24*time.Hour))
	if err != nil || week["g1"] != 1 || week["g2"] != 1 {
		t.Errorf("week = %v, %v", week, err)
	}
	if err := saves.PurgeHousehold(t.Context(), hhAda); err != nil {
		t.Fatal(err)
	}
	if all, _ := saves.Counts(t.Context(), time.Time{}); all["g1"] != 1 {
		t.Errorf("after purge = %v", all)
	}
}

func TestIntegrationRatingsJoinThroughCatalogKey(t *testing.T) {
	client := mongotest.Client(t)
	db := client.Database()
	r1, r2, r3, gone := bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID()
	h1, h2 := bson.NewObjectID(), bson.NewObjectID()
	if _, err := db.Collection(recipes.RecipesCollection).InsertMany(t.Context(), []any{
		bson.D{{Key: "_id", Value: r1}, {Key: "householdId", Value: h1}, {Key: "catalogKey", Value: "import:k"}},
		bson.D{{Key: "_id", Value: r2}, {Key: "householdId", Value: h2}, {Key: "catalogKey", Value: "import:k"}},
		bson.D{{Key: "_id", Value: r3}, {Key: "householdId", Value: h2}},
		bson.D{{Key: "_id", Value: gone}, {Key: "householdId", Value: h2}, {Key: "catalogKey", Value: "import:k"}, {Key: "deletedAt", Value: time.Now()}},
	}); err != nil {
		t.Fatal(err)
	}
	rate := func(h, r bson.ObjectID, score int) bson.D {
		return bson.D{{Key: "householdId", Value: h}, {Key: "recipeId", Value: r}, {Key: "userId", Value: bson.NewObjectID()}, {Key: "score", Value: score}}
	}
	if _, err := db.Collection(RatingsCollection).InsertMany(t.Context(), []any{
		rate(h1, r1, 5), rate(h1, r1, 3), // one household, mean 4
		rate(h2, r2, 5),
		rate(h2, r3, 1),   // no catalogKey: not a shared recipe
		rate(h2, gone, 1), // deleted copy
	}); err != nil {
		t.Fatal(err)
	}
	got, err := NewMongoRatings(db).CatalogRatings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st := got["import:k"]; st.Households != 2 || st.Sum != 9 || len(got) != 1 {
		t.Errorf("ratings = %+v", got)
	}
}

func TestIntegrationCollectionsCRUD(t *testing.T) {
	client := mongotest.Client(t)
	store := NewMongoCollections(client.Database())
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	c := Collection{
		ID: "pasta-week", Title: "Pasta Week", Type: TypeSponsored, RecipeIDs: []string{"66e5a1f2c3b4a5d6e7f80c01"},
		StartsAt: start, EndsAt: start.Add(7 * 24 * time.Hour), Position: 3,
		Sponsor: &Sponsor{Name: "Acme", URL: "https://example.com"}, UpdatedAt: start,
	}
	if err := store.Put(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	c.Title = "Pasta Week Two"
	if err := store.Put(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(t.Context())
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %+v, %v", list, err)
	}
	got := list[0]
	if got.Title != "Pasta Week Two" || !got.StartsAt.Equal(start) || got.Sponsor == nil || got.Sponsor.Name != "Acme" || got.RecipeIDs[0] != c.RecipeIDs[0] {
		t.Errorf("stored = %+v", got)
	}
	if err := store.Delete(t.Context(), "pasta-week"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), "pasta-week"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
}
