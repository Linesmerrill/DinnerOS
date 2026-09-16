package planning

import (
	"context"
	"errors"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// fakeSkips is a SkipSource with a fixed set per week.
type fakeSkips struct {
	byWeek map[string]grocery.SkipSet
	err    error
	calls  []string
}

func (f *fakeSkips) GrocerySkips(_ context.Context, householdID, week string) (grocery.SkipSet, error) {
	f.calls = append(f.calls, householdID+"/"+week)
	if f.err != nil {
		return nil, f.err
	}
	return f.byWeek[week], nil
}

func skippedNames(g GroceryList) []string {
	var out []string
	for _, it := range g.SkippedItems {
		out = append(out, it.Name)
	}
	return out
}

func listedNames(g GroceryList) []string {
	_, items := viewGroceryList(g)
	var out []string
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func TestGroceryListHoldsBackSkippedIngredients(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	source := &fakeSkips{byWeek: map[string]grocery.SkipSet{
		testWeek: {ingGarlic: grocery.SkipAlways},
	}}
	if got := svc.WithSkips(source); got != svc {
		t.Fatal("WithSkips must return the service it configures")
	}
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	for _, name := range listedNames(g) {
		if name == "Garlic" {
			t.Error("a skipped ingredient must not be on the list")
		}
	}
	if got := skippedNames(g); len(got) != 1 || got[0] != "Garlic" {
		t.Fatalf("SkippedItems = %v, want [Garlic]", got)
	}
	if got := g.SkippedItems[0]; got.Status != grocery.StatusSkipped || got.SkipScope != grocery.SkipAlways {
		t.Errorf("skipped item = status %q scope %q, want skipped/always", got.Status, got.SkipScope)
	}
	// The recipes that wanted it are still named, so nothing is silent.
	if len(g.SkippedItems[0].Sources) == 0 {
		t.Error("a skipped item must still say which recipes needed it")
	}
	if len(source.calls) != 1 || source.calls[0] != hhAda+"/"+testWeek {
		t.Errorf("skips were loaded as %v, want one call for this household and week", source.calls)
	}
}

func TestGroceryListSkipOnceAppliesToItsWeekOnly(t *testing.T) {
	ctx := context.Background()
	const nextWeek = "2026-W39"
	svc, _ := newTestService(t, newMemoryStore())
	svc.WithSkips(&fakeSkips{byWeek: map[string]grocery.SkipSet{
		testWeek: {ingGarlic: grocery.SkipThisWeek},
	}})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, nextWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	this, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	if got := skippedNames(this); len(got) != 1 || got[0] != "Garlic" {
		t.Fatalf("this week's SkippedItems = %v, want [Garlic]", got)
	}

	// Next week it is back on the list with no action from anyone.
	next, err := svc.GroceryList(ctx, hhAda, nextWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	if len(next.SkippedItems) != 0 {
		t.Errorf("next week's SkippedItems = %v, want none", skippedNames(next))
	}
	var found bool
	for _, name := range listedNames(next) {
		found = found || name == "Garlic"
	}
	if !found {
		t.Error("a skip-once must put the ingredient back on next week's list")
	}
}

func TestGroceryListWithoutSkipsBuysEverything(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	if len(g.SkippedItems) != 0 {
		t.Errorf("SkippedItems = %v on a server without skips, want none", skippedNames(g))
	}
}

func TestGroceryListFailsWhenSkipsCannotBeLoaded(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	sentinel := errors.New("boom")
	svc.WithSkips(&fakeSkips{err: sentinel})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	if _, err := svc.GroceryList(ctx, hhAda, testWeek); !errors.Is(err, sentinel) {
		t.Fatalf("GroceryList() error = %v, want the skip failure", err)
	}
}

func TestGroceryListResponseRendersSkippedItems(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	svc.WithSkips(&fakeSkips{byWeek: map[string]grocery.SkipSet{
		testWeek: {ingGarlic: grocery.SkipAlways},
	}})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}

	resp := newGroceryListResponse(g)
	if len(resp.SkippedItems) != 1 {
		t.Fatalf("response SkippedItems = %d, want 1", len(resp.SkippedItems))
	}
	item := resp.SkippedItems[0]
	if item.Name != "Garlic" || item.Status != grocery.StatusSkipped {
		t.Errorf("skipped item = %q/%q, want Garlic/skipped", item.Name, item.Status)
	}
	if item.SkipScope != grocery.SkipAlways || item.SkipText != "Never buying this" {
		t.Errorf("skip wording = %q/%q, want always/\"Never buying this\"", item.SkipScope, item.SkipText)
	}
	// Ordinary items carry no skip fields at all.
	for _, c := range resp.Categories {
		for _, it := range c.Items {
			if it.SkipScope != "" || it.SkipText != "" {
				t.Errorf("item %q carries skip fields it should not: %+v", it.Name, it)
			}
		}
	}
}
