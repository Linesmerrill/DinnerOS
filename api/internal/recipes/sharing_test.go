package recipes

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakePublisher records what reaches the global catalog.
type fakePublisher struct {
	published []Recipe
	err       error
}

func (p *fakePublisher) PublishRecipes(_ context.Context, rs []Recipe) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	p.published = append(p.published, rs...)
	return len(rs), nil
}

func (p *fakePublisher) names() []string {
	out := make([]string, 0, len(p.published))
	for _, r := range p.published {
		out = append(out, r.Name)
	}
	slices.Sort(out)
	return out
}

func newSharingService(t *testing.T) (*Service, *memoryStore, *fakePublisher) {
	t.Helper()
	store, publisher := newMemoryStore(), &fakePublisher{}
	return NewService(store).WithCatalog(publisher), store, publisher
}

// importOne stores one recipe and returns it.
func importOne(t *testing.T, s *Service, householdID, source, sourceID, name string) Recipe {
	t.Helper()
	res, err := s.Import(t.Context(), householdID, ImportFile{
		Version: ImportVersion, Source: source,
		Recipes: []ImportRecipe{{
			Source: source, SourceRecipeID: sourceID, Name: name, Servings: []int{2},
			Ingredients: []ImportIngredient{{Name: "olive oil", Amounts: []ImportAmount{{Servings: 2}}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("import rejected the recipe: %+v", res.Errors)
	}
	found, err := s.store.FindRecipesBySourceIDs(t.Context(), householdID, source, []string{sourceID})
	if err != nil || len(found) != 1 {
		t.Fatalf("stored recipe not found: %v", err)
	}
	return found[0]
}

func TestImportPublishesOnlyPublicSources(t *testing.T) {
	service, _, publisher := newSharingService(t)
	importOne(t, service, hhAda, SourceHelloFresh, "hf-1", "Harissa Chicken")
	importOne(t, service, hhAda, SourceManual, "m-1", "Grandma's Chili")

	if got := publisher.names(); !slices.Equal(got, []string{"Harissa Chicken"}) {
		t.Fatalf("published %v; want only the HelloFresh recipe", got)
	}
	for _, r := range publisher.published {
		if r.HouseholdID == "" {
			continue
		}
		// The publisher is handed the library recipe; it is the catalog's job
		// to strip. What matters here is that nothing private was offered.
		if !Publishable(r) {
			t.Errorf("a non-publishable recipe %q was offered to the catalog", r.Name)
		}
	}
}

func TestReimportingTheSameFilePublishesNothingNew(t *testing.T) {
	service, _, publisher := newSharingService(t)
	importOne(t, service, hhAda, SourceHelloFresh, "hf-1", "Harissa Chicken")
	before := len(publisher.published)
	importOne(t, service, hhAda, SourceHelloFresh, "hf-1", "Harissa Chicken")
	if len(publisher.published) != before {
		t.Errorf("an unchanged re-import published again: %v", publisher.names())
	}
}

func TestShareOptsInAndPublishes(t *testing.T) {
	service, _, publisher := newSharingService(t)
	stored := importOne(t, service, hhAda, SourceManual, "m-1", "Grandma's Chili")
	if stored.SharedToCatalog {
		t.Fatal("a new recipe is shared by default")
	}

	shared, err := service.Share(t.Context(), hhAda, stored.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !shared.SharedToCatalog {
		t.Error("Share did not set the opt-in")
	}
	if got := publisher.names(); !slices.Contains(got, "Grandma's Chili") {
		t.Errorf("sharing did not publish; published %v", got)
	}

	// Un-sharing stops future publishing but does not retract the entry.
	count := len(publisher.published)
	if _, err := service.Share(t.Context(), hhAda, stored.ID, false); err != nil {
		t.Fatal(err)
	}
	if len(publisher.published) != count {
		t.Error("un-sharing published again")
	}
}

func TestShareIsNotFoundForAnotherHouseholdsRecipe(t *testing.T) {
	service, _, _ := newSharingService(t)
	stored := importOne(t, service, hhAda, SourceHelloFresh, "hf-1", "Harissa Chicken")
	if _, err := service.Share(t.Context(), hhBob, stored.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Share across households = %v; want ErrNotFound", err)
	}
}

func TestAddFromCatalogCopiesWithoutHistoryAndIsIdempotent(t *testing.T) {
	service, _, _ := newSharingService(t)
	source := importOne(t, service, hhAda, SourceHelloFresh, "hf-1", "Harissa Chicken")
	source.OrderWeeks, source.TimesOrdered, source.LastOrderedWeek = []string{"2026-W04"}, 4, "2026-W04"
	content := source
	content.ID, content.HouseholdID = "", ""
	content.OrderWeeks, content.TimesOrdered, content.LastOrderedWeek = nil, 0, ""

	stored, created, err := service.AddFromCatalog(t.Context(), hhBob, content)
	if err != nil || !created {
		t.Fatalf("AddFromCatalog = created %v, %v; want created", created, err)
	}
	switch {
	case stored.HouseholdID != hhBob:
		t.Errorf("copy belongs to %q; want %q", stored.HouseholdID, hhBob)
	case stored.ID == source.ID:
		t.Error("the copy reuses the source recipe's ID")
	case stored.TimesOrdered != 0 || stored.LastOrderedWeek != "" || len(stored.OrderWeeks) != 0:
		t.Errorf("the copy carries order history: %+v", stored)
	}

	again, created, err := service.AddFromCatalog(t.Context(), hhBob, content)
	if err != nil {
		t.Fatal(err)
	}
	if created || again.ID != stored.ID {
		t.Errorf("a second add created %v with ID %q; want the first copy %q", created, again.ID, stored.ID)
	}
}

func TestAddFromCatalogReturnsWhatTheHouseholdAlreadyImported(t *testing.T) {
	service, _, _ := newSharingService(t)
	own := importOne(t, service, hhAda, SourceHelloFresh, "hf-1", "Harissa Chicken")
	content := own
	content.ID, content.HouseholdID = "", ""

	stored, created, err := service.AddFromCatalog(t.Context(), hhAda, content)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("adding a recipe the household imported itself created a duplicate")
	}
	if stored.ID != own.ID {
		t.Errorf("returned %q; want the household's own recipe %q", stored.ID, own.ID)
	}
}

func TestInLibraryMarksOnlyTheHouseholdsOwnKeys(t *testing.T) {
	service, _, _ := newSharingService(t)
	ada := importOne(t, service, hhAda, SourceHelloFresh, "hf-1", "Harissa Chicken")
	importOne(t, service, hhBob, SourceHelloFresh, "hf-2", "Lemon Salmon")

	have, err := service.InLibrary(t.Context(), hhAda, []string{"hellofresh:hf-1", "hellofresh:hf-2"})
	if err != nil {
		t.Fatal(err)
	}
	if have["hellofresh:hf-1"] != ada.ID {
		t.Errorf("own recipe not marked: %v", have)
	}
	if _, ok := have["hellofresh:hf-2"]; ok {
		t.Error("another household's recipe was reported as in this household's library")
	}
}

func TestBackfillKeysAndPublishesExistingRecipes(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store)
	// Stored the way a pre-catalog write would have: no catalogKey.
	now := time.Now().UTC()
	legacy := []Recipe{
		{Source: SourceHelloFresh, SourceRecipeID: "hf-1", Name: "Harissa Chicken", CreatedAt: now, UpdatedAt: now},
		{Source: SourceManual, SourceRecipeID: "m-1", Name: "Grandma's Chili", CreatedAt: now, UpdatedAt: now},
	}
	if err := store.SaveRecipes(t.Context(), hhAda, legacy); err != nil {
		t.Fatal(err)
	}
	for i := range store.recipes {
		store.recipes[i].CatalogKey = ""
	}

	res, err := service.Backfill(t.Context(), hhAda, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Read != 2 || res.Keyed != 2 || res.Published != 0 {
		t.Fatalf("dry run = %+v; want 2 read, 2 to key, 0 published", res)
	}
	if store.recipes[0].CatalogKey != "" {
		t.Error("the dry run wrote a catalogKey")
	}

	publisher := &fakePublisher{}
	res, err = service.WithCatalog(publisher).Backfill(t.Context(), hhAda, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Keyed != 2 || res.Publishable != 1 || res.Published != 1 {
		t.Fatalf("apply = %+v; want 2 keyed, 1 publishable, 1 published", res)
	}
	if got := publisher.names(); !slices.Equal(got, []string{"Harissa Chicken"}) {
		t.Fatalf("backfill published %v; want only the public-source recipe", got)
	}
	for _, r := range store.recipes {
		if r.CatalogKey == "" {
			t.Errorf("recipe %q still has no catalogKey", r.Name)
		}
	}

	// Re-running finds nothing left to key.
	res, err = service.Backfill(t.Context(), hhAda, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Keyed != 0 {
		t.Errorf("a second backfill re-keyed %d recipes", res.Keyed)
	}
}

func TestPublishFailureFailsTheImport(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store).WithCatalog(&fakePublisher{err: errors.New("catalog is down")})
	_, err := service.Import(t.Context(), hhAda, ImportFile{
		Version: ImportVersion, Source: SourceHelloFresh,
		Recipes: []ImportRecipe{{Source: SourceHelloFresh, SourceRecipeID: "hf-1", Name: "A", Servings: []int{2}}},
	})
	if err == nil || !strings.Contains(err.Error(), "publish to catalog") {
		t.Fatalf("Import error = %v; want a publish failure", err)
	}
}
