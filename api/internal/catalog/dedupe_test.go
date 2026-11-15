package catalog

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Decision 689: Shared Recipes shows each real recipe once. The store cases
// run against the in-memory store here and against MongoDB in
// TestIntegrationMongoDedupe.

var dedupeT0 = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

func TestMemoryStoreDedupe(t *testing.T) {
	runDedupeStoreCases(t, func(*testing.T) Store { return newMemoryStore() })
}

func TestIntegrationMongoDedupe(t *testing.T) {
	runDedupeStoreCases(t, func(t *testing.T) Store { return newTestMongoStore(t) })
}

// put stores each content as its own entry (its own CatalogKey), published
// an hour apart in order, as copies arriving from different households.
func put(t *testing.T, store Store, contents ...recipes.Recipe) []string {
	t.Helper()
	var ids []string
	for i, c := range contents {
		key := fmt.Sprintf("test:%d:%s", i, c.Name)
		if _, err := store.Upsert(t.Context(), []Recipe{{CatalogKey: key, Content: c}}, dedupeT0.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, idOfKey(t, store, key))
	}
	return ids
}

// idOfKey finds the ID of the entry stored under key.
func idOfKey(t *testing.T, store Store, key string) string {
	t.Helper()
	switch s := store.(type) {
	case *memoryStore:
		for _, e := range s.entries {
			if e.CatalogKey == key {
				return e.ID
			}
		}
	case *MongoStore:
		var d entryDoc
		if err := s.entries.FindOne(t.Context(), map[string]any{"catalogKey": key}).Decode(&d); err != nil {
			t.Fatal(err)
		}
		return d.ID.Hex()
	}
	t.Fatalf("no entry under %s", key)
	return ""
}

func searchAll(t *testing.T, store Store, f Filter) ([]Recipe, int) {
	t.Helper()
	if f.Limit == 0 {
		f.Limit = MaxLimit
	}
	found, total, err := store.Search(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	return found, total
}

func runDedupeStoreCases(t *testing.T, newStore func(*testing.T) Store) {
	count := func(t *testing.T, store Store) (search, scan int) {
		t.Helper()
		found, total := searchAll(t, store, Filter{})
		if len(found) != total {
			t.Errorf("search returned %d items but counted %d", len(found), total)
		}
		scanned, err := store.Scan(t.Context(), ScanLimit)
		if err != nil {
			t.Fatal(err)
		}
		return total, len(scanned)
	}
	for _, tc := range []struct {
		name     string
		contents []recipes.Recipe
		want     int
	}{
		{"Jane's and Paul's brownies: same title, different ingredients", []recipes.Recipe{
			typed("Brownies", []string{"Bake"}, "butter", "cocoa", "sugar"),
			typed("Brownies", []string{"Bake"}, "coconut oil", "cocoa", "maple syrup"),
		}, 2},
		{"two imports of the same link and title", []recipes.Recipe{
			dish(recipes.SourceUser, "r1", "https://example.com/chili", "Chili", "beans", "beef"),
			dish(recipes.SourceUser, "r2", "https://example.com/chili", "  chili ", "kidney beans", "ground beef"),
		}, 1},
		{"one blog post with two recipes: same link, different titles", []recipes.Recipe{
			dish(recipes.SourceUser, "r1", "https://example.com/2026/10/fall-dinners", "Chili", "beans"),
			dish(recipes.SourceUser, "r2", "https://www.example.com/2026/10/fall-dinners/", "Cornbread", "cornmeal"),
		}, 2},
		{"a site's home page shared by two recipes", []recipes.Recipe{
			dish(recipes.SourceUser, "r1", "https://example.com/", "Chili", "beans"),
			dish(recipes.SourceUser, "r2", "https://www.example.com", "Chili", "black beans"),
		}, 2},
		{"a recipe index shared by two recipes", []recipes.Recipe{
			dish(recipes.SourceUser, "r1", "https://example.com/recipes", "Chili", "beans"),
			dish(recipes.SourceUser, "r2", "https://example.com/recipes/dinner/", "Chili", "black beans"),
		}, 2},
		{"a search page shared by two recipes", []recipes.Recipe{
			dish(recipes.SourceUser, "r1", "https://example.com/search?q=chili", "Chili", "beans"),
			dish(recipes.SourceUser, "r2", "https://example.com/search?q=chili", "Chili", "black beans"),
		}, 2},
		{"a generic link and the same importer id", []recipes.Recipe{
			dish(recipes.SourceHelloFresh, "5f1", "https://www.hellofresh.com/recipes", "Pork Tacos", "pork"),
			dish(recipes.SourceHelloFresh, "5f1", "https://www.hellofresh.com/", "Pork Tacos", "pork", "lime"),
		}, 1},
		{"a generic link and different importer ids", []recipes.Recipe{
			dish(recipes.SourceHelloFresh, "5f1", "https://www.hellofresh.com/recipes", "Pork Tacos", "pork"),
			dish(recipes.SourceHelloFresh, "5f2", "https://www.hellofresh.com/recipes", "Beef Tacos", "beef"),
		}, 2},
		{"the same link with tracking and www", []recipes.Recipe{
			dish(recipes.SourceUser, "r1", "https://example.com/chili", "Chili", "beans"),
			dish(recipes.SourceUser, "r2", "http://www.Example.com/chili/?utm_source=pin&fbclid=x#top", "Chili", "black beans"),
			dish(recipes.SourceUser, "r3", "https://example.com/chili?gclid=9&utm_campaign=fall", "CHILI", "pinto beans"),
		}, 1},
		{"the same link with a different recipe parameter", []recipes.Recipe{
			dish(recipes.SourceUser, "r1", "https://example.com/r?id=1", "Chili", "beans"),
			dish(recipes.SourceUser, "r2", "https://example.com/r?id=2", "Chili", "beans"),
		}, 2},
		{"typed recipes that differ only in case and spacing", []recipes.Recipe{
			typed("Fudgy Brownies", []string{"Melt the butter.", "Bake 25 minutes."}, "butter", "Cocoa Powder"),
			typed("  fudgy  brownies", []string{"melt the  butter.", "BAKE 25 minutes."}, "BUTTER", "cocoa   powder"),
		}, 1},
		{"same title and ingredients, different steps", []recipes.Recipe{
			typed("Brownies", []string{"Bake 20 minutes."}, "butter", "cocoa"),
			typed("Brownies", []string{"Bake 35 minutes for a cakey middle."}, "butter", "cocoa"),
		}, 2},
		{"six copies of one HelloFresh import", []recipes.Recipe{
			dish(recipes.SourceUser, "a", "https://www.hellofresh.com/recipes/pork-tacos-5f1", "Pork Tacos", "pork"),
			dish(recipes.SourceUser, "b", "https://hellofresh.com/recipes/pork-tacos-5f1/", "Pork Tacos", "ground pork"),
			dish(recipes.SourceUser, "c", "https://hellofresh.com/recipes/pork-tacos-5f1?utm_medium=email", "Pork Tacos", "pork", "tortillas"),
			dish(recipes.SourceUser, "d", "http://hellofresh.com/recipes/pork-tacos-5f1#steps", "Pork Tacos", "pork", "lime"),
			dish(recipes.SourceUser, "e", "https://hellofresh.com/recipes/pork-tacos-5f1?mc_cid=1&mc_eid=2", "Pork Tacos", "pork", "salsa"),
			dish(recipes.SourceUser, "f", "hellofresh.com/recipes/pork-tacos-5f1", "Pork Tacos", "pork", "crema"),
		}, 1},
		{"six HelloFresh releases of one recipe: new ids, one link", []recipes.Recipe{
			dish(recipes.SourceHelloFresh, "6447d2af33e34d0ea5018115", "https://www.hellofresh.com/recipes/garlicky-shrimp-couscous-bowls-6447d2af33e34d0ea5018115?utm_source=email&utm_medium=menu", "Garlicky Shrimp Couscous Bowls", "shrimp", "couscous"),
			dish(recipes.SourceHelloFresh, "64a0c1e2f3a4b5c6d7e80001", "https://www.hellofresh.com/recipes/garlicky-shrimp-couscous-bowls-6447d2af33e34d0ea5018115", "Garlicky Shrimp Couscous Bowls", "shrimp", "couscous"),
			dish(recipes.SourceHelloFresh, "64a0c1e2f3a4b5c6d7e80002", "https://www.hellofresh.com/recipes/garlicky-shrimp-couscous-bowls-6447d2af33e34d0ea5018115", "Garlicky Shrimp Couscous Bowls", "shrimp", "couscous"),
			dish(recipes.SourceHelloFresh, "64a0c1e2f3a4b5c6d7e80003", "https://www.hellofresh.com/recipes/garlicky-shrimp-couscous-bowls-6447d2af33e34d0ea5018115", "Garlicky Shrimp Couscous Bowls", "shrimp", "couscous", "lemon"),
			dish(recipes.SourceHelloFresh, "64a0c1e2f3a4b5c6d7e80004", "https://www.hellofresh.com/recipes/garlicky-shrimp-couscous-bowls-6447d2af33e34d0ea5018115", "Garlicky Shrimp Couscous Bowls", "shrimp", "couscous"),
			dish(recipes.SourceHelloFresh, "64a0c1e2f3a4b5c6d7e80005", "https://www.hellofresh.com/recipes/garlicky-shrimp-couscous-bowls-6447d2af33e34d0ea5018115", "Garlicky Shrimp Couscous Bowls", "shrimp", "couscous"),
		}, 1},
		{"two HelloFresh recipes: different ids, different links", []recipes.Recipe{
			dish(recipes.SourceHelloFresh, "6447d2af33e34d0ea5018115", "https://www.hellofresh.com/recipes/garlicky-shrimp-couscous-bowls-6447d2af33e34d0ea5018115", "Garlicky Shrimp Couscous Bowls", "shrimp"),
			dish(recipes.SourceHelloFresh, "64a0c1e2f3a4b5c6d7e89999", "https://www.hellofresh.com/recipes/pork-tacos-64a0c1e2f3a4b5c6d7e89999", "Pork Tacos", "pork"),
		}, 2},
		{"the same importer recipe id and no link", []recipes.Recipe{
			dish(recipes.SourceHelloFresh, "5f1", "", "Pork Tacos", "pork"),
			dish("HelloFresh", "5f1", "", "Pork Tacos", "pork", "lime"),
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(t)
			put(t, store, tc.contents...)
			if search, scan := count(t, store); search != tc.want || scan != tc.want {
				t.Errorf("search shows %d and discovery %d; want %d", search, scan, tc.want)
			}
		})
	}

	t.Run("the most complete copy represents the group", func(t *testing.T) {
		store := newStore(t)
		url := "https://example.com/pie"
		bare := dish(recipes.SourceUser, "a", url, "Apple Pie", "apples")
		withPhoto := dish(recipes.SourceUser, "b", url, "Apple Pie", "apples", "flour")
		withPhoto.ImageURL = "https://example.com/pie.jpg"
		full := dish(recipes.SourceUser, "c", url, "Apple Pie", "apples", "butter")
		full.ImageURL, full.Steps = "https://example.com/pie.jpg", []recipes.Step{{Index: 1, Text: "Bake"}}
		fullLater := dish(recipes.SourceUser, "d", url, "Apple Pie", "apples", "sugar")
		fullLater.ImageURL, fullLater.Steps = "https://example.com/pie2.jpg", []recipes.Step{{Index: 1, Text: "Bake"}}
		ids := put(t, store, bare, withPhoto, full, fullLater)

		found, _ := searchAll(t, store, Filter{})
		if len(found) != 1 || found[0].ID != ids[2] {
			t.Fatalf("search shows %v; want only the first copy with a photo and steps (%s)", names(found), ids[2])
		}
		if len(found[0].Content.Steps) != 1 {
			t.Error("search's representative lost its steps")
		}
		if len(found[0].GroupKeys) != 4 {
			t.Errorf("group keys = %v; want all four copies", found[0].GroupKeys)
		}
		scanned, _ := store.Scan(t.Context(), ScanLimit)
		if len(scanned) != 1 || scanned[0].ID != ids[2] || len(scanned[0].GroupKeys) != 4 {
			t.Errorf("discovery shows %+v; want the same copy with four keys", scanned)
		}
		// A filter that only a weaker copy matches still shows the best copy.
		found, total := searchAll(t, store, Filter{Ingredient: "flour"})
		if total != 1 || found[0].ID != ids[2] {
			t.Errorf("filtered search shows %v of %d; want the group's best copy", names(found), total)
		}
		got, err := store.Get(t.Context(), ids[0])
		if err != nil || len(got.GroupKeys) != 4 {
			t.Errorf("Get(bare copy) keys = %v, %v; want the whole group", got.GroupKeys, err)
		}
		groups, err := store.Groups(t.Context(), []string{ids[0], ids[3], "66e5a1f2c3b4a5d6e7f8ffff"})
		if err != nil || groups[ids[0]] != groups[ids[3]] || groups[ids[0]] != Fingerprint(bare) || groups["66e5a1f2c3b4a5d6e7f8ffff"] != idGroup("66e5a1f2c3b4a5d6e7f8ffff") {
			t.Errorf("Groups = %v, %v", groups, err)
		}
	})

	t.Run("paging and counts are on recipes, not copies", func(t *testing.T) {
		store := newStore(t)
		var contents []recipes.Recipe
		for _, name := range []string{"A", "B", "C", "D", "E"} {
			for copyN := range 3 {
				contents = append(contents, dish(recipes.SourceUser, fmt.Sprint(copyN), "https://example.com/"+name, name, fmt.Sprintf("thing %d", copyN)))
			}
		}
		put(t, store, contents...)
		var seen []string
		for offset := 0; offset < 6; offset += 2 {
			found, total, err := store.Search(t.Context(), Filter{Offset: offset, Limit: 2})
			if err != nil {
				t.Fatal(err)
			}
			if total != 5 {
				t.Errorf("page at %d counted %d; want 5", offset, total)
			}
			seen = append(seen, names(found)...)
		}
		if !slices.Equal(seen, []string{"A", "B", "C", "D", "E"}) {
			t.Errorf("pages = %v; want each recipe once, in order", seen)
		}
		if scanned, _ := store.Scan(t.Context(), 3); len(scanned) != 3 {
			t.Errorf("Scan(3) = %d; the limit counts recipes", len(scanned))
		}
	})

	t.Run("hidden groups are dropped before counting", func(t *testing.T) {
		store := newStore(t)
		ids := put(t, store,
			dish(recipes.SourceUser, "a", "https://example.com/x", "X", "one"),
			dish(recipes.SourceUser, "b", "https://example.com/x", "X", "two"),
			dish(recipes.SourceUser, "c", "https://example.com/y", "Y", "one"),
			typed("Z", nil, "zest"),
		)
		groups, _ := store.Groups(t.Context(), []string{ids[1], ids[3]})
		found, total := searchAll(t, store, Filter{HiddenGroups: []string{groups[ids[1]]}})
		if total != 2 || !slices.Equal(names(found), []string{"Y", "Z"}) {
			t.Errorf("search hiding X = %v of %d; want Y, Z of 2", names(found), total)
		}
		// An entry hidden by its own ID is dropped too.
		found, total = searchAll(t, store, Filter{HiddenGroups: []string{idGroup(ids[3])}})
		if total != 2 || !slices.Equal(names(found), []string{"X", "Y"}) {
			t.Errorf("search hiding Z by id = %v of %d", names(found), total)
		}
	})
}

func names(rs []Recipe) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Content.Name)
	}
	return out
}

// --- Through the service --------------------------------------------------------

// shareAs publishes a household's copy of content, as sharing does.
func shareAs(t *testing.T, h *harness, household string, content recipes.Recipe) {
	t.Helper()
	content.HouseholdID, content.SharedToCatalog = household, true
	if n, err := h.service.PublishRecipes(t.Context(), []recipes.Recipe{content}); err != nil || n != 1 {
		t.Fatalf("publish %q = %d, %v", content.Name, n, err)
	}
}

func TestSharedRecipesShowEachRecipeOnce(t *testing.T) {
	ctx := t.Context()
	h := newHarness(t, nil)
	// Jane's and Paul's brownies are two recipes.
	shareAs(t, h, hhAda, typed("Brownies", []string{"Bake"}, "butter", "cocoa"))
	shareAs(t, h, hhBob, typed("Brownies", []string{"Bake"}, "coconut oil", "cocoa", "dates"))
	// Six households pasted the same meal-kit page.
	for i, url := range []string{
		"https://www.hellofresh.com/recipes/pork-tacos-5f1",
		"https://hellofresh.com/recipes/pork-tacos-5f1/?utm_source=app",
		"http://hellofresh.com/recipes/pork-tacos-5f1",
		"https://hellofresh.com/recipes/pork-tacos-5f1#ingredients",
		"https://hellofresh.com/recipes/pork-tacos-5f1?fbclid=1",
		"hellofresh.com/recipes/pork-tacos-5f1",
	} {
		shareAs(t, h, fmt.Sprintf("66e5a1f2c3b4a5d6e7f80b%02d", i), dish(recipes.SourceUser, fmt.Sprint(i), url, "Pork Tacos", "pork", fmt.Sprintf("topping %d", i)))
	}

	page, err := h.service.Discover(ctx, "66e5a1f2c3b4a5d6e7f80c01", Query{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || !slices.Equal(names(resultRecipes(page)), []string{"Brownies", "Brownies", "Pork Tacos"}) {
		t.Errorf("discover = %v of %d; want the two brownies and the tacos once", names(resultRecipes(page)), page.Total)
	}
	search, err := h.service.Search(ctx, hhAda, Query{Text: "tacos"})
	if err != nil || search.Total != 1 || len(search.Items) != 1 {
		t.Errorf("search tacos = %+v, %v; want one", search, err)
	}

	// Paging walks recipes: two pages of two and one.
	var seen []string
	cursor := ""
	for range 3 {
		p, err := h.service.Discover(ctx, "66e5a1f2c3b4a5d6e7f80c01", Query{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, names(resultRecipes(p))...)
		if p.Total != 3 {
			t.Errorf("page total = %d; want 3", p.Total)
		}
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 3 {
		t.Errorf("pages = %v; want three recipes", seen)
	}
}

func resultRecipes(p Page) []Recipe {
	out := make([]Recipe, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, it.Recipe)
	}
	return out
}

// A household that has any copy has the recipe.
func TestAnyCopyInTheLibraryCountsAsHavingIt(t *testing.T) {
	ctx := t.Context()
	h := newHarness(t, nil)
	mine := dish(recipes.SourceUser, "a", "https://example.com/chili", "Chili", "beans")
	shareAs(t, h, hhAda, mine)
	better := dish(recipes.SourceUser, "b", "https://www.example.com/chili/", "Chili", "black beans")
	better.ImageURL = "https://example.com/chili.jpg"
	shareAs(t, h, hhBob, better)
	h.library.have[hhAda] = map[string]string{recipes.CatalogKey(mine): "66e5a1f2c3b4a5d6e7f8aaaa"}

	if page, _ := h.service.Discover(ctx, hhAda, Query{}); len(page.Items) != 0 {
		t.Errorf("discover offers Ada the chili she has: %v", names(resultRecipes(page)))
	}
	search, err := h.service.Search(ctx, hhAda, Query{Text: "chili"})
	if err != nil || len(search.Items) != 1 {
		t.Fatalf("search = %+v, %v", search, err)
	}
	if it := search.Items[0]; it.Content.ImageURL == "" || it.LibraryRecipeID != "66e5a1f2c3b4a5d6e7f8aaaa" {
		t.Errorf("search shows %+v; want Bob's copy with the photo, marked as Ada's", it)
	}
	got, err := h.service.Get(ctx, hhAda, search.Items[0].ID)
	if err != nil || got.LibraryRecipeID != "66e5a1f2c3b4a5d6e7f8aaaa" {
		t.Errorf("get = %+v, %v; want it marked", got.LibraryRecipeID, err)
	}
}

func TestReportsCountAgainstTheWholeGroup(t *testing.T) {
	ctx := t.Context()
	h := newHarness(t, nil)
	h.service.WithReports(newMemoryReports())
	var copies []string
	for i := range 4 {
		hh := fmt.Sprintf("66e5a1f2c3b4a5d6e7f80d%02d", i)
		shareAs(t, h, hh, dish(recipes.SourceUser, fmt.Sprint(i), "https://example.com/stew?utm_source="+fmt.Sprint(i), "Odd Stew", fmt.Sprintf("thing %d", i)))
	}
	for _, e := range h.store.entries {
		copies = append(copies, e.ID)
	}
	shareAs(t, h, hhBob, typed("Good Soup", nil, "stock"))
	visible := func(hh string) []string {
		t.Helper()
		page, err := h.service.Discover(ctx, hh, Query{})
		if err != nil {
			t.Fatal(err)
		}
		search, err := h.service.Search(ctx, hh, Query{})
		if err != nil {
			t.Fatal(err)
		}
		if search.Total != len(search.Items) || len(search.Items) != len(page.Items) {
			t.Errorf("search shows %d of %d, discover %d", len(search.Items), search.Total, len(page.Items))
		}
		return names(resultRecipes(page))
	}

	// Ada reports one copy: every copy is gone for her, by list, get, and add.
	if _, err := h.service.Report(ctx, Report{CatalogID: copies[3], HouseholdID: hhAda, Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	if got := visible(hhAda); !slices.Equal(got, []string{"Good Soup"}) {
		t.Errorf("Ada sees %v after reporting one copy", got)
	}
	for _, id := range copies {
		if _, err := h.service.Get(ctx, hhAda, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Ada can still open copy %s: %v", id, err)
		}
		if _, _, err := h.service.Add(ctx, hhAda, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Ada can still add copy %s: %v", id, err)
		}
	}
	if got := visible(hhBob); len(got) != 2 {
		t.Errorf("Bob sees %v; one report hides it only for Ada", got)
	}

	// Two more households report two other copies: three households in all,
	// so no one sees any copy.
	for i, hh := range []string{hhBob, "66e5a1f2c3b4a5d6e7f80e03"} {
		if _, err := h.service.Report(ctx, Report{CatalogID: copies[i], HouseholdID: hh, Reason: "inappropriate"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := visible("66e5a1f2c3b4a5d6e7f80e04"); !slices.Equal(got, []string{"Good Soup"}) {
		t.Errorf("after %d households reported copies, a new household sees %v", PullAfter, got)
	}
	if _, err := h.service.Get(ctx, "66e5a1f2c3b4a5d6e7f80e04", copies[2]); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unreported copy of a pulled recipe still opens: %v", err)
	}
}

// Entries stored before decision 689 have no fingerprint. Until the backfill
// reaches them each stands alone, and the lists still work.
func TestIntegrationBackfillFingerprints(t *testing.T) {
	store := newTestMongoStore(t)
	put(t, store,
		dish(recipes.SourceUser, "a", "https://example.com/chili", "Chili", "beans"),
		dish(recipes.SourceUser, "b", "https://www.example.com/chili/", "Chili", "black beans"),
		typed("Soup", []string{"Simmer"}, "stock"),
	)
	if _, err := store.entries.UpdateMany(t.Context(), map[string]any{},
		map[string]any{"$unset": map[string]any{"fingerprint": "", "fingerprintVersion": "", "completeness": ""}}); err != nil {
		t.Fatal(err)
	}
	found, total := searchAll(t, store, Filter{})
	if total != 3 || len(found) != 3 {
		t.Fatalf("before the backfill search shows %v of %d; want each entry alone", names(found), total)
	}
	if found[0].Content.Name != "Chili" || len(found[0].Content.Ingredients) == 0 {
		t.Errorf("an entry with no fingerprint came back empty: %+v", found[0])
	}
	if scanned, _ := store.Scan(t.Context(), ScanLimit); len(scanned) != 3 {
		t.Errorf("before the backfill discovery shows %d; want 3", len(scanned))
	}
	groups, err := store.ListGroups(t.Context())
	if err != nil || len(groups) != 3 || groups[0].Group != groups[1].Group || groups[0].Current {
		t.Fatalf("ListGroups before the backfill = %+v, %v; want the chili copies grouped and not current", groups, err)
	}

	n, err := store.BackfillFingerprints(t.Context())
	if err != nil || n != 3 {
		t.Fatalf("BackfillFingerprints = %d, %v; want 3", n, err)
	}
	if n, err := store.BackfillFingerprints(t.Context()); err != nil || n != 0 {
		t.Errorf("second BackfillFingerprints = %d, %v; want nothing left to do", n, err)
	}
	if _, total := searchAll(t, store, Filter{}); total != 2 {
		t.Errorf("after the backfill search counts %d; want 2", total)
	}
	if scanned, _ := store.Scan(t.Context(), ScanLimit); len(scanned) != 2 {
		t.Errorf("after the backfill discovery shows %d; want 2", len(scanned))
	}
	groups, _ = store.ListGroups(t.Context())
	for _, g := range groups {
		if !g.Current {
			t.Errorf("entry %s is not current after the backfill", g.Name)
		}
	}
}

// Version 1 put the importer's ID first, so HelloFresh's weekly re-releases
// of one recipe stayed apart. The backfill recomputes them under the new rules.
func TestIntegrationBackfillRecomputesOlderVersions(t *testing.T) {
	store := newTestMongoStore(t)
	url := "https://www.hellofresh.com/recipes/garlicky-shrimp-couscous-bowls-6447d2af33e34d0ea5018115"
	put(t, store,
		dish(recipes.SourceHelloFresh, "6447d2af33e34d0ea5018115", url, "Garlicky Shrimp Couscous Bowls", "shrimp"),
		dish(recipes.SourceHelloFresh, "64a0c1e2f3a4b5c6d7e80001", url+"?utm_source=email", "Garlicky Shrimp Couscous Bowls", "shrimp"),
	)
	// Make them look like version 1 entries: one src: fingerprint each.
	cur, err := store.entries.Find(t.Context(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var docs []entryDoc
	if err := cur.All(t.Context(), &docs); err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if _, err := store.entries.UpdateByID(t.Context(), d.ID, map[string]any{"$set": map[string]any{
			"fingerprint": FingerprintSource + d.SourceID, "fingerprintVersion": 1,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, total := searchAll(t, store, Filter{}); total != 2 {
		t.Fatalf("version 1 fingerprints show %d; want the two copies apart", total)
	}
	if n, err := store.BackfillFingerprints(t.Context()); err != nil || n != 2 {
		t.Fatalf("BackfillFingerprints = %d, %v; want both recomputed", n, err)
	}
	if _, total := searchAll(t, store, Filter{}); total != 1 {
		t.Errorf("after the backfill search counts %d; want 1", total)
	}
}

func TestTwoReportsOfOneCopyFromOneHouseholdDoNotPullIt(t *testing.T) {
	ctx := t.Context()
	h := newHarness(t, nil)
	h.service.WithReports(newMemoryReports())
	for i := range 3 {
		shareAs(t, h, hhBob, dish(recipes.SourceUser, fmt.Sprint(i), "https://example.com/stew", "Stew", fmt.Sprintf("thing %d", i)))
	}
	// One household reporting three copies is still one household.
	for _, e := range h.store.entries {
		if _, err := h.service.Report(ctx, Report{CatalogID: e.ID, HouseholdID: hhAda, Reason: "spam"}); err != nil {
			t.Fatal(err)
		}
	}
	if page, _ := h.service.Discover(ctx, hhBob, Query{}); len(page.Items) != 1 {
		t.Errorf("one household's reports pulled the recipe for everyone: %v", names(resultRecipes(page)))
	}
}
