package planning

import (
	"errors"
	"testing"
	"time"
)

func TestParseWeek(t *testing.T) {
	valid := []struct {
		in     string
		monday string
	}{
		{"2026-W38", "2026-09-14"},
		{"2026-W01", "2025-12-29"}, // week 1 starts in the previous calendar year
		{"2026-W53", "2026-12-28"}, // 2026 starts on a Thursday, so it has 53 weeks
		{"2020-W53", "2020-12-28"}, // leap year starting on a Wednesday
		{"2027-W01", "2027-01-04"},
	}
	for _, tt := range valid {
		w, err := ParseWeek(tt.in)
		if err != nil {
			t.Errorf("ParseWeek(%q) error = %v", tt.in, err)
			continue
		}
		if w.String() != tt.in {
			t.Errorf("ParseWeek(%q).String() = %q", tt.in, w.String())
		}
		if got := w.Date(Monday); got != tt.monday {
			t.Errorf("%s Monday = %s, want %s", tt.in, got, tt.monday)
		}
	}

	for _, in := range []string{
		"", "2026-38", "2026-W5", "2026-w38", " 2026-W38", "2026-W38 ", "26-W38", "2026-W038",
		"2026-W00", "2026-W54", "2025-W53", "1999-W10", "２０２６-W38",
	} {
		if _, err := ParseWeek(in); !errors.Is(err, ErrInvalidWeek) {
			t.Errorf("ParseWeek(%q) error = %v, want ErrInvalidWeek", in, err)
		}
	}
}

func TestWeekArithmetic(t *testing.T) {
	w := mustWeek(t, testWeek)
	if got := w.Date(Sunday); got != "2026-09-20" {
		t.Errorf("Sunday = %s", got)
	}
	if got := w.Date(Day("someday")); got != "" {
		t.Errorf("Date(unknown) = %q, want empty", got)
	}
	if got := mustWeek(t, "2026-W52").AddWeeks(2).String(); got != "2027-W01" {
		t.Errorf("2026-W52 + 2 = %s, want 2027-W01 (2026 has 53 weeks)", got)
	}
	if got := mustWeek(t, "2027-W01").AddWeeks(-1).String(); got != "2026-W53" {
		t.Errorf("2027-W01 - 1 = %s", got)
	}
	if n := mustWeek(t, "2026-W01").WeeksUntil(mustWeek(t, "2027-W01")); n != 53 {
		t.Errorf("weeks in 2026 = %d, want 53", n)
	}
	if n := w.WeeksUntil(mustWeek(t, "2026-W30")); n != -8 {
		t.Errorf("WeeksUntil(earlier) = %d, want -8", n)
	}

	// Every day from 2020 through 2030 lies in the week WeekOf reports, and
	// that week's string parses back to itself.
	for d := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() <= 2030; d = d.AddDate(0, 0, 1) {
		wk := WeekOf(d)
		monday := wk.Monday()
		if d.Before(monday) || !d.Before(monday.AddDate(0, 0, 7)) || monday.Weekday() != time.Monday {
			t.Fatalf("%s: week %s starts %s", d.Format(time.DateOnly), wk, monday.Format(time.DateOnly))
		}
		if parsed, err := ParseWeek(wk.String()); err != nil || parsed != wk {
			t.Fatalf("ParseWeek(%s) = %v, %v", wk, parsed, err)
		}
	}
}

func TestParseDayAndStatus(t *testing.T) {
	for _, s := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		if d, err := ParseDay(s); err != nil || string(d) != s {
			t.Errorf("ParseDay(%q) = %q, %v", s, d, err)
		}
	}
	for _, s := range []string{"", "Mon", "monday", "7"} {
		if _, err := ParseDay(s); !errors.Is(err, ErrInvalidEntry) {
			t.Errorf("ParseDay(%q) error = %v, want ErrInvalidEntry", s, err)
		}
	}
	for _, s := range []string{"draft", "finalized"} {
		if st, err := ParseStatus(s); err != nil || string(st) != s {
			t.Errorf("ParseStatus(%q) = %q, %v", s, st, err)
		}
	}
	for _, s := range []string{"", "Draft", "ordered", "cooked"} {
		if _, err := ParseStatus(s); !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("ParseStatus(%q) error = %v, want ErrInvalidStatus", s, err)
		}
	}
}
