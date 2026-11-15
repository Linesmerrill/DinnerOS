package planning

import (
	"context"
	"sync"
	"testing"
)

type dinnerCounter struct {
	mu    sync.Mutex
	heard map[string]int
}

func (d *dinnerCounter) DinnersPlanned(_ context.Context, householdID string, n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.heard[householdID] += n
}

// Planning meals tells the listener how many dinners: add-ons don't count,
// and a change that fails tells it nothing.
func TestAddEntriesTellsTheDinnerListener(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	heard := &dinnerCounter{heard: map[string]int{}}
	svc.WithDinnerListener(heard)

	if _, _, err := svc.AddEntries(ctx, hhAda, userAda, testWeek, []NewEntry{
		{RecipeID: recipeTacos, Servings: 4}, {RecipeID: recipeBread, Servings: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if heard.heard[hhAda] != 1 {
		t.Errorf("heard %d dinners, want 1 (the add-on doesn't count)", heard.heard[hhAda])
	}
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2})
	if heard.heard[hhAda] != 2 {
		t.Errorf("heard %d dinners, want 2", heard.heard[hhAda])
	}
	if _, _, err := svc.AddEntries(ctx, hhAda, userAda, testWeek, []NewEntry{{RecipeID: recipeBobs, Servings: 2}}); err == nil {
		t.Fatal("another household's recipe was planned")
	}
	if heard.heard[hhAda] != 2 {
		t.Errorf("a failed change was heard: %d", heard.heard[hhAda])
	}
}
