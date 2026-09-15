package planning

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// fakePantry is a PantrySource with a fixed snapshot.
type fakePantry struct {
	stock grocery.PantryStock
	err   error
	calls []string
}

func (f *fakePantry) GroceryPantry(_ context.Context, householdID string) (grocery.PantryStock, error) {
	f.calls = append(f.calls, householdID)
	return f.stock, f.err
}

func statusesByName(g GroceryList) map[string]grocery.Status {
	out := map[string]grocery.Status{}
	_, items := viewGroceryList(g)
	for _, it := range items {
		out[it.Name] = it.Status
	}
	return out
}

func TestGroceryListAppliesPantry(t *testing.T) {
	ctx := context.Background()
	pantry := &fakePantry{stock: grocery.PantryStock{
		InStock:    map[string]bool{ingOliveOil: true, ingGarlic: true},
		OutOfStock: map[string]bool{},
	}}
	svc, _ := newTestService(t, newMemoryStore())
	if got := svc.WithPantry(pantry); got != svc {
		t.Fatal("WithPantry must return the service it configures")
	}
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})

	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	want := map[string]grocery.Status{
		"Olive Oil":    grocery.StatusInPantry,   // in stock; the soup also marks it a staple
		"Garlic":       grocery.StatusInPantry,   // in stock, not a staple
		"Salt":         grocery.StatusPantryHint, // both recipes mark it a staple; the pantry doesn't list it
		"Sour Cream":   grocery.StatusToBuy,
		"Yellow Onion": grocery.StatusToBuy,
	}
	if got := statusesByName(g); !maps.Equal(got, want) {
		t.Errorf("statuses = %v, want %v", got, want)
	}
	if !g.PantryApplied || !newGroceryListResponse(g).PantryApplied {
		t.Errorf("pantryApplied = %v, want true", g.PantryApplied)
	}
	if !slices.Equal(pantry.calls, []string{hhAda}) {
		t.Errorf("pantry loads = %v, want one for %s", pantry.calls, hhAda)
	}

	// Salt recorded as out: the staple hint no longer applies.
	pantry.stock.OutOfStock[ingSalt] = true
	g, err = svc.GroceryList(ctx, hhAda, testWeek)
	if s := statusesByName(g)["Salt"]; err != nil || s != grocery.StatusToBuy {
		t.Errorf("salt when out = %s, %v; want toBuy", s, err)
	}

	// An empty week needs no pantry read, but the pantry still applies.
	calls := len(pantry.calls)
	if empty, err := svc.GroceryList(ctx, hhAda, "2026-W39"); err != nil || !empty.PantryApplied || len(pantry.calls) != calls {
		t.Errorf("empty week = %+v, %v after %d pantry loads", empty, err, len(pantry.calls)-calls)
	}

	// A pantry failure fails the list instead of silently ignoring the pantry.
	pantry.err = errors.New("pantry unavailable")
	if _, err := svc.GroceryList(ctx, hhAda, testWeek); !errors.Is(err, pantry.err) {
		t.Errorf("GroceryList() with a failing pantry error = %v", err)
	}
}

func TestGroceryListWithoutPantry(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})
	g, err := svc.GroceryList(context.Background(), hhAda, testWeek)
	if err != nil || g.PantryApplied || newGroceryListResponse(g).PantryApplied {
		t.Fatalf("GroceryList() = pantryApplied %v, %v; want false", g.PantryApplied, err)
	}
	if s := statusesByName(g)["Olive Oil"]; s != grocery.StatusPantryHint {
		t.Errorf("olive oil = %s, want pantryHint without a pantry", s)
	}
}
