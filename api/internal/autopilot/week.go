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
