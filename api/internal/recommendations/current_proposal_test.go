package recommendations

import (
	"context"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// saveStaleProposal saves a pending proposal for testWeek as one made before
// the week moved on: three meals late in the week, two saying they haven't
// been had in a while.
func saveStaleProposal(t *testing.T, env *testEnv) Proposal {
	t.Helper()
	notRecently := Reason{Code: notRecentlyCode, Text: "Haven’t had it in 16 weeks"}
	familiar := Reason{Code: "familiar", Text: "A household regular (6 times)"}
	p, err := env.store.SaveProposal(context.Background(), Proposal{
		ID: newID(), HouseholdID: hhA, Week: testWeek, Status: StatusProposed, Planned: 3,
		Slots: []Slot{
			{ID: "thu", Day: "thu", RecipeID: rSalmon, RecipeName: "Salmon", Servings: 2},
			{ID: "fri", Day: "fri", RecipeID: rTacos, RecipeName: "Tacos", Servings: 2, Reasons: []Reason{familiar, notRecently}},
			{ID: "sat", Day: "sat", RecipeID: rCurry, RecipeName: "Curry", Servings: 2, Reasons: []Reason{familiar, notRecently}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func slotFor(p Proposal, recipeID string) (Slot, bool) {
	i := slices.IndexFunc(p.Slots, func(s Slot) bool { return s.RecipeID == recipeID })
	if i < 0 {
		return Slot{}, false
	}
	return p.Slots[i], true
}

// A meal planned by hand after the proposal was made isn't suggested again
// for another night of the same week (build 123 review: Chicken Penne al
// Limone, on Sunday and marked made, suggested again for Tuesday).
func TestProposalLeavesOutMealsAlreadyOnTheWeek(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	saved := saveStaleProposal(t, env)
	if _, _, err := env.planner.AddEntries(ctx, hhA, userAda, testWeek, []planning.NewEntry{{RecipeID: rSalmon, Day: "sun", Servings: 2}}); err != nil {
		t.Fatal(err)
	}
	got, err := env.svc.Proposal(ctx, hhA, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := slotFor(got, rSalmon); ok {
		t.Errorf("proposal still suggests the meal already planned: %+v", got.Slots)
	}
	if len(got.Slots) != 2 || got.Planned != 2 {
		t.Errorf("slots = %d, planned = %d; want 2 and 2", len(got.Slots), got.Planned)
	}

	// Adding the rest doesn't put the planned meal on the week twice.
	res, err := env.svc.Accept(ctx, hhA, userAda, testWeek, saved.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	salmon := 0
	for _, e := range res.Plan.Entries {
		if e.RecipeID == rSalmon {
			salmon++
		}
	}
	if salmon != 1 {
		t.Errorf("salmon is on the week %d times after Add; want once", salmon)
	}
	if !slices.Contains(res.Skipped, SkippedSlot{SlotID: "thu", Day: "thu", Reason: SkipAlreadyPlanned}) {
		t.Errorf("skipped = %+v; want the planned meal reported as already planned", res.Skipped)
	}
}

// "Haven’t had it in 16 weeks" goes once the meal has been made or planned
// since: a meal marked made counts. A meal still not had keeps it.
func TestProposalDropsNotRecentlyOnceTheMealIsHad(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	saveStaleProposal(t, env)
	w := mustWeek(t, testWeek)

	// Tacos made last week, marked made by a member.
	if _, _, err := env.events.Insert(ctx, []events.Event{{
		HouseholdID: hhA, UserID: userAda, Type: events.TypeRecipeCooked, RecipeID: rTacos,
		Week: w.AddWeeks(-1).String(), Payload: events.RecipeCooked{Date: "2026-09-09"}, OccurredAt: testNow.AddDate(0, 0, -6),
	}}); err != nil {
		t.Fatal(err)
	}
	// Curry was planned, but 12 weeks back: still a while.
	env.planner.set(planning.Plan{
		HouseholdID: hhA, Week: w.AddWeeks(-12), Status: planning.StatusDraft,
		Entries: []planning.Entry{{ID: "old-curry", RecipeID: rCurry, Day: "mon", Servings: 2}},
	})

	got, err := env.svc.Proposal(ctx, hhA, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	tacos, ok := slotFor(got, rTacos)
	if !ok {
		t.Fatalf("tacos left out: %+v", got.Slots)
	}
	if slices.ContainsFunc(tacos.Reasons, isNotRecently) {
		t.Errorf("tacos made last week still say %q", tacos.Reasons)
	}
	if !slices.ContainsFunc(tacos.Reasons, func(r Reason) bool { return r.Code == "familiar" }) {
		t.Errorf("tacos lost their other reasons: %+v", tacos.Reasons)
	}
	curry, ok := slotFor(got, rCurry)
	if !ok || !slices.ContainsFunc(curry.Reasons, isNotRecently) {
		t.Errorf("curry, last had 12 weeks back, should keep “Haven’t had it”: %+v", curry.Reasons)
	}

	// Planned on a nearby week reads the same as made.
	env.planner.set(planning.Plan{
		HouseholdID: hhA, Week: w.AddWeeks(-3), Status: planning.StatusDraft,
		Entries: []planning.Entry{{ID: "recent-curry", RecipeID: rCurry, Day: "tue", Servings: 2}},
	})
	again, err := env.svc.Proposal(ctx, hhA, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if curry, _ := slotFor(again, rCurry); slices.ContainsFunc(curry.Reasons, isNotRecently) {
		t.Errorf("curry planned 3 weeks back still says %q", curry.Reasons)
	}
}

// A decided proposal is history: nothing is taken out of it.
func TestDecidedProposalStaysAsItWas(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	saved := saveStaleProposal(t, env)
	saved.Status = StatusAccepted
	if _, err := env.store.SaveProposal(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.planner.AddEntries(ctx, hhA, userAda, testWeek, []planning.NewEntry{{RecipeID: rSalmon, Day: "sun", Servings: 2}}); err != nil {
		t.Fatal(err)
	}
	got, err := env.svc.Proposal(ctx, hhA, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slots) != 3 {
		t.Errorf("decided proposal has %d slots; want all 3", len(got.Slots))
	}
}
