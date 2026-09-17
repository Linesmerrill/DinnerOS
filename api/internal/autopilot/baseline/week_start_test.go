package baseline

import (
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

func TestWeekStartDatesAndNeighbors(t *testing.T) {
	start, err := autopilot.WeekStartOn("2026-W38", autopilot.Sunday)
	if err != nil || start.Format(time.DateOnly) != "2026-09-13" {
		t.Fatalf("WeekStartOn(2026-W38, sun) = %v, %v; want 2026-09-13", start, err)
	}
	m := &model{weekStart: start, firstDay: autopilot.Sunday.Index()}
	for day, want := range map[autopilot.Day]string{
		autopilot.Sunday: "2026-09-13", autopilot.Monday: "2026-09-14", autopilot.Saturday: "2026-09-19",
	} {
		if got := m.date(day.Index()).Format(time.DateOnly); got != want {
			t.Errorf("date(%s) = %s, want %s", day, got, want)
		}
	}
	idx := func(days ...autopilot.Day) []int {
		var out []int
		for _, d := range days {
			out = append(out, d.Index())
		}
		return out
	}
	for day, want := range map[autopilot.Day][]int{
		autopilot.Sunday:   idx(autopilot.Monday),
		autopilot.Saturday: idx(autopilot.Friday),
		autopilot.Monday:   idx(autopilot.Sunday, autopilot.Tuesday),
	} {
		if got := m.neighbors(day.Index()); !slices.Equal(got, want) {
			t.Errorf("Sunday-first neighbors(%s) = %v, want %v", day, got, want)
		}
	}
	iso := &model{}
	if got := iso.neighbors(autopilot.Sunday.Index()); !slices.Equal(got, idx(autopilot.Saturday)) {
		t.Errorf("Monday-first neighbors(sun) = %v", got)
	}
	if monday, _ := autopilot.WeekStartOn("2026-W38", ""); monday.Format(time.DateOnly) != "2026-09-14" {
		t.Errorf("WeekStartOn with no first day = %s, want the ISO Monday", monday.Format(time.DateOnly))
	}
}
