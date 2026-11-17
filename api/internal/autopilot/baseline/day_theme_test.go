package baseline

import (
	"fmt"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// "Tacos on Tuesday": a weekday rule naming a dish fills the day with that
// dish whatever its cuisine, Korean or Mexican, and leans hard away from a
// meal that isn't one, even a Mexican one (decision 637).
func TestWeekdayRuleDishes(t *testing.T) {
	p := New(Options{})
	in := input(
		meal("korean-tacos", cuisine("korean"), dishes("taco")),
		meal("enchiladas", cuisine("mexican"), dishes("enchilada")),
		meal("burger", cuisine("american"), dishes("burger")),
		meal("stew", cuisine("irish"), dishes("soup")),
		meal("curry", cuisine("indian"), dishes("curry")),
		meal("ramen", cuisine("japanese"), dishes("noodle")),
	)
	in.Ratings = []autopilot.Rating{rate("enchiladas", 5), rate("burger", 5), rate("stew", 5), rate("curry", 5), rate("ramen", 5), rate("korean-tacos", 3)}
	in.Preferences.Rules = []autopilot.WeekdayRule{{Day: autopilot.Tuesday, Label: "Taco Tuesday", Dishes: []string{"taco"}}}
	res := generate(t, p, in)
	tue := slotOn(res, autopilot.Tuesday)
	if tue == nil || tue.ItemID != "korean-tacos" {
		t.Fatalf("tuesday = %+v: %s", tue, describe(res))
	}
	if !hasReason(tue.Reasons, "Taco Tuesday · Tacos") || tue.Signals[SignalRule] != 1 {
		t.Errorf("reasons = %v, rule %v", reasonTexts(tue.Reasons), tue.Signals[SignalRule])
	}
	for _, rec := range rank(t, p, in, autopilot.Tuesday).Items {
		if rec.ItemID == "enchiladas" && rec.Signals[SignalRule] != -1 {
			t.Errorf("enchiladas on taco night: rule %v, want -1", rec.Signals[SignalRule])
		}
	}
}

func plannedOn(day autopilot.Day, ids ...string) []autopilot.Interaction {
	var out []autopilot.Interaction
	for i, id := range ids {
		out = append(out, autopilot.Interaction{Kind: autopilot.KindPlanned, ItemID: id, Day: day, Week: fmt.Sprintf("2026-W%02d", 30+i)})
	}
	return out
}

// Without a rule, Autopilot learns a day's habit from what was planned on
// it, as a dish or as a cuisine, whichever explains the day better: tacos
// most Tuesdays (Korean and Mexican alike), Italian most Mondays (lasagna,
// risotto, pasta) (decision 637).
func TestDayThemesAreLearnedFromHistory(t *testing.T) {
	p := New(Options{})
	in := input(
		meal("korean-tacos", cuisine("korean"), dishes("taco")),
		meal("fish-tacos", cuisine("mexican"), dishes("taco")),
		meal("carnitas-tacos", cuisine("mexican"), dishes("taco")),
		meal("new-tacos", cuisine("thai"), dishes("taco")),
		meal("burger", cuisine("american"), dishes("burger")),
		meal("lasagna", cuisine("italian"), dishes("pasta")),
		meal("risotto", cuisine("italian"), dishes("risotto")),
		meal("chicken-parm", cuisine("italian")),
		meal("new-italian", cuisine("italian")),
		meal("curry", cuisine("indian"), dishes("curry")),
	)
	in.Week = "2026-W40"
	in.History = append(plannedOn(autopilot.Tuesday, "korean-tacos", "fish-tacos", "carnitas-tacos", "burger"),
		plannedOn(autopilot.Monday, "lasagna", "risotto", "chicken-parm", "curry")...)

	tue := rank(t, p, in, autopilot.Tuesday)
	for _, rec := range tue.Items {
		if rec.ItemID == "new-tacos" {
			if rec.Signals[SignalDayTheme] != 0.75 || !hasReason(rec.Reasons, "Usually tacos on Tuesdays") {
				t.Errorf("new tacos on Tuesday: theme %v, reasons %v", rec.Signals[SignalDayTheme], reasonTexts(rec.Reasons))
			}
		}
		if rec.ItemID == "burger" && rec.Signals[SignalDayTheme] != 0 {
			t.Errorf("burger on taco Tuesday: theme %v", rec.Signals[SignalDayTheme])
		}
	}
	mon := rank(t, p, in, autopilot.Monday)
	for _, rec := range mon.Items {
		if rec.ItemID == "new-italian" && !hasReason(rec.Reasons, "Usually Italian on Mondays") {
			t.Errorf("new Italian on Monday: reasons %v", reasonTexts(rec.Reasons))
		}
	}

	// A weekday rule says it outright: no learned theme that day.
	in.Preferences.Rules = []autopilot.WeekdayRule{{Day: autopilot.Tuesday, Label: "Burger night", Dishes: []string{"burger"}}}
	for _, rec := range rank(t, p, in, autopilot.Tuesday).Items {
		if rec.Signals[SignalDayTheme] != 0 {
			t.Errorf("%s: theme %v with a rule on the day", rec.ItemID, rec.Signals[SignalDayTheme])
		}
	}
}

// Too few meals on a day, or no clear lean, is no theme.
func TestDayThemeNeedsAClearHabit(t *testing.T) {
	c := newThemeCounts()
	taco := &item{dishTypes: []string{"taco"}, cuisines: []string{"mexican"}}
	other := func(cu string) *item { return &item{cuisines: []string{cu}} }
	c.add(1, taco)
	c.add(1, taco)
	c.add(1, other("thai"))
	if th := c.themes(); th[1] != nil {
		t.Errorf("three meals: theme %+v, want none", th[1])
	}
	c.add(1, other("greek"))
	c.add(1, other("indian"))
	th := c.themes()
	if th[1] == nil || th[1].dish != "" || th[1].cuisine != "" {
		t.Fatalf("2 of 5 tacos: theme %+v, want no clear theme", th[1])
	}
	// A loose lean still counts a little, without saying so.
	if v, text := th[1].value(taco, autopilot.Tuesday); v != 0.4 || text != "" {
		t.Errorf("2 of 5 Mexican: value %v %q, want 0.4 and no text", v, text)
	}
	if v, _ := th[1].value(other("thai"), autopilot.Tuesday); v != 0 {
		t.Errorf("1 of 5 Thai: value %v, want 0", v)
	}
	if got := dishPlural("curry") + "," + dishPlural("sandwich") + "," + dishPlural("stir-fry") + "," + dishPlural("pasta"); got != "curries,sandwiches,stir-fries,pasta" {
		t.Errorf("plurals = %q", got)
	}
}

func cookedOn(day autopilot.Day, ids ...string) []autopilot.Interaction {
	var out []autopilot.Interaction
	for i, id := range ids {
		out = append(out, autopilot.Interaction{Kind: autopilot.KindCooked, ItemID: id, Day: day, Week: fmt.Sprintf("2026-W%02d", 30+i)})
	}
	return out
}

// Mexican on Tuesdays, learned from what was cooked with no plan at all,
// lifts a Mexican meal on Tuesday and says why; on Wednesday it doesn't
// (decision 724).
func TestDayThemeLearnsCuisineFromCookedMeals(t *testing.T) {
	p := New(Options{})
	in := input(
		meal("enchiladas", cuisine("mexican"), dishes("enchilada")),
		meal("carnitas", cuisine("mexican"), dishes("taco")),
		meal("pozole", cuisine("mexican"), dishes("soup")),
		meal("mole", cuisine("mexican")),
		meal("burger", cuisine("american"), dishes("burger")),
		meal("new-mexican", cuisine("mexican")),
		meal("new-thai", cuisine("thai")),
	)
	in.Week = "2026-W40"
	in.History = cookedOn(autopilot.Tuesday, "enchiladas", "carnitas", "pozole", "burger", "mole")

	signals := func(day autopilot.Day) map[string]autopilot.Recommendation {
		out := map[string]autopilot.Recommendation{}
		for _, rec := range rank(t, p, in, day).Items {
			out[rec.ItemID] = rec
		}
		return out
	}
	tue := signals(autopilot.Tuesday)
	if got := tue["new-mexican"]; got.Signals[SignalDayTheme] != 0.8 || !hasReason(got.Reasons, "Usually Mexican on Tuesdays") {
		t.Errorf("new Mexican on Tuesday: theme %v, reasons %v", got.Signals[SignalDayTheme], reasonTexts(got.Reasons))
	}
	if got := tue["new-thai"]; got.Signals[SignalDayTheme] != 0 {
		t.Errorf("new Thai on Tuesday: theme %v, want 0", got.Signals[SignalDayTheme])
	}
	if tue["new-mexican"].Score <= tue["new-thai"].Score {
		t.Errorf("Tuesday: Mexican %v not above Thai %v", tue["new-mexican"].Score, tue["new-thai"].Score)
	}
	if got := signals(autopilot.Wednesday)["new-mexican"]; got.Signals[SignalDayTheme] != 0 {
		t.Errorf("new Mexican on Wednesday: theme %v, want 0", got.Signals[SignalDayTheme])
	}

	// A week ending unanswered is no night anyone cooked.
	for i := range in.History {
		in.History[i].Implicit = true
	}
	if got := signals(autopilot.Tuesday)["new-mexican"]; got.Signals[SignalDayTheme] != 0 {
		t.Errorf("implicit cooks: theme %v, want 0", got.Signals[SignalDayTheme])
	}
}

// When tacos are the day's theme, a Mexican meal that isn't a taco still
// gets the day's Mexican share, and a Mexican taco gets the larger of the
// two (decision 724).
func TestDayThemeGradesCuisineBesideADish(t *testing.T) {
	c := newThemeCounts()
	mexTaco := &item{id: "a", dishTypes: []string{"taco"}, cuisines: []string{"mexican"}}
	korTaco := &item{id: "b", dishTypes: []string{"taco"}, cuisines: []string{"korean"}}
	enchilada := &item{id: "c", dishTypes: []string{"enchilada"}, cuisines: []string{"mexican"}}
	burger := &item{id: "d", cuisines: []string{"american"}}
	for _, it := range []*item{mexTaco, korTaco, korTaco, enchilada, burger} {
		c.add(1, it)
	}
	th := c.themes()[1]
	if th == nil || th.dish != "taco" || th.share != 0.6 {
		t.Fatalf("theme = %+v, want tacos at 0.6", th)
	}
	if v, text := th.value(enchilada, autopilot.Tuesday); v != 0.4 || text != "" {
		t.Errorf("enchilada: %v %q, want 0.4 and no text", v, text)
	}
	if v, text := th.value(mexTaco, autopilot.Tuesday); v != 0.6 || text != "Usually tacos on Tuesdays" {
		t.Errorf("Mexican taco: %v %q", v, text)
	}
	if v, _ := th.value(burger, autopilot.Tuesday); v != 0 {
		t.Errorf("burger: %v, want 0", v)
	}
}

// A meal planned and then cooked on the same day of the same week counts
// once.
func TestDayThemeCountsAPlannedAndCookedMealOnce(t *testing.T) {
	c := newThemeCounts()
	it := &item{id: "tacos", cuisines: []string{"mexican"}}
	c.addOnce("2026-W30", 1, it)
	c.addOnce("2026-W30", 1, it)
	c.addOnce("2026-W31", 1, it)
	c.addOnce("2026-W31", 2, it)
	if c.meals[1] != 2 || c.meals[2] != 1 {
		t.Errorf("meals = %v, want 2 on Tuesday and 1 on Wednesday", c.meals)
	}
}
