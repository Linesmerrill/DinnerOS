package baseline

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// Fixtures are made-up meals; nothing here comes from a real catalog.

func categories(c ...string) opt { return func(it *autopilot.Item) { it.MealCategories = c } }
func regionsOf(r ...string) opt  { return func(it *autopilot.Item) { it.CuisineRegions = r } }

// TestMealCategoryVariety: two meals of the same kind repeat even when their
// cuisine labels differ or are missing, which is the case the cuisine penalty
// alone misses.
func TestMealCategoryVariety(t *testing.T) {
	in := input(
		meal("pasta-italian", cuisine("italian"), regionsOf("southern european", "european"), categories("pasta"), proteins("chicken")),
		meal("pasta-unlabeled", cuisine(), categories("pasta"), proteins("beef")),
		meal("tacos", cuisine("mexican"), regionsOf("latin american"), categories("tacos"), proteins("pork")),
	)
	in.Fixed = []autopilot.Assignment{{ItemID: "pasta-italian", Day: autopilot.Monday}}
	r := rank(t, New(Options{}), in, autopilot.Tuesday)

	want := -DefaultWeights().MealCategoryRepeat
	for _, rec := range r.Items {
		if rec.ItemID == "pasta-unlabeled" && rec.Signals[SignalVariety] != want {
			t.Errorf("pasta-unlabeled variety = %v, want %v; a second pasta repeats whatever its cuisine says",
				rec.Signals[SignalVariety], want)
		}
		if rec.ItemID == "tacos" && rec.Signals[SignalVariety] != 0 {
			t.Errorf("tacos variety = %v, want 0", rec.Signals[SignalVariety])
		}
	}
	if r.Items[0].ItemID != "tacos" {
		t.Errorf("ranking after a pasta Monday = %+v; want a different kind of meal first", r.Items)
	}
}

// TestCuisineRegionVariety: sharing only a region is a partial repeat, worth
// less than a shared cuisine, and the two never stack.
func TestCuisineRegionVariety(t *testing.T) {
	w := DefaultWeights()
	in := input(
		meal("italian-1", cuisine("italian"), regionsOf("southern european", "european")),
		meal("italian-2", cuisine("italian"), regionsOf("southern european", "european")),
		meal("greek", cuisine("greek"), regionsOf("southern european", "european")),
		meal("thai", cuisine("thai"), regionsOf("southeast asian", "asian")),
	)
	in.Fixed = []autopilot.Assignment{{ItemID: "italian-1", Day: autopilot.Monday}}
	r := rank(t, New(Options{}), in, autopilot.Tuesday)

	want := map[string]float64{"italian-2": -w.CuisineRepeat, "greek": -w.CuisineRegionRepeat, "thai": 0}
	for _, rec := range r.Items {
		if w, ok := want[rec.ItemID]; ok && rec.Signals[SignalVariety] != w {
			t.Errorf("%s variety = %v, want %v", rec.ItemID, rec.Signals[SignalVariety], w)
		}
	}
	if w.CuisineRegionRepeat >= w.CuisineRepeat {
		t.Errorf("region repeat %v should be lighter than a cuisine repeat %v", w.CuisineRegionRepeat, w.CuisineRepeat)
	}
}

// TestWeekdayRuleMatchesRegion: a rule for a country cuisine matches a recipe
// the catalog labeled only with that cuisine's region, but not a different
// cuisine that merely shares a broader region.
func TestWeekdayRuleMatchesRegion(t *testing.T) {
	italianRule := autopilot.WeekdayRule{
		Day: autopilot.Monday, Label: "Italian Monday",
		Cuisines: []string{"italian"}, CuisineRegions: []string{"southern european", "european"},
		Frequency: autopilot.EveryWeek,
	}
	in := input(
		meal("region-pasta", cuisine("southern european"), regionsOf("european")),
		meal("french", cuisine("french"), regionsOf("western european", "european")),
	)
	in.Preferences.MealsPerWeek = 1
	in.Preferences.Rules = []autopilot.WeekdayRule{italianRule}
	res := generate(t, New(Options{}), in)

	got := slotOn(res, autopilot.Monday)
	if got == nil || got.ItemID != "region-pasta" {
		t.Fatalf("Monday = %v; an Italian rule should match a recipe labeled southern european: %s", got, describe(res))
	}
	if got.Signals[SignalRule] != 1 {
		t.Errorf("rule signal = %v, want a full match", got.Signals[SignalRule])
	}
	// The French meal shares only the European region, so it isn't Italian.
	in.Preferences.MealsPerWeek = 2
	res = generate(t, New(Options{}), in)
	for _, s := range res.Slots {
		if s.ItemID == "french" && s.Signals[SignalRule] == 1 {
			t.Errorf("french fully matched an Italian rule: %s", describe(res))
		}
	}
}

// TestAtMostOnceRuleBites is the owner's report: a Monday "Italian, at most
// once a week" rule put pasta on Monday and another pasta later the same week.
// Every Italian meal here is strong (top-rated, familiar, not had recently)
// and two carry only the region label, as the real catalog does.
func TestAtMostOnceRuleBites(t *testing.T) {
	var catalog []autopilot.Item
	var ratings []autopilot.Rating
	var history []autopilot.Interaction
	labels := []opt{
		cuisine("italian"), cuisine("italian"),
		// Labeled only with the region, so before region-aware rules these
		// escaped the rule, the repeat penalty, and the cuisine penalty.
		cuisine("southern european"), cuisine("southern european"),
	}
	for i, label := range labels {
		id := fmt.Sprintf("italian-%d", i)
		catalog = append(catalog, meal(id, label, regionsOf("southern european", "european"), categories("pasta")))
		ratings = append(ratings, rate(id, 5, autopilot.FeedbackMakeAgain))
		// Familiar, and last had long enough ago to earn the recency boost.
		for _, week := range []string{"2026-W20", "2026-W22", "2026-W24"} {
			history = append(history, autopilot.Interaction{ItemID: id, Kind: autopilot.KindCooked, Week: week})
		}
	}
	for i := range 4 {
		id := fmt.Sprintf("other-%d", i)
		catalog = append(catalog, meal(id, cuisine(fmt.Sprintf("cuisine-%d", i)), proteins(fmt.Sprintf("protein-%d", i))))
		ratings = append(ratings, rate(id, 3))
	}
	in := input(catalog...)
	in.Ratings, in.History = ratings, history
	in.Preferences.Rules = []autopilot.WeekdayRule{{
		Day: autopilot.Monday, Label: "Italian Monday",
		Cuisines: []string{"italian"}, CuisineRegions: []string{"southern european", "european"},
		Frequency: autopilot.AtMostOnce,
	}}

	res := generate(t, New(Options{}), in)
	if res.Planned != 5 {
		t.Fatalf("planned %d of 5: %s", res.Planned, describe(res))
	}
	n := 0
	for _, id := range pickedIDs(res) {
		if strings.HasPrefix(id, "italian") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("planned %d Italian meals; an at-most-once rule allows one: %s", n, describe(res))
	}
	if got := slotOn(res, autopilot.Monday); got == nil || !strings.HasPrefix(got.ItemID, "italian") {
		t.Errorf("Monday = %v; the rule's day should get the Italian meal: %s", got, describe(res))
	}
	if res.Score.Rules >= 0 != (n == 1) {
		t.Errorf("rules score = %v with %d Italian meals", res.Score.Rules, n)
	}
}

// TestVarietyIsDeterministic: the same inputs give the same week, whatever
// order the catalog arrives in.
func TestVarietyIsDeterministic(t *testing.T) {
	build := func(reverse bool) autopilot.Input {
		var catalog []autopilot.Item
		for i := range 6 {
			catalog = append(catalog, meal(fmt.Sprintf("meal-%d", i),
				cuisine(fmt.Sprintf("cuisine-%d", i%3)), categories(fmt.Sprintf("category-%d", i%2)),
				regionsOf("region-a")))
		}
		if reverse {
			slices.Reverse(catalog)
		}
		in := input(catalog...)
		in.Preferences.Rules = []autopilot.WeekdayRule{{
			Day: autopilot.Monday, Label: "Italian Monday", Cuisines: []string{"cuisine-0"},
			CuisineRegions: []string{"region-a"}, Frequency: autopilot.AtMostOnce,
		}}
		return in
	}
	p := New(Options{})
	first := generate(t, p, build(false))
	for range 3 {
		if got := pickedIDs(generate(t, p, build(false))); !slices.Equal(got, pickedIDs(first)) {
			t.Errorf("same inputs gave %v then %v", pickedIDs(first), got)
		}
	}
	if got := pickedIDs(generate(t, p, build(true))); !slices.Equal(got, pickedIDs(first)) {
		t.Errorf("catalog order changed the week: %v vs %v", pickedIDs(first), got)
	}
	if first.Score.Variety != generate(t, p, build(true)).Score.Variety {
		t.Error("variety score depends on catalog order")
	}
}
