package recommendations

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// A pending proposal leaves out a meal whose recipe was deleted since it was
// made: it showed as a placeholder suggestion that couldn't be added (build
// 122 review). Accepting works on the meals that are left.
func TestProposalLeavesOutDeletedRecipes(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := env.svc
	p, err := svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Slots) < 2 {
		t.Fatalf("proposal has %d meals; want at least 2", len(p.Slots))
	}
	gone := p.Slots[0].RecipeID
	deletedOne := false
	for i, r := range env.recipes.list {
		if r.ID == gone && r.HouseholdID == hhA {
			env.recipes.list[i].DeletedAt = time.Now()
			deletedOne = true
		}
	}
	if !deletedOne {
		t.Fatalf("recipe %s not in the fake library", gone)
	}
	got, err := svc.Proposal(ctx, hhA, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(got.Slots, func(s Slot) bool { return s.RecipeID == gone }) {
		t.Errorf("proposal still suggests the deleted recipe: %+v", got.Slots)
	}
	if len(got.Slots) != len(p.Slots)-1 || got.Planned != len(got.Slots) {
		t.Errorf("slots = %d, planned = %d; want %d", len(got.Slots), got.Planned, len(p.Slots)-1)
	}
	// Wholly removed from the library reads the same.
	env.recipes.list = slices.DeleteFunc(env.recipes.list, func(r recipes.Recipe) bool { return r.ID == gone })
	if again, err := svc.Proposal(ctx, hhA, testWeek); err != nil || len(again.Slots) != len(got.Slots) {
		t.Errorf("after removal: %d slots, %v", len(again.Slots), err)
	}
}
