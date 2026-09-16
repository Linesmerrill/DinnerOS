package skips

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// fakeCatalog resolves normalized names to catalog ingredient IDs.
type fakeCatalog struct {
	byKey map[string]string
	calls [][]string
	err   error
}

func (f *fakeCatalog) IngredientsByKey(_ context.Context, keys []string) ([]recipes.Ingredient, error) {
	f.calls = append(f.calls, keys)
	if f.err != nil {
		return nil, f.err
	}
	var out []recipes.Ingredient
	for _, k := range keys {
		if id, ok := f.byKey[k]; ok {
			out = append(out, recipes.Ingredient{ID: id, Key: k, Name: k})
		}
	}
	return out, nil
}

func newTestService(t *testing.T, catalog Catalog) (*Service, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	svc := NewService(store, catalog, nil)
	svc.now = func() time.Time { return testNow }
	return svc, store
}

func editor(householdID string) households.Membership {
	return households.Membership{HouseholdID: householdID, UserID: testUser, Role: households.RoleAdmin}
}

// viewer may see the household but not change the plan, so it may not skip.
func viewer(householdID string) households.Membership {
	return households.Membership{HouseholdID: householdID, UserID: otherUser, Role: households.Role("viewer")}
}

func TestSetSkipCreatesThenReplacesWithoutStacking(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)

	skip, created, err := svc.Set(ctx, editor(testHousehold), Input{
		IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeWeek, Week: "2026-W38",
	})
	if err != nil || !created {
		t.Fatalf("Set() = %+v, created %v, error %v; want created", skip, created, err)
	}
	if skip.Key != "cilantro" {
		t.Errorf("normalized key = %q, want %q", skip.Key, "cilantro")
	}

	// "Skip once" → "skip forever" replaces rather than adding a second skip.
	forever, created, err := svc.Set(ctx, editor(testHousehold), Input{
		IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeAlways,
	})
	if err != nil {
		t.Fatalf("Set() forever error = %v", err)
	}
	if created {
		t.Error("changing the lifetime must replace the skip, not create a second one")
	}
	if forever.ID != skip.ID || forever.Scope != ScopeAlways || forever.Week != "" {
		t.Errorf("replaced skip = %+v, want the same ID with scope always and no week", forever)
	}
	items, err := svc.List(ctx, testHousehold)
	if err != nil || len(items) != 1 {
		t.Fatalf("List() = %d skips, %v; want 1", len(items), err)
	}
}

func TestSetSkipValidates(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)
	cases := []struct {
		name string
		in   Input
	}{
		{"no ingredient key", Input{Name: "Cilantro", Scope: ScopeAlways}},
		{"unknown scope", Input{IngredientKey: "name:cilantro", Name: "Cilantro", Scope: Scope("forever")}},
		{"week scope without a week", Input{IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeWeek}},
		{"always scope with a week", Input{IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeAlways, Week: "2026-W38"}},
		{"catalog key without a name", Input{IngredientKey: "66e5a1f2c3b4a5d6e7f82001", Scope: ScopeAlways}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var validation *ValidationError
			if _, _, err := svc.Set(ctx, editor(testHousehold), tc.in); !errors.As(err, &validation) {
				t.Fatalf("Set(%+v) error = %v, want a ValidationError", tc.in, err)
			}
		})
	}
}

func TestSetSkipNeedsPlanEdit(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)
	in := Input{IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeAlways}
	if _, _, err := svc.Set(ctx, viewer(testHousehold), in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Set() as a viewer = %v, want ErrForbidden", err)
	}
	if err := svc.Remove(ctx, viewer(testHousehold), "66e5a1f2c3b4a5d6e7f80001"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Remove() as a viewer = %v, want ErrForbidden", err)
	}
}

func TestRemoveResumesTheIngredient(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)
	skip, _, err := svc.Set(ctx, editor(testHousehold), Input{
		IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeAlways,
	})
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := svc.Remove(ctx, editor(testHousehold), skip.ID); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	set, err := svc.GrocerySkips(ctx, testHousehold, "2026-W38")
	if err != nil {
		t.Fatalf("GrocerySkips() error = %v", err)
	}
	if len(set) != 0 {
		t.Errorf("a resumed ingredient still skips: %v", set)
	}
	if err := svc.Remove(ctx, editor(testHousehold), skip.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove() twice = %v, want ErrNotFound", err)
	}
}

func TestGrocerySkipsAppliesWeekScopeToOneWeekOnly(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)
	if _, _, err := svc.Set(ctx, editor(testHousehold), Input{
		IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeWeek, Week: "2026-W38",
	}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if _, _, err := svc.Set(ctx, editor(testHousehold), Input{
		IngredientKey: "name:dill", Name: "Dill", Scope: ScopeAlways,
	}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	thisWeek, err := svc.GrocerySkips(ctx, testHousehold, "2026-W38")
	if err != nil {
		t.Fatalf("GrocerySkips() error = %v", err)
	}
	if scope, ok := thisWeek.Skip("name:cilantro"); !ok || scope != grocery.SkipThisWeek {
		t.Errorf("this week's cilantro = %q, %v; want a week skip", scope, ok)
	}
	if scope, ok := thisWeek.Skip("name:dill"); !ok || scope != grocery.SkipAlways {
		t.Errorf("this week's dill = %q, %v; want an always skip", scope, ok)
	}

	// Next week the once-skip is gone on its own, and the forever-skip stays.
	nextWeek, err := svc.GrocerySkips(ctx, testHousehold, "2026-W39")
	if err != nil {
		t.Fatalf("GrocerySkips() error = %v", err)
	}
	if _, ok := nextWeek.Skip("name:cilantro"); ok {
		t.Error("a skip-once must come back the next week")
	}
	if _, ok := nextWeek.Skip("name:dill"); !ok {
		t.Error("a skip-forever must apply to every week")
	}
}

func TestGrocerySkipsMatchesCatalogAndFreeTextLines(t *testing.T) {
	ctx := context.Background()
	const catalogID = "66e5a1f2c3b4a5d6e7f82001"
	catalog := &fakeCatalog{byKey: map[string]string{"cilantro": catalogID}}
	svc, _ := newTestService(t, catalog)
	if _, _, err := svc.Set(ctx, editor(testHousehold), Input{
		IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeAlways,
	}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	set, err := svc.GrocerySkips(ctx, testHousehold, "2026-W38")
	if err != nil {
		t.Fatalf("GrocerySkips() error = %v", err)
	}
	// Skipping the ingredient skips it however a recipe reaches it.
	if _, ok := set.Skip("name:cilantro"); !ok {
		t.Error("the free-text line must be skipped")
	}
	if _, ok := set.Skip(catalogID); !ok {
		t.Error("a recipe reaching cilantro through the catalog must be skipped too")
	}
}

func TestGrocerySkipsIgnoresOtherHouseholds(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)
	if _, _, err := svc.Set(ctx, editor(otherHousehold), Input{
		IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeAlways,
	}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	set, err := svc.GrocerySkips(ctx, testHousehold, "2026-W38")
	if err != nil {
		t.Fatalf("GrocerySkips() error = %v", err)
	}
	if len(set) != 0 {
		t.Errorf("another household's skips leaked: %v", set)
	}
}

func TestSetSkipCapsOneHousehold(t *testing.T) {
	ctx := context.Background()
	svc, store := newTestService(t, nil)
	for i := range MaxPerHousehold {
		if _, _, err := store.PutSkip(ctx, Skip{
			HouseholdID: testHousehold, IngredientKey: "name:herb-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Key: "herb", Name: "Herb", Scope: ScopeAlways, CreatedBy: testUser, CreatedAt: testNow,
			UpdatedBy: testUser, UpdatedAt: testNow,
		}); err != nil {
			t.Fatalf("seeding skip %d error = %v", i, err)
		}
	}
	var validation *ValidationError
	if _, _, err := svc.Set(ctx, editor(testHousehold), Input{
		IngredientKey: "name:cilantro", Name: "Cilantro", Scope: ScopeAlways,
	}); !errors.As(err, &validation) {
		t.Fatalf("Set() past the cap = %v, want a ValidationError", err)
	}
	// Changing a skip that is already stored still works at the cap.
	if _, _, err := svc.Set(ctx, editor(testHousehold), Input{
		IngredientKey: "name:herb-aa", Name: "Herb", Scope: ScopeWeek, Week: "2026-W38",
	}); err != nil {
		t.Fatalf("changing an existing skip at the cap error = %v", err)
	}
}
