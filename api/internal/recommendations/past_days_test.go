package recommendations

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// pastDay reports whether a slot or open day of testWeek (Monday first) is
// dated before today.
func pastDay(day, today string) bool {
	w, _ := planning.ParseWeek(testWeek)
	return w.DateOn(planning.Monday, planning.Day(day)) < today
}

// Generating partway through a week suggests only today and later: Tuesday
// to Thursday were offered on a Saturday (build 122 review).
func TestGenerateSuggestsNoPastDays(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// Tuesday, September 15, noon in Denver: Monday is gone.
	p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Slots) == 0 {
		t.Fatal("no meals suggested for the rest of the week")
	}
	for _, sl := range p.Slots {
		if pastDay(sl.Day, "2026-09-15") {
			t.Errorf("suggested a past day: %s", sl.Day)
		}
	}
	for _, u := range p.Unfilled {
		if pastDay(u.Day, "2026-09-15") {
			t.Errorf("a past day is open: %s", u.Day)
		}
	}
	if p.Planned != len(p.Slots) {
		t.Errorf("planned = %d, slots = %d", p.Planned, len(p.Slots))
	}
}

// A proposal made early in the week loses the days that have gone by since,
// when it's read and when it's accepted.
func TestSavedProposalDropsDaysGoneBy(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.svc.now = func() time.Time { return beforeTestWeeks }
	p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(p.Slots, func(s Slot) bool { return s.Day == "mon" }) {
		t.Fatalf("want a Monday meal on Sunday; got %+v", p.Slots)
	}
	// Thursday, September 17, in Denver.
	env.svc.now = func() time.Time { return time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC) }
	got, err := env.svc.Proposal(ctx, hhA, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	for _, sl := range got.Slots {
		if pastDay(sl.Day, "2026-09-17") {
			t.Errorf("still suggests %s", sl.Day)
		}
	}
	if len(got.Slots) >= len(p.Slots) || got.Planned != len(got.Slots) {
		t.Errorf("slots = %d of %d, planned = %d", len(got.Slots), len(p.Slots), got.Planned)
	}
	res, err := env.svc.Accept(ctx, hhA, userAda, testWeek, got.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Plan.Entries {
		if pastDay(string(e.Day), "2026-09-17") {
			t.Errorf("accepted a meal on %s", e.Day)
		}
	}
}

// A week that's over gets no suggestions.
func TestWeekGoneByGetsNoSuggestions(t *testing.T) {
	env := newTestEnv(t)
	env.svc.now = func() time.Time { return time.Date(2026, 9, 22, 18, 0, 0, 0, time.UTC) }
	p, err := env.svc.Generate(context.Background(), hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Slots) != 0 || p.Planned != 0 || len(p.Unfilled) != 0 {
		t.Errorf("past week = %+v", p)
	}
}

func TestUpcomingPlanDays(t *testing.T) {
	w, _ := planning.ParseWeek(testWeek)
	weekdays := []autopilot.Day{autopilot.Monday, autopilot.Tuesday, autopilot.Wednesday, autopilot.Thursday, autopilot.Friday}
	tests := []struct {
		name     string
		planDays []autopilot.Day
		today    string
		want     []autopilot.Day
	}{
		{"before the week", weekdays, "2026-09-13", weekdays},
		{"today counts", weekdays, "2026-09-16", []autopilot.Day{autopilot.Wednesday, autopilot.Thursday, autopilot.Friday}},
		{"none set means every day", nil, "2026-09-19", []autopilot.Day{autopilot.Saturday, autopilot.Sunday}},
		{"week over keeps one day, never none", weekdays, "2026-09-25", []autopilot.Day{autopilot.Sunday}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upcomingPlanDays(tt.planDays, w, planning.Monday, tt.today); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
