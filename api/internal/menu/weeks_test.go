package menu

import (
	"context"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

func TestBuildWeekStrip(t *testing.T) {
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	stored := time.Date(2026, 9, 7, 18, 0, 0, 0, time.UTC)
	plans := map[planning.Week]planning.Summary{
		week("2026-W37"): {Week: week("2026-W37"), Status: planning.StatusFinalized, EntryCount: 3, UpdatedAt: stored},
		week("2026-W39"): {Week: week("2026-W39"), Status: planning.StatusDraft, EntryCount: 0, UpdatedAt: stored},
		// A week nobody planned still comes back from planning.List.
		week("2026-W40"): {Week: week("2026-W40"), Status: planning.StatusDraft},
	}
	catalog := []recipes.Recipe{
		newRecipe("r1", "Main", orderWeeks("2023-W05", "2026-W37")),
		newRecipe("r2", "Side", addon(), orderWeeks("2022-W01", "2026-W37")),
		newRecipe("r3", "Other", orderWeeks("2026-W37")),
	}
	cooked := []events.Event{
		{RecipeID: "r1", Week: "2026-W37", Payload: events.RecipeCooked{EntryID: "e1"}},
		{RecipeID: "r1", Week: "2026-W37", Payload: events.RecipeCooked{EntryID: "e1"}}, // a retry counts once
		{RecipeID: "r1", Week: "2026-W37", Payload: events.RecipeCooked{Date: "2026-09-09"}},
		// No week: Sept 14 03:00 UTC is Sept 13 in Denver, the last day of W37.
		{RecipeID: "r3", OccurredAt: time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC), Payload: events.RecipeCooked{}},
	}
	earliestPlanned := week("2024-W10")

	strip := buildWeekStrip(week("2026-W36"), week("2026-W40"), current, loc, plans, catalog, cooked, &earliestPlanned)

	want := []WeekSummary{
		{Week: week("2026-W36"), Timing: TimingPast, Status: WeekStatusNone},
		{Week: week("2026-W37"), Timing: TimingPast, Planned: 3, Cooked: 3, Ordered: 2, Status: string(planning.StatusFinalized)},
		{Week: week("2026-W38"), Timing: TimingCurrent, Status: WeekStatusNone},
		{Week: week("2026-W39"), Timing: TimingUpcoming, Status: string(planning.StatusDraft)},
		{Week: week("2026-W40"), Timing: TimingUpcoming, Status: WeekStatusNone},
	}
	if len(strip.Weeks) != len(want) {
		t.Fatalf("strip has %d weeks, want %d", len(strip.Weeks), len(want))
	}
	for i, got := range strip.Weeks {
		if got != want[i] {
			t.Errorf("week %d = %+v, want %+v", i, got, want[i])
		}
	}
	// The add-on's 2022 delivery doesn't count; the earliest main-meal order
	// beats the earliest planned week.
	if strip.Earliest == nil || *strip.Earliest != week("2023-W05") {
		t.Errorf("earliest = %v, want 2023-W05", strip.Earliest)
	}
}

func TestBuildWeekStripWithoutHistory(t *testing.T) {
	strip := buildWeekStrip(week("2026-W37"), current, current, time.UTC, nil, nil, nil, nil)
	if strip.Earliest != nil {
		t.Errorf("earliest = %v, want none", strip.Earliest)
	}
	for _, w := range strip.Weeks {
		if w.Status != WeekStatusNone || w.Planned != 0 || w.Cooked != 0 || w.Ordered != 0 {
			t.Errorf("week = %+v, want an empty week", w)
		}
	}
}

// Weeks clamps the range to 52 weeks either side and reads plans in ranges
// planning accepts.
func TestWeeksClampsTheRangeAndChunksPlanReads(t *testing.T) {
	f := &fakeSources{
		household: households.Household{ID: "hh", TimeZone: "America/Denver"},
		profile:   recommendations.DefaultProfile("hh"),
		plans:     map[string]planning.Plan{},
	}
	svc := NewService(f.options(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)))

	strip, err := svc.Weeks(context.Background(), "hh", "", 60, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(strip.Weeks) != 105 {
		t.Fatalf("strip has %d weeks, want 105", len(strip.Weeks))
	}
	if first, last := strip.Weeks[0].Week, strip.Weeks[104].Week; first != current.AddWeeks(-52) || last != current.AddWeeks(52) {
		t.Errorf("strip runs %s..%s, want %s..%s", first, last, current.AddWeeks(-52), current.AddWeeks(52))
	}
	if strip.Weeks[52].Timing != TimingCurrent {
		t.Errorf("week 52 = %+v, want the current week in the middle", strip.Weeks[52])
	}
	// The fake fails a range planning would reject, so reaching here means
	// every read was within MaxRangeWeeks.
	if len(f.listRanges) != 5 {
		t.Errorf("plan reads = %v, want 5 chunks", f.listRanges)
	}

	if _, err := svc.Weeks(context.Background(), "hh", "nonsense", 1, 1); err == nil {
		t.Error("Weeks() with an invalid week should fail")
	}
}
