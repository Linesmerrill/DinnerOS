package recommendations

import (
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// Context signals (docs/autopilot.md#context-engine).
//
// DinnerOS computes the week's season, holidays, and order date on the server
// from the household's time zone and order day, with no outside service.
// Calendar busyness and weather are computed on the member's phone, which
// sends only derived values with a generate request (DeviceSignals): never
// calendar events, forecasts, or a location.

// Values accepted in device signals.
var (
	BusynessValues      = []string{autopilot.BusynessFree, autopilot.BusynessSome, autopilot.BusynessBusy}
	TemperatureBands    = []string{autopilot.TemperatureCold, autopilot.TemperatureMild, autopilot.TemperatureHot}
	PrecipitationValues = []string{autopilot.PrecipitationNone, autopilot.PrecipitationRain, autopilot.PrecipitationSnow}
)

// MaxEveningFreeMinutes bounds DeviceDaySignals.EveningFreeMinutes.
const MaxEveningFreeMinutes = 24 * 60

// DeviceDaySignals are one day's signals derived on the member's device. Every
// field is optional; empty or zero means unknown.
type DeviceDaySignals struct {
	Day string
	// Busyness is free, some, or busy (from the calendar).
	Busyness string
	// EveningFreeMinutes is how long the evening has free for cooking (from
	// the calendar); 0 means unknown.
	EveningFreeMinutes int
	// TemperatureBand is cold, mild, or hot (from the forecast).
	TemperatureBand string
	// Precipitation is none, rain, or snow (from the forecast).
	Precipitation string
}

func (d DeviceDaySignals) empty() bool {
	return d.Busyness == "" && d.EveningFreeMinutes == 0 && d.TemperatureBand == "" && d.Precipitation == ""
}

// DeviceSignals are the signals a device sent for a week.
type DeviceSignals struct {
	Days []DeviceDaySignals
}

// Holiday is a US holiday on a day of the week.
type Holiday struct {
	Day  string
	Name string
	// Kind is feast, cookout, or dayOff.
	Kind string
}

// ProposalContext is the context a proposal was planned with.
type ProposalContext struct {
	// Season is winter, spring, summer, or fall; empty when unknown.
	Season string
	// OrderDate is the date of the household's order day in the week, or "".
	OrderDate string
	Holidays  []Holiday
	Device    DeviceSignals
}

// normalizeDeviceSignals validates device signals and orders them by day.
// Days with nothing known are dropped.
func normalizeDeviceSignals(in DeviceSignals) (DeviceSignals, error) {
	var out DeviceSignals
	seen := map[string]bool{}
	for _, d := range in.Days {
		switch {
		case !slices.Contains(optionValues(DayOptions), d.Day):
			return DeviceSignals{}, invalidf("signals.days.day must be one of mon, tue, wed, thu, fri, sat, sun")
		case seen[d.Day]:
			return DeviceSignals{}, invalidf("signals.days: %s appears more than once", d.Day)
		case d.Busyness != "" && !slices.Contains(BusynessValues, d.Busyness):
			return DeviceSignals{}, invalidf("signals.days.busyness must be one of %s", strings.Join(BusynessValues, ", "))
		case d.EveningFreeMinutes < 0 || d.EveningFreeMinutes > MaxEveningFreeMinutes:
			return DeviceSignals{}, invalidf("signals.days.eveningFreeMinutes must be between 1 and %d", MaxEveningFreeMinutes)
		case d.TemperatureBand != "" && !slices.Contains(TemperatureBands, d.TemperatureBand):
			return DeviceSignals{}, invalidf("signals.days.temperatureBand must be one of %s", strings.Join(TemperatureBands, ", "))
		case d.Precipitation != "" && !slices.Contains(PrecipitationValues, d.Precipitation):
			return DeviceSignals{}, invalidf("signals.days.precipitation must be one of %s", strings.Join(PrecipitationValues, ", "))
		}
		seen[d.Day] = true
		if !d.empty() {
			out.Days = append(out.Days, d)
		}
	}
	slices.SortFunc(out.Days, func(a, b DeviceDaySignals) int {
		return slices.Index(optionValues(DayOptions), a.Day) - slices.Index(optionValues(DayOptions), b.Day)
	})
	return out, nil
}

// weekContext computes the server-side signals for a week and adds the
// device's.
func weekContext(w planning.Week, timeZone, orderDay string, device DeviceSignals) ProposalContext {
	c := ProposalContext{Season: seasonOf(w.Monday().AddDate(0, 0, 3), timeZone), Device: device}
	if orderDay != "" {
		c.OrderDate = w.Date(planning.Day(orderDay))
	}
	for i, day := range optionValues(DayOptions) {
		if h, ok := usHoliday(w.Monday().AddDate(0, 0, i)); ok {
			h.Day = day
			c.Holidays = append(c.Holidays, h)
		}
	}
	return c
}

// apply adds the context's signals to the provider's week context.
func (c ProposalContext) apply(wc *autopilot.WeekContext) {
	week := autopilot.Signals{}
	if c.Season != "" {
		week[autopilot.ContextSeason] = autopilot.Text(c.Season)
	}
	if c.OrderDate != "" {
		week[autopilot.ContextOrderDate] = autopilot.Text(c.OrderDate)
	}
	if len(week) > 0 {
		wc.Signals = week
	}
	day := func(code string) autopilot.Signals {
		i := slices.IndexFunc(wc.Days, func(d autopilot.DayContext) bool { return string(d.Day) == code })
		if i < 0 {
			wc.Days = append(wc.Days, autopilot.DayContext{Day: autopilot.Day(code)})
			i = len(wc.Days) - 1
		}
		if wc.Days[i].Signals == nil {
			wc.Days[i].Signals = autopilot.Signals{}
		}
		return wc.Days[i].Signals
	}
	for _, h := range c.Holidays {
		s := day(h.Day)
		s[autopilot.ContextHoliday] = autopilot.Text(h.Name)
		s[autopilot.ContextHolidayKind] = autopilot.Text(h.Kind)
	}
	for _, d := range c.Device.Days {
		s := day(d.Day)
		if d.Busyness != "" {
			s[autopilot.ContextBusyness] = autopilot.Text(d.Busyness)
		}
		if d.EveningFreeMinutes > 0 {
			s[autopilot.ContextEveningFreeMinutes] = autopilot.Number(float64(d.EveningFreeMinutes))
		}
		if d.TemperatureBand != "" {
			s[autopilot.ContextTemperatureBand] = autopilot.Text(d.TemperatureBand)
		}
		if d.Precipitation != "" {
			s[autopilot.ContextPrecipitation] = autopilot.Text(d.Precipitation)
		}
	}
}

// southernZones are time-zone prefixes in the southern hemisphere, where the
// seasons are reversed. A time zone is the only location a household has, so
// this is approximate: tropical zones get temperate seasons.
var southernZones = []string{
	"Australia/", "Antarctica/", "Pacific/Auckland", "Pacific/Chatham", "Pacific/Fiji", "Pacific/Tongatapu", "Pacific/Apia",
	"America/Argentina/", "America/Buenos_Aires", "America/Santiago", "America/Punta_Arenas", "America/Sao_Paulo",
	"America/Montevideo", "America/Asuncion", "America/La_Paz", "America/Lima", "Africa/Johannesburg", "Africa/Maputo",
	"Africa/Harare", "Africa/Windhoek", "Africa/Gaborone", "Africa/Lusaka", "Indian/Mauritius", "Indian/Reunion", "Atlantic/Stanley",
}

// seasonOf returns the meteorological season of date where the time zone is:
// December–February is winter in the northern hemisphere.
func seasonOf(date time.Time, timeZone string) string {
	month := int(date.Month())
	if slices.ContainsFunc(southernZones, func(prefix string) bool { return strings.HasPrefix(timeZone, prefix) }) {
		month = (month+5)%12 + 1
	}
	switch month {
	case 12, 1, 2:
		return autopilot.SeasonWinter
	case 3, 4, 5:
		return autopilot.SeasonSpring
	case 6, 7, 8:
		return autopilot.SeasonSummer
	}
	return autopilot.SeasonFall
}

// usHoliday reports whether date is a US holiday Autopilot plans around:
// feasts (a special dinner that day, easy nights around it), cookouts, and
// days off (time for a longer cook). Computed locally.
func usHoliday(date time.Time) (Holiday, bool) {
	y, m, d := date.Date()
	nth := func(month time.Month, weekday time.Weekday, n int) bool {
		return m == month && date.Weekday() == weekday && (d-1)/7 == n-1
	}
	last := func(month time.Month, weekday time.Weekday) bool {
		return m == month && date.Weekday() == weekday && d+7 > daysIn(y, month)
	}
	easterMonth, easterDay := easter(y)
	switch {
	case m == time.January && d == 1:
		return Holiday{Name: "New Year's Day", Kind: autopilot.HolidayDayOff}, true
	case nth(time.January, time.Monday, 3):
		return Holiday{Name: "Martin Luther King Jr. Day", Kind: autopilot.HolidayDayOff}, true
	case nth(time.February, time.Monday, 3):
		return Holiday{Name: "Presidents' Day", Kind: autopilot.HolidayDayOff}, true
	case m == easterMonth && d == easterDay:
		return Holiday{Name: "Easter", Kind: autopilot.HolidayFeast}, true
	case last(time.May, time.Monday):
		return Holiday{Name: "Memorial Day", Kind: autopilot.HolidayCookout}, true
	case m == time.June && d == 19:
		return Holiday{Name: "Juneteenth", Kind: autopilot.HolidayDayOff}, true
	case m == time.July && d == 4:
		return Holiday{Name: "the Fourth of July", Kind: autopilot.HolidayCookout}, true
	case nth(time.September, time.Monday, 1):
		return Holiday{Name: "Labor Day", Kind: autopilot.HolidayCookout}, true
	case nth(time.November, time.Thursday, 4):
		return Holiday{Name: "Thanksgiving", Kind: autopilot.HolidayFeast}, true
	case m == time.December && d == 24:
		return Holiday{Name: "Christmas Eve", Kind: autopilot.HolidayFeast}, true
	case m == time.December && d == 25:
		return Holiday{Name: "Christmas", Kind: autopilot.HolidayFeast}, true
	case m == time.December && d == 31:
		return Holiday{Name: "New Year's Eve", Kind: autopilot.HolidayFeast}, true
	}
	return Holiday{}, false
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// easter returns Western Easter Sunday (the anonymous Gregorian algorithm).
func easter(year int) (time.Month, int) {
	a, b, c := year%19, year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Month(month), day
}
