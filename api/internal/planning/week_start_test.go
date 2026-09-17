package planning

import (
	"slices"
	"testing"
	"time"
)

func TestWeekDatesOnFirstDay(t *testing.T) {
	w := mustWeek(t, "2026-W38") // ISO: Mon Sep 14 – Sun Sep 20
	tests := []struct {
		first      Day
		start, end string
		sun, mon   string
	}{
		{Monday, "2026-09-14", "2026-09-20", "2026-09-20", "2026-09-14"},
		{Sunday, "2026-09-13", "2026-09-19", "2026-09-13", "2026-09-14"},
		{Saturday, "2026-09-12", "2026-09-18", "2026-09-13", "2026-09-14"},
		{Friday, "2026-09-11", "2026-09-17", "2026-09-13", "2026-09-14"},
		{Thursday, "2026-09-17", "2026-09-23", "2026-09-20", "2026-09-21"},
		{"", "2026-09-14", "2026-09-20", "2026-09-20", "2026-09-14"},
	}
	for _, tt := range tests {
		if got := w.StartDateOn(tt.first); got != tt.start {
			t.Errorf("%q: start = %s, want %s", tt.first, got, tt.start)
		}
		if got := w.EndDateOn(tt.first); got != tt.end {
			t.Errorf("%q: end = %s, want %s", tt.first, got, tt.end)
		}
		if got := w.DateOn(tt.first, Sunday); got != tt.sun {
			t.Errorf("%q: Sunday = %s, want %s", tt.first, got, tt.sun)
		}
		if got := w.DateOn(tt.first, Monday); got != tt.mon {
			t.Errorf("%q: Monday = %s, want %s", tt.first, got, tt.mon)
		}
	}
	if got := w.DateOn(Sunday, Day("someday")); got != "" {
		t.Errorf("DateOn(unknown) = %q, want empty", got)
	}

	// Today in the user's report: Thursday Sep 17, 2026 is in 2026-W38 with
	// Sunday-first weeks, Saturday Sep 19 too, and Sunday Sep 20 starts W39.
	for date, want := range map[string]string{
		"2026-09-13": "2026-W38", "2026-09-17": "2026-W38", "2026-09-19": "2026-W38", "2026-09-20": "2026-W39",
		// Year boundaries: Sunday-first 2027-W01 is Sun Jan 3 – Sat Jan 9, 2027.
		"2027-01-02": "2026-W53", "2027-01-03": "2027-W01", "2020-12-27": "2020-W53", "2021-01-03": "2021-W01",
	} {
		d, _ := time.Parse(time.DateOnly, date)
		if got := WeekOfOn(d, Sunday).String(); got != want {
			t.Errorf("WeekOfOn(%s, sun) = %s, want %s", date, got, want)
		}
	}
	// The calendar date in the time's own zone decides: 11pm Saturday in
	// Denver is Sunday in UTC.
	denver, _ := time.LoadLocation("America/Denver")
	if got := WeekOfOn(time.Date(2026, 9, 19, 23, 0, 0, 0, denver), Sunday).String(); got != "2026-W38" {
		t.Errorf("late Saturday in Denver = %s, want 2026-W38", got)
	}

	if got := DaysFrom(Sunday); !slices.Equal(got, []Day{Sunday, Monday, Tuesday, Wednesday, Thursday, Friday, Saturday}) {
		t.Errorf("DaysFrom(sun) = %v", got)
	}
	if got := DaysFrom(""); !slices.Equal(got, dayOrder) {
		t.Errorf("DaysFrom(\"\") = %v", got)
	}
	if Sunday.OrderOn(Sunday) != 0 || Saturday.OrderOn(Sunday) != 6 || Monday.OrderOn(Saturday) != 2 || Day("x").OrderOn(Sunday) != -1 {
		t.Error("OrderOn gives the wrong positions")
	}
}

func TestPlanResponseFollowsFirstDay(t *testing.T) {
	sun := Sunday
	p := Plan{Week: mustWeek(t, "2026-W38"), FirstDay: Sunday, Entries: []Entry{{ID: "a", Day: Sunday}, {ID: "b", Day: Saturday}, {ID: "c"}}}
	resp := NewPlanResponse(p)
	if resp.StartDate != "2026-09-13" || resp.EndDate != "2026-09-19" {
		t.Errorf("plan dates = %s – %s, want 2026-09-13 – 2026-09-19", resp.StartDate, resp.EndDate)
	}
	if e := resp.Entries[0]; e.Day == nil || *e.Day != sun || *e.Date != "2026-09-13" {
		t.Errorf("Sunday entry = %+v", e)
	}
	if e := resp.Entries[1]; *e.Date != "2026-09-19" {
		t.Errorf("Saturday entry date = %s", *e.Date)
	}
	if e := resp.Entries[2]; e.Day != nil || e.Date != nil {
		t.Errorf("unscheduled entry = %+v", e)
	}
	p.FirstDay = ""
	if resp := NewPlanResponse(p); resp.StartDate != "2026-09-14" || *resp.Entries[0].Date != "2026-09-20" {
		t.Errorf("unset first day: %s, Sunday %s; want ISO dates", resp.StartDate, *resp.Entries[0].Date)
	}
}

// For every first day and every date from 2020 through 2030, the date lies in
// the week WeekOfOn reports, on the weekday it names, and that week keeps at
// least four days of its ISO week.
func TestWeekOfOnRoundTrip(t *testing.T) {
	for _, first := range dayOrder {
		for d := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() <= 2030; d = d.AddDate(0, 0, 1) {
			wk := WeekOfOn(d, first)
			start := wk.StartOn(first)
			if d.Before(start) || !d.Before(start.AddDate(0, 0, 7)) {
				t.Fatalf("%s (%s): week %s starts %s", d.Format(time.DateOnly), first, wk, start.Format(time.DateOnly))
			}
			if start.Weekday() != time.Weekday((first.index()+1)%7) {
				t.Fatalf("%s: week %s starts on %s", first, wk, start.Weekday())
			}
			weekday := dayOrder[(int(d.Weekday())+6)%7]
			if got := wk.DateOn(first, weekday); got != d.Format(time.DateOnly) {
				t.Fatalf("%s (%s): %s of %s = %s", d.Format(time.DateOnly), first, weekday, wk, got)
			}
			if diff := int(start.Sub(wk.Monday()).Hours() / 24); diff < -3 || diff > 3 {
				t.Fatalf("%s: week %s starts %d days from its ISO Monday", first, wk, diff)
			}
		}
	}
}
