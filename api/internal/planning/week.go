package planning

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Week is an ISO 8601 week ("2026-W38"). Weeks start on Monday. ISO weeks are
// calendar concepts, so no time zone is involved.
type Week struct {
	Year   int
	Number int
}

var weekPattern = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)

// Year bounds keep plan keys four-digit, so they sort as strings.
const (
	minWeekYear = 2000
	maxWeekYear = 9999
)

// ParseWeek parses "YYYY-Www". The week number must exist in that year: most
// years have 52 ISO weeks, some have 53.
func ParseWeek(s string) (Week, error) {
	m := weekPattern.FindStringSubmatch(s)
	if m == nil {
		return Week{}, fmt.Errorf("%w: week must look like 2026-W38", ErrInvalidWeek)
	}
	year, _ := strconv.Atoi(m[1])
	number, _ := strconv.Atoi(m[2])
	if year < minWeekYear || year > maxWeekYear {
		return Week{}, fmt.Errorf("%w: week year must be between %d and %d", ErrInvalidWeek, minWeekYear, maxWeekYear)
	}
	if number < 1 || number > weeksInYear(year) {
		return Week{}, fmt.Errorf("%w: %d has no ISO week %d", ErrInvalidWeek, year, number)
	}
	return Week{Year: year, Number: number}, nil
}

// weeksInYear returns 52 or 53. December 28 is always in the year's last week.
func weeksInYear(year int) int {
	_, w := time.Date(year, time.December, 28, 0, 0, 0, 0, time.UTC).ISOWeek()
	return w
}

// WeekOf returns the ISO week containing t's calendar date.
func WeekOf(t time.Time) Week {
	y, w := t.ISOWeek()
	return Week{Year: y, Number: w}
}

// String returns "YYYY-Www".
func (w Week) String() string { return fmt.Sprintf("%04d-W%02d", w.Year, w.Number) }

// Monday returns the week's first day at midnight UTC.
func (w Week) Monday() time.Time {
	// January 4 is always in week 1.
	jan4 := time.Date(w.Year, time.January, 4, 0, 0, 0, 0, time.UTC)
	offset := (int(jan4.Weekday()) + 6) % 7 // days since Monday
	return jan4.AddDate(0, 0, -offset+7*(w.Number-1))
}

// AddWeeks returns the week n weeks later (earlier when n is negative).
func (w Week) AddWeeks(n int) Week { return WeekOf(w.Monday().AddDate(0, 0, 7*n)) }

// WeeksUntil returns how many weeks later other is (negative when earlier).
func (w Week) WeeksUntil(other Week) int {
	return int(other.Monday().Sub(w.Monday()).Hours()) / (24 * 7)
}

// Day is a weekday within a plan's week.
type Day string

// Days of the week, Monday first as in ISO weeks.
const (
	Monday    Day = "mon"
	Tuesday   Day = "tue"
	Wednesday Day = "wed"
	Thursday  Day = "thu"
	Friday    Day = "fri"
	Saturday  Day = "sat"
	Sunday    Day = "sun"
)

var dayOrder = []Day{Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday}

// ParseDay validates a day code.
func ParseDay(s string) (Day, error) {
	for _, d := range dayOrder {
		if string(d) == s {
			return d, nil
		}
	}
	return "", fmt.Errorf("%w: day must be one of mon, tue, wed, thu, fri, sat, sun, or null", ErrInvalidEntry)
}

// Date returns the calendar date ("YYYY-MM-DD") of day d in week w.
func (w Week) Date(d Day) string {
	for i, known := range dayOrder {
		if known == d {
			return w.Monday().AddDate(0, 0, i).Format(time.DateOnly)
		}
	}
	return ""
}
