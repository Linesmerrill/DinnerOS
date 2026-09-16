package baseline

import (
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

func category(c ...string) opt { return func(it *autopilot.Item) { it.MealCategories = c } }

func onDay(day autopilot.Day, kv ...string) autopilot.DayContext {
	s := autopilot.Signals{}
	for i := 0; i+1 < len(kv); i += 2 {
		s[kv[i]] = autopilot.Text(kv[i+1])
	}
	return autopilot.DayContext{Day: day, Signals: s}
}

// contextCatalog has one meal of each kind the calculators know.
func contextCatalog() autopilot.Input {
	return input(
		meal("soup", category("soup"), minutes(30)),
		meal("salad", category("salad"), minutes(15)),
		meal("grill", methods("grill"), minutes(25)),
		meal("roast", minutes(120)),
		meal("quick", minutes(15)),
		meal("salmon", proteins("fish"), minutes(25)),
		meal("plain", minutes(30)),
	)
}

func TestContextAbsentChangesNothing(t *testing.T) {
	p := New(Options{})
	in := contextCatalog()
	res := generate(t, p, in)
	for _, s := range res.Slots {
		if _, ok := s.Signals[SignalContext]; ok {
			t.Errorf("%s has a context signal without context: %v", s.ItemID, s.Signals)
		}
	}
	for _, m := range res.Messages {
		if m.Code == "holiday" || m.Code == "device_context" {
			t.Errorf("message without context: %+v", m)
		}
	}
	// Unknown keys and values are ignored.
	in.Context.Signals = autopilot.Signals{"moonPhase": autopilot.Text("full"), autopilot.ContextSeason: autopilot.Text("monsoon"), autopilot.ContextOrderDate: autopilot.Text("soon")}
	in.Context.Days = []autopilot.DayContext{
		onDay(autopilot.Tuesday, autopilot.ContextBusyness, "slammed", autopilot.ContextTemperatureBand, "balmy"),
		{Day: autopilot.Wednesday, Signals: autopilot.Signals{autopilot.ContextEveningFreeMinutes: autopilot.Text("thirty"), autopilot.ContextBusyness: autopilot.Number(3)}},
		onDay(autopilot.Thursday, autopilot.ContextHoliday, "Mystery Day"),
	}
	if got := generate(t, p, in); describe(got) != describe(res) {
		t.Errorf("unknown signals changed the week:\n got %s\nwant %s", describe(got), describe(res))
	}
}

func contextReason(t *testing.T, p *Provider, in autopilot.Input, day autopilot.Day, id string) autopilot.Recommendation {
	t.Helper()
	return recommendationFor(t, rank(t, p, in, day), id)
}

func TestSeasonContext(t *testing.T) {
	p := New(Options{})
	for _, tc := range []struct {
		season, id, text string
	}{
		{"winter", "soup", "A warming dinner for winter"},
		{"fall", "soup", "A cozy dinner for fall"},
		{"summer", "grill", "Grill season"},
		{"summer", "salad", "A light dinner for summer"},
		{"summer", "soup", "Soup in summer"},
	} {
		in := contextCatalog()
		in.Context.Signals = autopilot.Signals{autopilot.ContextSeason: autopilot.Text(tc.season)}
		if r := contextReason(t, p, in, autopilot.Tuesday, tc.id); !hasReason(r.Reasons, tc.text) || r.Signals[SignalContext] == 0 {
			t.Errorf("%s %s: %v %v", tc.season, tc.id, reasonTexts(r.Reasons), r.Signals)
		}
	}
	in := contextCatalog()
	in.Context.Signals = autopilot.Signals{autopilot.ContextSeason: autopilot.Text("Winter")}
	res := rank(t, p, in, autopilot.Tuesday)
	if position(res, "soup") > position(res, "plain") {
		t.Errorf("winter should favor soup over an equal meal: %v", pickedRank(res))
	}
	if r := recommendationFor(t, res, "plain"); r.Signals[SignalContext] != 0 {
		t.Errorf("season moved an unrelated meal: %v", r.Signals)
	}
}

func TestWeatherContext(t *testing.T) {
	p := New(Options{})
	in := contextCatalog()
	in.Context.Signals = autopilot.Signals{autopilot.ContextSeason: autopilot.Text("summer")}
	in.Context.Days = []autopilot.DayContext{
		onDay(autopilot.Tuesday, autopilot.ContextTemperatureBand, "cold", autopilot.ContextPrecipitation, "rain"),
		onDay(autopilot.Wednesday, autopilot.ContextTemperatureBand, "hot"),
		onDay(autopilot.Thursday, autopilot.ContextPrecipitation, "rain"),
	}
	cases := []struct {
		day      autopilot.Day
		id, text string
		positive bool
	}{
		// A day's forecast wins over the season.
		{autopilot.Tuesday, "soup", "Cold and rainy Tuesday, comfort food", true},
		{autopilot.Wednesday, "salad", "Hot Wednesday, something lighter", true},
		{autopilot.Wednesday, "soup", "Soup on a hot Wednesday", false},
		{autopilot.Wednesday, "roast", "A long cook on a hot Wednesday", false},
		{autopilot.Thursday, "grill", "Grilling in the rain on Thursday", false},
		{autopilot.Thursday, "soup", "Rainy Thursday, comfort food", true},
	}
	for _, tc := range cases {
		r := contextReason(t, p, in, tc.day, tc.id)
		if !hasReason(r.Reasons, tc.text) || (r.Signals[SignalContext] > 0) != tc.positive {
			t.Errorf("%s %s: %v %v", tc.day, tc.id, reasonTexts(r.Reasons), r.Signals)
		}
	}
	res := rank(t, p, in, autopilot.Tuesday)
	if position(res, "soup") != 0 {
		t.Errorf("cold rainy day ranking = %v", pickedRank(res))
	}
	week := generate(t, p, in)
	if !slices.ContainsFunc(week.Messages, func(m autopilot.Message) bool { return m.Text == "Planned around the forecast." }) {
		t.Errorf("messages = %+v", week.Messages)
	}
}

func TestCalendarContext(t *testing.T) {
	p := New(Options{})
	in := contextCatalog()
	in.Context.Days = []autopilot.DayContext{
		onDay(autopilot.Tuesday, autopilot.ContextBusyness, "busy"),
		onDay(autopilot.Wednesday, autopilot.ContextBusyness, "free"),
		{Day: autopilot.Thursday, Signals: autopilot.Signals{autopilot.ContextEveningFreeMinutes: autopilot.Number(20), autopilot.ContextBusyness: autopilot.Text("some")}},
	}
	for _, tc := range []struct {
		day      autopilot.Day
		id, text string
		positive bool
	}{
		{autopilot.Tuesday, "quick", "Quick for your busy Tuesday evening", true},
		{autopilot.Tuesday, "roast", "A long cook for your busy Tuesday evening", false},
		{autopilot.Wednesday, "roast", "Free Wednesday evening, time for a longer cook", true},
		{autopilot.Thursday, "quick", "Fits the 20 min you have free Thursday", true},
		{autopilot.Thursday, "plain", "Longer than the 20 min you have free Thursday", false},
	} {
		r := contextReason(t, p, in, tc.day, tc.id)
		if !hasReason(r.Reasons, tc.text) || (r.Signals[SignalContext] > 0) != tc.positive {
			t.Errorf("%s %s: %v %v", tc.day, tc.id, reasonTexts(r.Reasons), r.Signals)
		}
	}
	if res := rank(t, p, in, autopilot.Tuesday); position(res, "roast") != len(res.Items)-1 {
		t.Errorf("busy evening ranking = %v", pickedRank(res))
	}
	week := generate(t, p, in)
	if slot := slotOn(week, autopilot.Tuesday); slot == nil || slot.TimeBand == autopilot.BandLong {
		t.Errorf("busy Tuesday got a long cook: %s", describe(week))
	}
	if !slices.ContainsFunc(week.Messages, func(m autopilot.Message) bool { return m.Text == "Planned around your calendar." }) {
		t.Errorf("messages = %+v", week.Messages)
	}
}

func TestHolidayContext(t *testing.T) {
	p := New(Options{})
	in := contextCatalog()
	in.Preferences.PlanDays = []autopilot.Day{autopilot.Monday, autopilot.Tuesday, autopilot.Wednesday, autopilot.Thursday, autopilot.Friday}
	in.Context.Days = []autopilot.DayContext{
		onDay(autopilot.Thursday, autopilot.ContextHoliday, "Thanksgiving", autopilot.ContextHolidayKind, "feast"),
	}
	if r := contextReason(t, p, in, autopilot.Thursday, "roast"); !hasReason(r.Reasons, "Something special for Thanksgiving") {
		t.Errorf("feast day: %v", reasonTexts(r.Reasons))
	}
	if r := contextReason(t, p, in, autopilot.Tuesday, "quick"); !hasReason(r.Reasons, "An easy night in Thanksgiving week") {
		t.Errorf("feast week: %v", reasonTexts(r.Reasons))
	}
	week := generate(t, p, in)
	if !slices.ContainsFunc(week.Messages, func(m autopilot.Message) bool {
		return m.Code == "holiday" && m.Text == "Thanksgiving is Thursday: something special that day, easy nights around it."
	}) {
		t.Errorf("messages = %+v", week.Messages)
	}

	in.Context.Days = []autopilot.DayContext{onDay(autopilot.Monday, autopilot.ContextHoliday, "Labor Day", autopilot.ContextHolidayKind, "cookout")}
	if r := contextReason(t, p, in, autopilot.Monday, "grill"); !hasReason(r.Reasons, "A cookout for Labor Day") {
		t.Errorf("cookout: %v", reasonTexts(r.Reasons))
	}
	if r := contextReason(t, p, in, autopilot.Tuesday, "quick"); r.Signals[SignalContext] != 0 {
		t.Errorf("a cookout holiday changed the rest of the week: %v", r.Signals)
	}
	in.Context.Days = []autopilot.DayContext{onDay(autopilot.Monday, autopilot.ContextHoliday, "Presidents' Day", autopilot.ContextHolidayKind, "dayOff")}
	if r := contextReason(t, p, in, autopilot.Monday, "roast"); !hasReason(r.Reasons, "Time for a longer cook on Presidents' Day") {
		t.Errorf("day off: %v", reasonTexts(r.Reasons))
	}
}

func TestWeekdayContext(t *testing.T) {
	p := New(Options{})
	in := contextCatalog()
	in.Preferences.PlanDays = autopilot.Days
	in.Preferences.Weeknights = []autopilot.Day{autopilot.Monday, autopilot.Tuesday, autopilot.Wednesday, autopilot.Thursday}
	if r := contextReason(t, p, in, autopilot.Sunday, "roast"); !hasReason(r.Reasons, "Sunday has time for a longer cook") {
		t.Errorf("sunday: %v", reasonTexts(r.Reasons))
	}
	if r := contextReason(t, p, in, autopilot.Wednesday, "roast"); r.Signals[SignalContext] != 0 {
		t.Errorf("weeknight got a weekend boost: %v", r.Signals)
	}
	// A weekday rule speaks for the day.
	in.Preferences.Rules = []autopilot.WeekdayRule{{Day: autopilot.Sunday, Label: "Sunday roast", TimeBand: autopilot.BandLong}}
	if r := contextReason(t, p, in, autopilot.Sunday, "roast"); r.Signals[SignalContext] != 0 {
		t.Errorf("rule day got a weekend boost too: %v", r.Signals)
	}
}

func TestOrderDateContext(t *testing.T) {
	p := New(Options{})
	in := contextCatalog()
	// 2026-W38 starts Monday 2026-09-14; Saturday 2026-09-12 repeats weekly.
	in.Context.Signals = autopilot.Signals{autopilot.ContextOrderDate: autopilot.Text("2026-09-12")}
	if r := contextReason(t, p, in, autopilot.Monday, "salmon"); !hasReason(r.Reasons, "Fresh from Saturday's order") || r.Signals[SignalContext] <= 0 {
		t.Errorf("monday: %v %v", reasonTexts(r.Reasons), r.Signals)
	}
	if r := contextReason(t, p, in, autopilot.Friday, "salmon"); !hasReason(r.Reasons, "Late in the week after Saturday's order") || r.Signals[SignalContext] >= 0 {
		t.Errorf("friday: %v %v", reasonTexts(r.Reasons), r.Signals)
	}
	if r := contextReason(t, p, in, autopilot.Monday, "plain"); r.Signals[SignalContext] != 0 {
		t.Errorf("order date moved a shelf-stable meal: %v", r.Signals)
	}
}

func TestContextIsSoftAndBounded(t *testing.T) {
	p := New(Options{})
	in := contextCatalog()
	in.Context.MaxMinutes = 60
	in.Context.Signals = autopilot.Signals{autopilot.ContextSeason: autopilot.Text("winter")}
	in.Context.Days = []autopilot.DayContext{
		onDay(autopilot.Tuesday, autopilot.ContextBusyness, "free", autopilot.ContextTemperatureBand, "cold", autopilot.ContextHoliday, "Feast", autopilot.ContextHolidayKind, "feast"),
	}
	res := rank(t, p, in, autopilot.Tuesday)
	if position(res, "roast") >= 0 {
		t.Errorf("context reintroduced a meal over the hard cap: %v", pickedRank(res))
	}
	for _, r := range res.Items {
		if v := r.Signals[SignalContext]; v < -1 || v > 1 {
			t.Errorf("%s context = %v", r.ItemID, v)
		}
	}
}
