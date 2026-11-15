package planning

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// A recipe delete (decision 695) takes the recipe off this week from today
// on and every later week; past days, past weeks, and finalized weeks keep
// it. Undo puts the meals back.
func TestRemoveRecipeFromUpcomingAndRestore(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc, reader := newTestService(t, store)
	// testNow is Tuesday of 2026-W38.
	mustAdd(t, svc, hhAda, userAda, "2026-W37", NewEntry{RecipeID: recipeTacos, Day: "thu", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "wed", Servings: 4, Note: "for guests"})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Day: "thu", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W39", NewEntry{RecipeID: recipeTacos, Day: "fri", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W40", NewEntry{RecipeID: recipeTacos, Day: "fri", Servings: 2})
	if _, err := svc.SetStatus(ctx, hhAda, "2026-W40", string(StatusFinalized)); err != nil {
		t.Fatal(err)
	}

	removed, err := svc.RemoveRecipeFromUpcoming(ctx, hhAda, userAda, recipeTacos)
	if err != nil {
		t.Fatal(err)
	}
	want := []recipes.RemovedPlanEntry{
		{Week: testWeek, Day: "wed", Servings: 4, Note: "for guests", Origin: string(OriginManual)},
		{Week: testWeek, Servings: 2, Origin: string(OriginManual)},
		{Week: "2026-W39", Day: "fri", Servings: 2, Origin: string(OriginManual)},
	}
	if !slices.Equal(removed, want) {
		t.Fatalf("removed = %+v\nwant %+v", removed, want)
	}
	tacosIn := func(week string) []Day {
		p, err := svc.Get(ctx, hhAda, week)
		if err != nil {
			t.Fatal(err)
		}
		var days []Day
		for _, e := range p.Entries {
			if e.RecipeID == recipeTacos {
				days = append(days, e.Day)
			}
		}
		return days
	}
	for week, days := range map[string][]Day{"2026-W37": {"thu"}, testWeek: {"mon"}, "2026-W39": nil, "2026-W40": {"fri"}} {
		if got := tacosIn(week); !slices.Equal(got, days) {
			t.Errorf("%s tacos on %v; want %v", week, got, days)
		}
	}
	// The week's list is worked out from the plan, so it loses the rest of
	// the tacos with them; soup is untouched.
	list, err := svc.GroceryList(ctx, hhAda, "2026-W39")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Meals) != 0 {
		t.Errorf("W39 list meals = %+v", list.Meals)
	}

	// A deleted recipe can't be planned again.
	deleted := tacosRecipe()
	deleted.DeletedAt = testNow
	reader.put(deleted)
	if _, _, err := svc.AddEntry(ctx, hhAda, userAda, "2026-W41", NewEntry{RecipeID: recipeTacos, Servings: 2}); !errors.Is(err, ErrRecipeNotFound) {
		t.Errorf("planning a deleted recipe = %v; want ErrRecipeNotFound", err)
	}

	// Undo: the recipe is back, then its meals are.
	reader.put(tacosRecipe())
	back := append(slices.Clone(removed),
		recipes.RemovedPlanEntry{Week: testWeek, Day: "mon", Servings: 2},    // a day that has passed
		recipes.RemovedPlanEntry{Week: "2026-W37", Day: "fri", Servings: 2},  // a week that has ended
		recipes.RemovedPlanEntry{Week: "2026-W40", Day: "sat", Servings: 2},  // finalized since
		recipes.RemovedPlanEntry{Week: "2026-W39", Servings: 2, Origin: "?"}, // an origin it no longer knows
	)
	if err := svc.RestoreRecipeEntries(ctx, hhAda, userAda, recipeTacos, back); err != nil {
		t.Fatal(err)
	}
	for week, days := range map[string][]Day{"2026-W37": {"thu"}, testWeek: {"mon", "wed", ""}, "2026-W39": {"fri", ""}, "2026-W40": {"fri"}} {
		if got := tacosIn(week); !slices.Equal(got, days) {
			t.Errorf("after restore %s tacos on %v; want %v", week, got, days)
		}
	}
	p, _ := svc.Get(ctx, hhAda, testWeek)
	for _, e := range p.Entries {
		if e.RecipeID == recipeTacos && e.Day == "wed" && (e.Note != "for guests" || e.Servings != 4) {
			t.Errorf("restored entry = %+v", e)
		}
	}
}
