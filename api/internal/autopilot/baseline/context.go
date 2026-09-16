package baseline

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// Context calculators (docs/autopilot.md#context-engine).
//
// Each calculator reads the typed signals it understands and gives a meal a
// small value for a day; a signal that is absent, or has a value the
// calculator doesn't know, contributes nothing. The sum is clamped to -1..1
// and weighted by Weights.Context, and the strongest part is named in the
// meal's explanation.

// daySignals are one day's parsed signals.
type daySignals struct {
	holiday, holidayKind string
	busyness             string
	freeMinutes          int
	temperature          string
	precipitation        string
}

// maxFreeMinutes bounds eveningFreeMinutes.
const maxFreeMinutes = 24 * 60

func textSignal(signals autopilot.Signals, key string, allowed ...string) string {
	v, ok := signals[key]
	if !ok || v.IsNumber {
		return ""
	}
	s := strings.TrimSpace(v.Text)
	if len(allowed) == 0 {
		return s
	}
	for _, a := range allowed {
		if strings.EqualFold(s, a) {
			return a
		}
	}
	return ""
}

func parseDaySignals(signals autopilot.Signals) daySignals {
	d := daySignals{
		holiday:       textSignal(signals, autopilot.ContextHoliday),
		holidayKind:   textSignal(signals, autopilot.ContextHolidayKind, autopilot.HolidayFeast, autopilot.HolidayCookout, autopilot.HolidayDayOff),
		busyness:      textSignal(signals, autopilot.ContextBusyness, autopilot.BusynessFree, autopilot.BusynessSome, autopilot.BusynessBusy),
		temperature:   textSignal(signals, autopilot.ContextTemperatureBand, autopilot.TemperatureCold, autopilot.TemperatureMild, autopilot.TemperatureHot),
		precipitation: textSignal(signals, autopilot.ContextPrecipitation, autopilot.PrecipitationNone, autopilot.PrecipitationRain, autopilot.PrecipitationSnow),
	}
	if v, ok := signals[autopilot.ContextEveningFreeMinutes]; ok && v.IsNumber && v.Number > 0 && v.Number <= maxFreeMinutes {
		d.freeMinutes = int(math.Round(v.Number))
	}
	if d.holidayKind == "" {
		d.holiday = ""
	}
	if len(d.holiday) > 60 {
		d.holiday = ""
	}
	return d
}

func parseWeekSignals(c *weekCtx, signals autopilot.Signals) {
	c.season = textSignal(signals, autopilot.ContextSeason, autopilot.SeasonWinter, autopilot.SeasonSpring, autopilot.SeasonSummer, autopilot.SeasonFall)
	if s := textSignal(signals, autopilot.ContextOrderDate); s != "" {
		if t, err := time.Parse(time.DateOnly, s); err == nil {
			c.orderDate = t
		}
	}
}

// Kinds of meal the calculators recognize, in the tenant's generic
// vocabulary: meal categories, tags, methods, and proteins.
var (
	comfortCategories = []string{"soup", "curry", "stew", "chili", "casserole"}
	lightCategories   = []string{"salad"}
	perishableProtein = []string{"fish", "shellfish", "seafood"}
)

func comfort(it *item) bool {
	return intersect(it.categories, comfortCategories) != "" || slices.Contains(it.methods, "slow-cooker") ||
		slices.ContainsFunc(it.tags, func(t string) bool { return strings.Contains(t, "comfort") })
}

func light(it *item) bool {
	return intersect(it.categories, lightCategories) != "" || slices.Contains(it.methods, "grill")
}

func perishable(it *item) bool {
	return intersect(it.proteins, perishableProtein) != "" || intersect(it.categories, lightCategories) != ""
}

func isSoup(it *item) bool { return slices.Contains(it.categories, "soup") }

type contextPart struct {
	code, text string
	value      float64
}

// contextFor returns the context signal (-1..1) for a meal on a slot and the
// reason naming its strongest part.
func (m *model) contextFor(s *slot, it *item) (float64, []reason) {
	if m.w.Context == 0 {
		return 0, nil
	}
	day := autopilot.Days[s.day]
	sig := m.ctx.days[s.day].sig
	var parts []contextPart
	add := func(code string, value float64, text string) {
		if value != 0 {
			parts = append(parts, contextPart{code: code, text: text, value: value})
		}
	}

	// Calendar: how much of the evening is free.
	calendar := sig.freeMinutes > 0 || sig.busyness != ""
	switch {
	case sig.freeMinutes > 0 && it.minutes <= 0:
		add("calendar", -0.2, fmt.Sprintf("Cook time unknown for the %d min you have free %s", sig.freeMinutes, day.Name()))
	case sig.freeMinutes > 0 && it.minutes <= sig.freeMinutes:
		add("calendar", 0.5, fmt.Sprintf("Fits the %d min you have free %s", sig.freeMinutes, day.Name()))
	case sig.freeMinutes > 0:
		add("calendar", -math.Min(1, 2*float64(it.minutes-sig.freeMinutes)/float64(sig.freeMinutes)),
			fmt.Sprintf("Longer than the %d min you have free %s", sig.freeMinutes, day.Name()))
	case sig.busyness == autopilot.BusynessBusy && it.minutes <= 0:
		add("calendar", -0.2, fmt.Sprintf("Cook time unknown for your busy %s evening", day.Name()))
	case sig.busyness == autopilot.BusynessBusy && it.band == autopilot.BandQuick:
		add("calendar", 0.6, fmt.Sprintf("Quick for your busy %s evening", day.Name()))
	case sig.busyness == autopilot.BusynessBusy && it.band == autopilot.BandLong:
		add("calendar", -0.8, fmt.Sprintf("A long cook for your busy %s evening", day.Name()))
	case sig.busyness == autopilot.BusynessFree && it.band == autopilot.BandLong:
		add("calendar", 0.4, fmt.Sprintf("Free %s evening, time for a longer cook", day.Name()))
	}

	// Weather: a day's forecast wins over the season.
	cold := sig.temperature == autopilot.TemperatureCold
	wet := sig.precipitation == autopilot.PrecipitationRain || sig.precipitation == autopilot.PrecipitationSnow
	weather := weatherPhrase(sig, day)
	switch {
	case (cold || wet) && comfort(it):
		v := 0.3
		switch {
		case cold && wet:
			v = 0.8
		case cold, sig.precipitation == autopilot.PrecipitationSnow:
			v = 0.6
		}
		add("weather", v, weather+", comfort food")
	case sig.temperature == autopilot.TemperatureHot && light(it):
		add("weather", 0.5, weather+", something lighter")
	case sig.temperature == autopilot.TemperatureHot && isSoup(it):
		add("weather", -0.4, "Soup on a hot "+day.Name())
	case sig.temperature == autopilot.TemperatureHot && it.band == autopilot.BandLong:
		add("weather", -0.3, "A long cook on a hot "+day.Name())
	case wet && slices.Contains(it.methods, "grill"):
		add("weather", -0.4, fmt.Sprintf("Grilling in the %s on %s", sig.precipitation, day.Name()))
	}
	if sig.temperature == "" {
		switch {
		case m.ctx.season == autopilot.SeasonWinter && comfort(it):
			add("season", 0.4, "A warming dinner for winter")
		case m.ctx.season == autopilot.SeasonFall && comfort(it):
			add("season", 0.25, "A cozy dinner for fall")
		case m.ctx.season == autopilot.SeasonSummer && slices.Contains(it.methods, "grill") && !wet:
			add("season", 0.4, "Grill season")
		case m.ctx.season == autopilot.SeasonSummer && light(it):
			add("season", 0.3, "A light dinner for summer")
		case m.ctx.season == autopilot.SeasonSummer && isSoup(it):
			add("season", -0.25, "Soup in summer")
		}
	}

	// Holidays: something special on a feast day and easy nights around it; a
	// cookout on cookout holidays; time for a longer cook on a day off.
	switch sig.holidayKind {
	case autopilot.HolidayFeast:
		if it.band == autopilot.BandLong || slices.Contains(it.methods, "smoker") {
			add("holiday", 0.5, "Something special for "+sig.holiday)
		}
	case autopilot.HolidayCookout:
		if slices.Contains(it.methods, "grill") || slices.Contains(it.methods, "smoker") {
			add("holiday", 0.6, "A cookout for "+sig.holiday)
		}
	case autopilot.HolidayDayOff:
		if it.band == autopilot.BandLong {
			add("holiday", 0.3, "Time for a longer cook on "+sig.holiday)
		}
	case "":
		if feast := m.ctx.feast; feast != "" {
			switch it.band {
			case autopilot.BandQuick:
				add("holiday", 0.3, "An easy night in "+feast+" week")
			case autopilot.BandLong:
				add("holiday", -0.3, "A long cook in "+feast+" week")
			}
		}
	}

	// Weekday: a weekend day with no rule and no calendar says has time for a
	// longer cook.
	if (s.day == 5 || s.day == 6) && !m.prefs.weeknights[s.day] && s.rule == nil && !calendar && sig.holidayKind == "" &&
		it.band == autopilot.BandLong {
		add("weekday", 0.3, day.Name()+" has time for a longer cook")
	}

	// Order date: perishable meals early in the order week.
	if !m.ctx.orderDate.IsZero() && perishable(it) {
		date := m.weekStart.AddDate(0, 0, s.day)
		since := ((int(date.Sub(m.ctx.orderDate).Hours()/24))%7 + 7) % 7
		order := m.ctx.orderDate.Weekday().String()
		switch {
		case since <= 2:
			add("orderDate", 0.4, "Fresh from "+order+"'s order")
		case since >= 5:
			add("orderDate", -0.4, "Late in the week after "+order+"'s order")
		}
	}

	if len(parts) == 0 {
		return 0, nil
	}
	total := 0.0
	for _, p := range parts {
		total += p.value
	}
	value := clamp(total)
	slices.SortStableFunc(parts, func(a, b contextPart) int {
		return cmp.Or(cmp.Compare(math.Abs(b.value), math.Abs(a.value)), cmp.Compare(a.code, b.code))
	})
	var reasons []reason
	if top := parts[0]; top.text != "" {
		reasons = append(reasons, reason{code: top.code, text: top.text, value: value * m.w.Context, forced: true})
	}
	return value, reasons
}

func weatherPhrase(sig daySignals, day autopilot.Day) string {
	cold := sig.temperature == autopilot.TemperatureCold
	switch {
	case cold && sig.precipitation == autopilot.PrecipitationRain:
		return "Cold and rainy " + day.Name()
	case cold && sig.precipitation == autopilot.PrecipitationSnow, sig.precipitation == autopilot.PrecipitationSnow:
		return "Snowy " + day.Name()
	case cold:
		return "Cold " + day.Name()
	case sig.precipitation == autopilot.PrecipitationRain:
		return "Rainy " + day.Name()
	case sig.temperature == autopilot.TemperatureHot:
		return "Hot " + day.Name()
	}
	return day.Name()
}

// contextMessages explain week-level context: holidays on the planned days
// and signals that came from the household's devices.
func (m *model) contextMessages(slots []*slot) []autopilot.Message {
	var out []autopilot.Message
	calendar, forecast := false, false
	for _, s := range slots {
		sig := m.ctx.days[s.day].sig
		calendar = calendar || sig.busyness != "" || sig.freeMinutes > 0
		forecast = forecast || sig.temperature != "" || sig.precipitation != ""
	}
	for d, dc := range m.ctx.days {
		sig := dc.sig
		if sig.holidayKind == "" || !slices.Contains(m.prefs.planDays, d) {
			continue
		}
		day := autopilot.Days[d].Name()
		text := fmt.Sprintf("%s is %s.", sig.holiday, day)
		switch sig.holidayKind {
		case autopilot.HolidayFeast:
			text = fmt.Sprintf("%s is %s: something special that day, easy nights around it.", sig.holiday, day)
		case autopilot.HolidayCookout:
			text = fmt.Sprintf("%s is %s: a cookout if the grill or smoker is free.", sig.holiday, day)
		case autopilot.HolidayDayOff:
			text = fmt.Sprintf("%s is %s: time for a longer cook.", sig.holiday, day)
		}
		out = append(out, autopilot.Message{Code: "holiday", Text: strings.ToUpper(text[:1]) + text[1:]})
	}
	switch {
	case calendar && forecast:
		out = append(out, autopilot.Message{Code: "device_context", Text: "Planned around your calendar and the forecast."})
	case calendar:
		out = append(out, autopilot.Message{Code: "device_context", Text: "Planned around your calendar."})
	case forecast:
		out = append(out, autopilot.Message{Code: "device_context", Text: "Planned around the forecast."})
	}
	return out
}
