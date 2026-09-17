package autopilot

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var weekPattern = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)

// WeekStart returns the Monday (UTC midnight) of an ISO week such as
// "2026-W38".
func WeekStart(week string) (time.Time, error) {
	m := weekPattern.FindStringSubmatch(week)
	if m == nil {
		return time.Time{}, fmt.Errorf("%w: week must look like 2026-W38", ErrInvalidRequest)
	}
	year, _ := strconv.Atoi(m[1])
	number, _ := strconv.Atoi(m[2])
	_, last := time.Date(year, time.December, 28, 0, 0, 0, 0, time.UTC).ISOWeek()
	if number < 1 || number > last {
		return time.Time{}, fmt.Errorf("%w: %d has no ISO week %d", ErrInvalidRequest, year, number)
	}
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	offset := (int(jan4.Weekday()) + 6) % 7
	return jan4.AddDate(0, 0, -offset+7*(number-1)), nil
}

// WeekStartOn returns the first day (UTC midnight) of week when weeks start
// on first: the ISO Monday moved by 0 to 3 days for Monday to Thursday, and
// back 3 to 1 days for Friday to Sunday, so the week keeps most of its ISO
// week. An empty or unknown first day means Monday.
func WeekStartOn(week string, first Day) (time.Time, error) {
	monday, err := WeekStart(week)
	if err != nil {
		return time.Time{}, err
	}
	return monday.AddDate(0, 0, startOffset(first)), nil
}

func startOffset(first Day) int {
	switch i := first.Index(); {
	case i < 0:
		return 0
	case i > 3:
		return i - 7
	default:
		return i
	}
}

// Position returns d's place in the week (0 for the first day) when weeks
// start on first, or -1 for an unknown day. An empty or unknown first day
// means Monday.
func (d Day) Position(first Day) int {
	i := d.Index()
	if i < 0 {
		return -1
	}
	return (i - max(first.Index(), 0) + len(Days)) % len(Days)
}

// WeeksBetween returns how many weeks b is after a (negative when before).
// ok is false when either week is invalid.
func WeeksBetween(a, b string) (weeks int, ok bool) {
	ta, err := WeekStart(a)
	if err != nil {
		return 0, false
	}
	tb, err := WeekStart(b)
	if err != nil {
		return 0, false
	}
	return int(tb.Sub(ta).Hours()) / (24 * 7), true
}
