package catalog

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Decision 697: a typed or pasted recipe's catalog entry is its household
// recipe's own, so sharing never overwrites another household's recipe, and
// re-sharing an edited recipe updates the entry instead of adding one.

func TestMemoryShareKeys(t *testing.T) {
	runShareKeyCases(t, func(*testing.T) Store { return newMemoryStore() })
}

func TestIntegrationMongoShareKeys(t *testing.T) {
	runShareKeyCases(t, func(t *testing.T) Store { return newTestMongoStore(t) })
}

// householdBrownies is one household's typed brownies, with the random
// source ID a typed recipe gets.
func householdBrownies(household string, steps ...string) recipes.Recipe {
	r := typed("Brownies", steps, "butter", "cocoa")
	r.HouseholdID, r.SourceRecipeID, r.SharedToCatalog = household, "generated-"+household, true
	return r
}

func runShareKeyCases(t *testing.T, newStore func(*testing.T) Store) {
	setup := func(t *testing.T) (*Service, Store) {
		store := newStore(t)
		return NewService(ServiceOptions{Store: store, Now: func() time.Time { return testNow }}), store
	}
	share := func(t *testing.T, s *Service, rs ...recipes.Recipe) {
		t.Helper()
		if _, err := s.PublishRecipes(t.Context(), rs); err != nil {
			t.Fatal(err)
		}
	}
	stored := func(t *testing.T, store Store) []Recipe {
		t.Helper()
		var out []Recipe
		for _, id := range allIDs(t, store) {
			e, err := store.Get(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, e)
		}
		return out
	}
	shown := func(t *testing.T, s *Service) int {
		t.Helper()
		page, err := s.Discover(t.Context(), "66e5a1f2c3b4a5d6e7f80f99", Query{})
		if err != nil {
			t.Fatal(err)
		}
		search, err := s.Search(t.Context(), "66e5a1f2c3b4a5d6e7f80f99", Query{})
		if err != nil {
			t.Fatal(err)
		}
		if search.Total != page.Total {
			t.Errorf("search counts %d, discover %d", search.Total, page.Total)
		}
		return page.Total
	}

	t.Run("same title and ingredient names, different steps, two households", func(t *testing.T) {
		s, store := setup(t)
		share(t, s, householdBrownies(hhAda, "Bake 20 minutes."))
		share(t, s, householdBrownies(hhBob, "Bake 35 minutes for a cakey middle."))
		entries := stored(t, store)
		if len(entries) != 2 || entries[0].CatalogKey == entries[1].CatalogKey {
			t.Fatalf("stored %d entries; want Ada's and Bob's, under different keys", len(entries))
		}
		if n := shown(t, s); n != 2 {
			t.Errorf("shows %d; want both", n)
		}
	})

	t.Run("a household edits its steps and shares again", func(t *testing.T) {
		s, store := setup(t)
		share(t, s, householdBrownies(hhAda, "Bake 20 minutes."))
		before := stored(t, store)
		share(t, s, householdBrownies(hhAda, "Bake 25 minutes, then cool."))
		after := stored(t, store)
		if len(after) != 1 || after[0].ID != before[0].ID {
			t.Fatalf("stored %d entries; want Ada's one entry, updated", len(after))
		}
		if got := after[0].Content.Steps[0].Text; got != "Bake 25 minutes, then cool." {
			t.Errorf("entry's step = %q; want the edit", got)
		}
		if after[0].CatalogKey == before[0].CatalogKey {
			t.Error("the entry kept its old key, so a library with the edited recipe won't match it")
		}
		if n := shown(t, s); n != 1 {
			t.Errorf("shows %d; want 1", n)
		}
	})

	t.Run("the same typed recipe from two households", func(t *testing.T) {
		s, store := setup(t)
		share(t, s, householdBrownies(hhAda, "Bake 20 minutes."), householdBrownies(hhBob, "Bake 20 minutes."))
		if entries := stored(t, store); len(entries) != 2 {
			t.Fatalf("stored %d entries; want one per household", len(entries))
		}
		if n := shown(t, s); n != 1 {
			t.Errorf("shows %d; want one recipe", n)
		}
	})

	t.Run("an importer's recipe is still one entry for everyone", func(t *testing.T) {
		s, store := setup(t)
		a := dish(recipes.SourceHelloFresh, "hf-1", "", "Pork Tacos", "pork")
		a.HouseholdID, a.SharedToCatalog = hhAda, true
		b := a
		b.HouseholdID = hhBob
		share(t, s, a)
		share(t, s, b)
		if entries := stored(t, store); len(entries) != 1 || entries[0].Origin != "" {
			t.Errorf("stored %+v; want one entry with no origin", entries)
		}
	})

	t.Run("an entry from before decision 697 is taken over, not copied", func(t *testing.T) {
		s, store := setup(t)
		// Published before 697: keyed by name and ingredient names, no origin.
		old := householdBrownies(hhAda, "Bake 20 minutes.")
		legacy := recipes.LegacyCatalogKey(old)
		if _, err := store.Upsert(t.Context(), []Recipe{{CatalogKey: legacy, Content: Content(old)}}, testNow); err != nil {
			t.Fatal(err)
		}
		before := stored(t, store)
		share(t, s, householdBrownies(hhAda, "Bake 20 minutes, then cool."))
		after := stored(t, store)
		if len(after) != 1 || after[0].ID != before[0].ID || after[0].Origin == "" || after[0].CatalogKey == legacy {
			t.Fatalf("after re-sharing: %d entries, %+v; want the old entry, now Ada's", len(after), after)
		}
		// Bob shares brownies with the same names: his own entry, Ada's kept.
		share(t, s, householdBrownies(hhBob, "Bake 40 minutes."))
		if entries := stored(t, store); len(entries) != 2 {
			t.Errorf("stored %d entries; want Ada's and Bob's", len(entries))
		}
	})

	t.Run("a household still storing the old key has the recipe", func(t *testing.T) {
		store := newStore(t)
		library := newFakeLibrary()
		s := NewService(ServiceOptions{Store: store, Library: library, Now: func() time.Time { return testNow }})
		r := householdBrownies(hhBob, "Bake 20 minutes.")
		share(t, s, r)
		// Ada's copy was written before 697 and still stores the old key.
		library.have[hhAda] = map[string]string{recipes.LegacyCatalogKey(r): "66e5a1f2c3b4a5d6e7f8aaaa"}
		page, err := s.Discover(t.Context(), hhAda, Query{})
		if err != nil || len(page.Items) != 0 {
			t.Errorf("discover offers Ada brownies she has: %+v, %v", page.Items, err)
		}
	})
}

// A database from before decision 697 has a unique catalogKey index and
// entries with no entryKey. Startup drops the index and the backfill gives
// every entry its entryKey; then two households can share the same recipe.
func TestIntegrationShareKeysMigrateAnOldDatabase(t *testing.T) {
	client := mongotest.Client(t)
	coll := client.Database().Collection(RecipesCollection)
	if _, err := coll.Indexes().CreateOne(t.Context(), mongo.IndexModel{
		Keys: bson.D{{Key: "catalogKey", Value: 1}}, Options: options.Index().SetUnique(true).SetName("catalogKey_unique"),
	}); err != nil {
		t.Fatal(err)
	}
	old := householdBrownies(hhAda, "Bake 20 minutes.")
	hf := dish(recipes.SourceHelloFresh, "hf-1", "", "Pork Tacos", "pork")
	for _, e := range []Recipe{{CatalogKey: recipes.LegacyCatalogKey(old), Content: Content(old)}, {CatalogKey: "hellofresh:hf-1", Content: hf}} {
		d, err := newEntryDoc(e)
		if err != nil {
			t.Fatal(err)
		}
		d.ID, d.EntryKey, d.FingerprintVersion = bson.NewObjectID(), "", 2
		if _, err := coll.InsertOne(t.Context(), d); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.EnsureIndexes(t.Context(), Indexes()...); err != nil {
		t.Fatalf("ensure indexes over the old ones: %v", err)
	}
	store := NewMongoStore(client.Database())
	if n, err := store.BackfillFingerprints(t.Context()); err != nil || n != 2 {
		t.Fatalf("BackfillFingerprints = %d, %v; want both", n, err)
	}
	if n, err := coll.CountDocuments(t.Context(), bson.D{{Key: "entryKey", Value: bson.D{{Key: "$exists", Value: false}}}}); err != nil || n != 0 {
		t.Errorf("%d entries still have no entryKey (%v)", n, err)
	}

	s := NewService(ServiceOptions{Store: store, Now: func() time.Time { return testNow }})
	hfBob := hf
	hfBob.HouseholdID, hfBob.SharedToCatalog = hhBob, true
	if _, err := s.PublishRecipes(t.Context(), []recipes.Recipe{
		householdBrownies(hhAda, "Bake 20 minutes."), householdBrownies(hhBob, "Bake 20 minutes."), hfBob,
	}); err != nil {
		t.Fatal(err)
	}
	if n, _ := coll.CountDocuments(t.Context(), bson.D{}); n != 3 {
		t.Errorf("stored %d entries; want Ada's (the old one), Bob's, and the one HelloFresh entry", n)
	}
	if _, total := searchAll(t, store, Filter{}); total != 2 {
		t.Errorf("search counts %d; want the brownies once and the tacos", total)
	}
}

// allIDs lists every stored entry's ID, in either store.
func allIDs(t *testing.T, store Store) []string {
	t.Helper()
	switch s := store.(type) {
	case *memoryStore:
		var out []string
		for _, e := range s.entries {
			out = append(out, e.ID)
		}
		return out
	case *MongoStore:
		cur, err := s.entries.Find(t.Context(), map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var docs []entryDoc
		if err := cur.All(t.Context(), &docs); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range docs {
			out = append(out, d.ID.Hex())
		}
		return out
	}
	t.Fatal("unknown store")
	return nil
}
