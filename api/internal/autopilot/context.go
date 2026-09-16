package autopilot

import "strconv"

// Signals is optional, typed context about a week or a day: a generic map
// rather than weather- or calendar-specific fields, so a tenant sends what it
// knows and a provider uses what it understands. Every signal is optional;
// providers must plan a sensible week without any of them, and ignore keys or
// values they don't know.
//
// The weekday of a day is always known (DayContext.Day, a slot's Day) and is
// not repeated here.
type Signals map[string]Value

// Value is one signal's value: text or a number.
type Value struct {
	Text     string
	Number   float64
	IsNumber bool
}

// Text returns a text value.
func Text(s string) Value { return Value{Text: s} }

// Number returns a numeric value.
func Number(n float64) Value { return Value{Number: n, IsNumber: true} }

// String formats the value for fingerprints and logs.
func (v Value) String() string {
	if v.IsNumber {
		return strconv.FormatFloat(v.Number, 'f', -1, 64)
	}
	return v.Text
}

// Week signals (WeekContext.Signals).
const (
	// ContextSeason is the season where the household lives: SeasonWinter,
	// SeasonSpring, SeasonSummer, or SeasonFall.
	ContextSeason = "season"
	// ContextOrderDate is a date (YYYY-MM-DD) the household's groceries are
	// ordered or delivered; it repeats weekly, so any occurrence works.
	ContextOrderDate = "orderDate"
)

// Day signals (DayContext.Signals).
const (
	// ContextHoliday is the display name of a holiday on the day
	// ("Thanksgiving").
	ContextHoliday = "holiday"
	// ContextHolidayKind is HolidayFeast, HolidayCookout, or HolidayDayOff.
	ContextHolidayKind = "holidayKind"
	// ContextBusyness is how busy the evening is: BusynessFree, BusynessSome,
	// or BusynessBusy.
	ContextBusyness = "busyness"
	// ContextEveningFreeMinutes is how many minutes the evening has free for
	// cooking (a number).
	ContextEveningFreeMinutes = "eveningFreeMinutes"
	// ContextTemperatureBand is TemperatureCold, TemperatureMild, or
	// TemperatureHot.
	ContextTemperatureBand = "temperatureBand"
	// ContextPrecipitation is PrecipitationNone, PrecipitationRain, or
	// PrecipitationSnow.
	ContextPrecipitation = "precipitation"
)

// Signal values.
const (
	SeasonWinter = "winter"
	SeasonSpring = "spring"
	SeasonSummer = "summer"
	SeasonFall   = "fall"

	HolidayFeast   = "feast"
	HolidayCookout = "cookout"
	HolidayDayOff  = "dayOff"

	BusynessFree = "free"
	BusynessSome = "some"
	BusynessBusy = "busy"

	TemperatureCold = "cold"
	TemperatureMild = "mild"
	TemperatureHot  = "hot"

	PrecipitationNone = "none"
	PrecipitationRain = "rain"
	PrecipitationSnow = "snow"
)
