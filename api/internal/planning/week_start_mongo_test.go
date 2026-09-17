package planning

import (
	"context"
	"testing"
)

// fakeWeekStart is a WeekStartSource with one value for every household.
type fakeWeekStart struct{ day string }

func (f *fakeWeekStart) WeekStartsOn(context.Context, string) (string, error) { return f.day, nil }

// entryDates maps each entry's ID to "week day date" as the household sees it.
func entryDates(t *testing.T, svc *Service, householdID string, weeks ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, week := range weeks {
		p, err := svc.Get(context.Background(), householdID, week)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range p.Entries {
			out[e.ID] = week + " " + string(e.Day) + " " + p.DateOf(e.Day)
		}
	}
	return out
}

func TestIntegrationMoveEntriesForWeekStart(t *testing.T) {
	store, _ := newTestMongoStore(t)
	svc, _ := newTestService(t, store)
	source := &fakeWeekStart{day: "mon"}
	svc.WithWeekStart(source)
	ctx := context.Background()

	// Monday-first weeks, as every plan stored before the setting existed.
	_, monday := mustAdd(t, svc, hhAda, userAda, "2026-W38", NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
	_, sunday := mustAdd(t, svc, hhAda, userAda, "2026-W38", NewEntry{RecipeID: recipeSoup, Day: "sun", Servings: 4})
	_, unscheduled := mustAdd(t, svc, hhAda, userAda, "2026-W38", NewEntry{RecipeID: recipeSoup, Servings: 4})
	_, lastSunday := mustAdd(t, svc, hhAda, userAda, "2026-W37", NewEntry{RecipeID: recipeTacos, Day: "sun", Servings: 2})
	_, other := mustAdd(t, svc, hhBob, userBob, "2026-W38", NewEntry{RecipeID: recipeBobs, Day: "sun", Servings: 2})
	if _, err := svc.SetStatus(ctx, hhAda, "2026-W38", "finalized"); err != nil {
		t.Fatal(err)
	}
	before := entryDates(t, svc, hhAda, "2026-W37", "2026-W38")
	if before[sunday.ID] != "2026-W38 sun 2026-09-20" || before[lastSunday.ID] != "2026-W37 sun 2026-09-13" {
		t.Fatalf("Monday-first dates = %v", before)
	}

	moved, err := svc.ChangeWeekStart(ctx, hhAda, "mon", "sun")
	if err != nil || moved != 2 {
		t.Fatalf("ChangeWeekStart(mon→sun) = %d, %v; want both Sundays moved", moved, err)
	}
	// A retry of the same change (the household save failed) moves nothing twice.
	if moved, err := svc.ChangeWeekStart(ctx, hhAda, "mon", "sun"); err != nil || moved != 0 {
		t.Fatalf("retried ChangeWeekStart = %d, %v; want 0", moved, err)
	}
	source.day = "sun"
	if err := svc.FinishWeekStart(ctx, hhAda); err != nil {
		t.Fatal(err)
	}

	after := entryDates(t, svc, hhAda, "2026-W37", "2026-W38", "2026-W39")
	want := map[string]string{
		monday.ID:      "2026-W38 mon 2026-09-14",
		unscheduled.ID: "2026-W38  ",
		sunday.ID:      "2026-W39 sun 2026-09-20", // same date, next Sunday-first week
		lastSunday.ID:  "2026-W38 sun 2026-09-13",
	}
	if len(after) != len(want) {
		t.Fatalf("entries after the move = %v", after)
	}
	for id, w := range want {
		if after[id] != w {
			t.Errorf("entry %s = %q, want %q", id, after[id], w)
		}
	}
	if p, _ := svc.Get(ctx, hhAda, "2026-W39"); p.Status != StatusDraft {
		t.Errorf("created destination plan status = %s, want draft", p.Status)
	}
	if bob := entryDates(t, svc, hhBob, "2026-W38"); bob[other.ID] == "" {
		t.Errorf("another household's entry moved: %v", bob)
	}

	// And back: Monday-first again restores the original weeks.
	if moved, err := svc.ChangeWeekStart(ctx, hhAda, "sun", "mon"); err != nil || moved != 2 {
		t.Fatalf("ChangeWeekStart(sun→mon) = %d, %v", moved, err)
	}
	source.day = "mon"
	if err := svc.FinishWeekStart(ctx, hhAda); err != nil {
		t.Fatal(err)
	}
	if back := entryDates(t, svc, hhAda, "2026-W37", "2026-W38", "2026-W39"); back[sunday.ID] != before[sunday.ID] || back[lastSunday.ID] != before[lastSunday.ID] || back[monday.ID] != before[monday.ID] {
		t.Errorf("after moving back = %v, want %v", back, before)
	}

	// Saturday-first: Sunday stays put, Saturdays move.
	_, saturday := mustAdd(t, svc, hhAda, userAda, "2026-W39", NewEntry{RecipeID: recipeTacos, Day: "sat", Servings: 2}) // Sat Sep 26
	if _, err := svc.ChangeWeekStart(ctx, hhAda, "mon", "sat"); err != nil {
		t.Fatal(err)
	}
	source.day = "sat"
	if got := entryDates(t, svc, hhAda, "2026-W40")[saturday.ID]; got != "2026-W40 sat 2026-09-26" {
		t.Errorf("Saturday-first Saturday = %q, want 2026-W40 sat 2026-09-26", got)
	}
}
