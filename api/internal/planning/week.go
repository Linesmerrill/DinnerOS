package planning

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Week is an ISO 8601 week ("2026-W38"), the key plans and every other weekly
// document are stored under. Which seven dates a week covers depends on the
// household's first day of the week (StartOn, DateOn, WeekOfOn): with Monday
// they are the ISO week itself, and with another first day the range shifts by
// at most three days, so it always keeps most of its ISO week
// (docs/architecture.md, decision 503). Weeks are calendar concepts, so no
// time zone is involved.
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

// WeekOf returns the ISO week containing t's calendar date: the week key for a
// household whose weeks start on Monday. WeekOfOn handles any first day.
func WeekOf(t time.Time) Week {
	y, w := t.ISOWeek()
	return Week{Year: y, Number: w}
}

// WeekOfOn returns the week containing t's calendar date (in t's location)
// when weeks start on first.
func WeekOfOn(t time.Time, first Day) Week {
	date := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return WeekOf(date.AddDate(0, 0, -startOffset(first)))
}

// String returns "YYYY-Www".
func (w Week) String() string { return fmt.Sprintf("%04d-W%02d", w.Year, w.Number) }

// Monday returns the ISO week's Monday at midnight UTC.
func (w Week) Monday() time.Time {
	// January 4 is always in week 1.
	jan4 := time.Date(w.Year, time.January, 4, 0, 0, 0, 0, time.UTC)
	offset := (int(jan4.Weekday()) + 6) % 7 // days since Monday
	return jan4.AddDate(0, 0, -offset+7*(w.Number-1))
}

// startOffset is how many days the week's first day falls after its ISO
// Monday: 0 to 3 for Monday to Thursday, -3 to -1 for Friday to Sunday. A week
// therefore always keeps at least four days of its ISO week (Sunday-first
// 2026-W38 is Sep 13–19), and a date's week is the ISO week of the date moved
// back by the offset. An unknown first day means Monday.
func startOffset(first Day) int {
	switch i := first.index(); {
	case i < 0:
		return 0
	case i > 3:
		return i - 7
	default:
		return i
	}
}

// StartOn returns the week's first day at midnight UTC when weeks start on
// first.
func (w Week) StartOn(first Day) time.Time {
	return w.Monday().AddDate(0, 0, startOffset(first))
}

// StartDateOn returns the week's first date ("YYYY-MM-DD") when weeks start on
// first.
func (w Week) StartDateOn(first Day) string { return w.StartOn(first).Format(time.DateOnly) }

// EndDateOn returns the week's last date ("YYYY-MM-DD") when weeks start on
// first.
func (w Week) EndDateOn(first Day) string {
	return w.StartOn(first).AddDate(0, 0, 6).Format(time.DateOnly)
}

// DateOn returns the calendar date ("YYYY-MM-DD") of day d in week w when weeks
// start on first, or "" for an unknown day.
func (w Week) DateOn(first, d Day) string {
	if d.index() < 0 {
		return ""
	}
	return w.StartOn(first).AddDate(0, 0, d.OrderOn(first)).Format(time.DateOnly)
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

// DefaultWeekStart is the first day of the week for new households.
// LegacyWeekStart is the first day of households that never chose one: plans
// stored before the setting existed are laid out in ISO weeks.
const (
	DefaultWeekStart = Sunday
	LegacyWeekStart  = Monday
)

// ParseDay validates a day code.
func ParseDay(s string) (Day, error) {
	for _, d := range dayOrder {
		if string(d) == s {
			return d, nil
		}
	}
	return "", fmt.Errorf("%w: day must be one of mon, tue, wed, thu, fri, sat, sun, or null", ErrInvalidEntry)
}

// index returns d's position in an ISO week (0 for Monday), or -1.
func (d Day) index() int {
	for i, known := range dayOrder {
		if known == d {
			return i
		}
	}
	return -1
}

// OrderOn returns d's position in the week (0 for the first day) when weeks
// start on first, or -1 for an unknown day. An unknown first day means Monday.
func (d Day) OrderOn(first Day) int {
	i := d.index()
	if i < 0 {
		return -1
	}
	return (i - max(first.index(), 0) + len(dayOrder)) % len(dayOrder)
}

// DaysFrom returns the seven days in week order when weeks start on first.
func DaysFrom(first Day) []Day {
	f := max(first.index(), 0)
	out := make([]Day, 0, len(dayOrder))
	for i := range dayOrder {
		out = append(out, dayOrder[(f+i)%len(dayOrder)])
	}
	return out
}

// Date returns the calendar date ("YYYY-MM-DD") of day d in the ISO week w
// (weeks starting on Monday). Code that shows or schedules a household's week
// uses DateOn with the household's first day, or Plan.DateOf.
func (w Week) Date(d Day) string { return w.DateOn(Monday, d) }
