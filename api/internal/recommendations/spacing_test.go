package recommendations

import (
	"context"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

func sharedMeals(a, b map[string]string) []string {
	seen := map[string]bool{}
	for _, id := range a {
		seen[id] = true
	}
	var out []string
	for _, id := range b {
		if seen[id] {
			out = append(out, id)
		}
	}
	return out
}

// Two coming weeks never suggest the same meals: a week's suggestions keep
// apart from the other weeks' pending suggestions and from the coming weeks'
// plans, as they do from past ones (decision 642).
func TestComingWeeksDontSuggestTheSameMeals(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := env.svc
	if _, err := svc.UpdateProfile(ctx, hhA, userAda, smokerProfile(), true); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Generate(ctx, hhA, userAda, "2026-W39", GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Generate(ctx, hhA, userAda, "2026-W40", GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Slots) < 4 || len(second.Slots) < 4 {
		t.Fatalf("slots = %d and %d", len(first.Slots), len(second.Slots))
	}
	// The smoker rule has two meals to choose from, so at most one repeats.
	if same := sharedMeals(slotRecipes(first), slotRecipes(second)); len(same) > 1 {
		t.Errorf("W39 %v and W40 %v share %d meals", slotRecipes(first), slotRecipes(second), len(same))
	}

}

// A week planned further out counts too: W41's meals stay out of W40.
func TestASuggestedWeekKeepsApartFromAPlannedLaterWeek(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := env.svc
	w41 := mustWeek(t, "2026-W41")
	var entries []planning.Entry
	for i, id := range []string{rCurry, rSalmon, rRigatoni} {
		entries = append(entries, planning.Entry{ID: "e" + id, RecipeID: id, Day: []planning.Day{planning.Monday, planning.Tuesday, planning.Wednesday}[i], Servings: 2, Origin: planning.OriginManual})
	}
	env.planner.set(planning.Plan{HouseholdID: hhA, Week: w41, Status: planning.StatusDraft, Entries: entries})
	again, err := svc.Generate(ctx, hhA, userAda, "2026-W40", GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	planned := map[string]string{"a": rCurry, "b": rSalmon, "c": rRigatoni}
	if same := sharedMeals(planned, slotRecipes(again)); len(same) > 0 {
		t.Errorf("W40 %v repeats W41's planned %v", slotRecipes(again), same)
	}
}
