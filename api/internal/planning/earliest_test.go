package planning

import (
	"context"
	"testing"
	"time"
)

func TestEarliestPlannedWeek(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc := NewService(store, nil)
	const hh = "hh1"
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	if _, ok, err := svc.EarliestPlannedWeek(ctx, hh); err != nil || ok {
		t.Fatalf("EarliestPlannedWeek() with no plans = ok %v, err %v; want none", ok, err)
	}
	if _, _, err := svc.EarliestPlannedWeek(ctx, ""); err == nil {
		t.Error("EarliestPlannedWeek(\"\") should require a household")
	}

	w38, _ := ParseWeek("2026-W38")
	w30, _ := ParseWeek("2026-W30")
	w12, _ := ParseWeek("2026-W12")
	for _, w := range []Week{w38, w30} {
		if _, _, err := store.AddEntries(ctx, hh, w, []Entry{{RecipeID: "r1", Servings: 2}}, MaxEntriesPerWeek, now); err != nil {
			t.Fatal(err)
		}
	}
	// An earlier plan without entries (only a status) doesn't count, and
	// another household's plans are ignored.
	if _, err := store.SetStatus(ctx, hh, w12, StatusFinalized, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddEntries(ctx, "other", w12, []Entry{{RecipeID: "r1", Servings: 2}}, MaxEntriesPerWeek, now); err != nil {
		t.Fatal(err)
	}

	got, ok, err := svc.EarliestPlannedWeek(ctx, hh)
	if err != nil || !ok || got != w30 {
		t.Errorf("EarliestPlannedWeek() = %v, %v, %v; want %v", got, ok, err, w30)
	}
}
