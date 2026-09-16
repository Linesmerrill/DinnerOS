package recommendations

import (
	"context"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// TestSkipReasonsMatchTheProviderContract keeps the vocabulary the app records
// and the vocabulary the planner interprets from drifting apart. A reason the
// app can record but the provider doesn't know falls through to "this is about
// the meal" and quietly counts as a dislike, which is the failure this whole
// signal exists to avoid.
func TestSkipReasonsMatchTheProviderContract(t *testing.T) {
	known := []string{
		autopilot.SkipNoTime, autopilot.SkipAteOut, autopilot.SkipMissingIngredients,
		autopilot.SkipNotInTheMood, autopilot.SkipOther,
	}
	for _, reason := range events.SkipReasons {
		if !slices.Contains(known, reason) {
			t.Errorf("the app can record %q, which autopilot has no constant for", reason)
		}
	}
	for _, reason := range known {
		if !slices.Contains(events.SkipReasons, reason) {
			t.Errorf("autopilot knows %q, which the app can never record", reason)
		}
	}
}

// TestBuildInputCarriesSkipReasons checks the adapter forwards why a planned
// meal wasn't cooked, so the baseline can tell a meal the household turned
// down from one the week simply got in the way of.
func TestBuildInputCarriesSkipReasons(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	w := mustWeek(t, testWeek)

	if _, _, err := env.events.Insert(ctx, []events.Event{
		{
			HouseholdID: hhA, UserID: userAda, Type: events.TypeRecipeSkipped, RecipeID: rTacos, Week: testWeek,
			Payload: events.RecipeSkipped{Date: "2026-09-15", Reason: autopilot.SkipAteOut}, OccurredAt: testNow,
		},
		{
			HouseholdID: hhA, UserID: userAda, Type: events.TypeRecipeSkipped, RecipeID: rCurry, Week: testWeek,
			Payload: events.RecipeSkipped{Date: "2026-09-15", Reason: autopilot.SkipNotInTheMood}, OccurredAt: testNow,
		},
		{
			HouseholdID: hhA, UserID: userAda, Type: events.TypeRecipeSkipped, RecipeID: rSalmon, Week: testWeek,
			Payload: events.RecipeSkipped{Date: "2026-09-15"}, OccurredAt: testNow,
		},
		{
			HouseholdID: hhA, UserID: userAda, Type: events.TypeRecipeCooked, RecipeID: rBurger, Week: testWeek,
			Payload: events.RecipeCooked{Date: "2026-09-15"}, OccurredAt: testNow,
		},
	}); err != nil {
		t.Fatal(err)
	}

	profile, err := env.svc.Profile(ctx, hhA)
	if err != nil {
		t.Fatal(err)
	}
	in, _, err := env.svc.buildInput(ctx, hhA, w, profile, WeekContext{}, planning.Plan{}, DeviceSignals{})
	if err != nil {
		t.Fatal(err)
	}

	outcomes := map[string]autopilot.Interaction{}
	for _, h := range in.History {
		if h.Kind == autopilot.KindSkipped || h.Kind == autopilot.KindCooked {
			outcomes[h.ItemID] = h
		}
	}
	for _, tc := range []struct {
		name       string
		recipeID   string
		wantKind   autopilot.InteractionKind
		wantReason string
	}{
		{"a skip the household explained", rTacos, autopilot.KindSkipped, autopilot.SkipAteOut},
		{"a skip about the meal", rCurry, autopilot.KindSkipped, autopilot.SkipNotInTheMood},
		{"a skip with no reason given", rSalmon, autopilot.KindSkipped, ""},
		{"a cooked meal carries no reason", rBurger, autopilot.KindCooked, ""},
	} {
		got := outcomes[tc.recipeID]
		if got.Kind != tc.wantKind || got.Reason != tc.wantReason {
			t.Errorf("%s: interaction = {kind: %q, reason: %q}; want {kind: %q, reason: %q}",
				tc.name, got.Kind, got.Reason, tc.wantKind, tc.wantReason)
		}
	}
}
