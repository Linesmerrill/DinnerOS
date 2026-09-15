package baseline

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

func TestDiversityPenalties(t *testing.T) {
	var catalog []autopilot.Item
	var ratings []autopilot.Rating
	for i := range 4 {
		id := fmt.Sprintf("pasta-%d", i)
		catalog = append(catalog, meal(id, cuisine("italian"), proteins(fmt.Sprintf("protein-%d", i))))
		ratings = append(ratings, rate(id, 5))
	}
	for i := range 4 {
		id := fmt.Sprintf("other-%d", i)
		catalog = append(catalog, meal(id, proteins(fmt.Sprintf("other-protein-%d", i))))
		ratings = append(ratings, rate(id, 4))
	}
	in := input(catalog...)
	in.Ratings = ratings

	countItalian := func(res autopilot.WeekResult) int {
		n := 0
		for _, id := range pickedIDs(res) {
			if strings.HasPrefix(id, "pasta") {
				n++
			}
		}
		return n
	}
	res := generate(t, New(Options{}), in)
	if n := countItalian(res); n > 2 || res.Planned != 5 {
		t.Errorf("planned %d Italian meals; variety should cap repeats: %s", n, describe(res))
	}
	if res.Score.Variety >= 0 && countItalian(res) > 1 {
		t.Errorf("variety penalty = %v for repeated cuisines", res.Score.Variety)
	}

	noVariety := DefaultWeights()
	noVariety.CuisineRepeat, noVariety.ProteinRepeat = 0, 0
	if n := countItalian(generate(t, New(Options{Weights: noVariety}), in)); n != 4 {
		t.Errorf("without variety penalties, %d Italian meals; want all 4 top-rated", n)
	}

	// A repeated protein is penalized too, and the signal says so.
	in = input(meal("chicken-1", proteins("chicken")), meal("chicken-2", proteins("chicken")), meal("tofu", proteins("tofu")))
	in.Fixed = []autopilot.Assignment{{ItemID: "chicken-1", Day: autopilot.Monday}}
	r := rank(t, New(Options{}), in, autopilot.Tuesday)
	for _, rec := range r.Items {
		if rec.ItemID == "chicken-2" && rec.Signals[SignalVariety] != -0.15 {
			t.Errorf("chicken-2 variety = %v, want -0.15", rec.Signals[SignalVariety])
		}
	}
	if r.Items[0].ItemID != "tofu" {
		t.Errorf("ranking after a chicken Monday = %+v", r.Items)
	}
}

func TestWeekdayRules(t *testing.T) {
	p := New(Options{})

	t.Run("taco tuesday", func(t *testing.T) {
		in := input(meal("tacos", cuisine("Mexican")), meal("burger", cuisine("american")), meal("stew", cuisine("irish")),
			meal("curry", cuisine("indian")), meal("salad", cuisine("greek")), meal("ramen", cuisine("japanese")))
		in.Ratings = []autopilot.Rating{rate("burger", 5), rate("stew", 5), rate("curry", 5), rate("salad", 5), rate("ramen", 5), rate("tacos", 3)}
		in.Preferences.Rules = []autopilot.WeekdayRule{{Day: autopilot.Tuesday, Label: "Taco Tuesday", Cuisines: []string{"mexican"}}}
		res := generate(t, p, in)
		tue := slotOn(res, autopilot.Tuesday)
		if tue == nil || tue.ItemID != "tacos" {
			t.Fatalf("tuesday = %+v: %s", tue, describe(res))
		}
		if tue.Reasons[0].Text != "Taco Tuesday · Mexican" || tue.Signals[SignalRule] != 1 {
			t.Errorf("tuesday reasons = %v, rule signal %v", reasonTexts(tue.Reasons), tue.Signals[SignalRule])
		}
		if r := rank(t, p, in, autopilot.Tuesday); r.Items[len(r.Items)-1].Signals[SignalRule] != -0.3 {
			t.Errorf("an every-week rule should penalize meals that don't match: %+v", r.Items[len(r.Items)-1].Signals)
		}
	})

	t.Run("sunday smoker night, at most once", func(t *testing.T) {
		catalog := []autopilot.Item{
			meal("smoked-pork", proteins("pork"), methods("smoker"), minutes(240)),
			meal("smoked-chicken", proteins("chicken"), methods("smoker"), minutes(180)),
			meal("grilled-pork", proteins("pork"), methods("grill"), minutes(25)),
		}
		for i := range 8 {
			catalog = append(catalog, meal(fmt.Sprintf("weeknight-%d", i), proteins(fmt.Sprintf("protein-%d", i)), minutes(25)))
		}
		in := input(catalog...)
		in.Preferences.PlanDays = autopilot.Days
		in.Preferences.MealsPerWeek = 7
		in.Preferences.Equipment = []string{"smoker"}
		in.Preferences.CookTime = autopilot.CookTimeMix{MaxLongPerWeek: 0, AvoidConsecutiveLong: true}
		in.Preferences.Rules = []autopilot.WeekdayRule{{
			Day: autopilot.Sunday, Label: "Sunday smoker night", Proteins: []string{"chicken", "pork"}, Methods: []string{"smoker"},
			TimeBand: autopilot.BandLong, Frequency: autopilot.AtMostOnce,
		}}
		res := generate(t, p, in)
		sun := slotOn(res, autopilot.Sunday)
		if sun == nil || !strings.HasPrefix(sun.ItemID, "smoked-") {
			t.Fatalf("sunday = %+v: %s", sun, describe(res))
		}
		protein := map[string]string{"smoked-pork": "Pork", "smoked-chicken": "Chicken"}[sun.ItemID]
		if want := "Sunday smoker night · " + protein + " · Long cook OK"; sun.Reasons[0].Text != want {
			t.Errorf("sunday reason = %q, want %q", sun.Reasons[0].Text, want)
		}
		smoked := 0
		for _, s := range res.Slots {
			if strings.HasPrefix(s.ItemID, "smoked-") {
				smoked++
			}
		}
		if smoked != 1 {
			t.Errorf("planned %d smoker meals; an at-most-once rule allows one: %s", smoked, describe(res))
		}
		// The long Sunday cook is allowed even with no long meals allowed
		// elsewhere, so it isn't penalized.
		if sun.Signals[SignalCookTimeMix] != 0 || sun.TimeBand != autopilot.BandLong {
			t.Errorf("sunday cook-time mix = %v, band %s", sun.Signals[SignalCookTimeMix], sun.TimeBand)
		}
		// On another day, the second smoker meal carries the rule penalty.
		in.Fixed = []autopilot.Assignment{{ItemID: sun.ItemID, Day: autopilot.Sunday}}
		for _, rec := range rank(t, p, in, autopilot.Saturday).Items {
			if strings.HasPrefix(rec.ItemID, "smoked-") && rec.Signals[SignalRuleFrequency] != -0.4 {
				t.Errorf("%s rule frequency = %v, want -0.4", rec.ItemID, rec.Signals[SignalRuleFrequency])
			}
		}
	})
}

func TestCookTimeMix(t *testing.T) {
	p := New(Options{})
	longAndMedium := func(longRating int) []autopilot.Item {
		var catalog []autopilot.Item
		for i := range 5 {
			catalog = append(catalog, meal(fmt.Sprintf("long-%d", i), minutes(50+i)))
			catalog = append(catalog, meal(fmt.Sprintf("medium-%d", i), minutes(30)))
		}
		return catalog
	}
	longRatings := func(score int) []autopilot.Rating {
		var ratings []autopilot.Rating
		for i := range 5 {
			ratings = append(ratings, rate(fmt.Sprintf("long-%d", i), score))
		}
		return ratings
	}
	countLong := func(res autopilot.WeekResult) (n int, days []int) {
		for _, s := range res.Slots {
			if s.TimeBand == autopilot.BandLong {
				n++
				days = append(days, s.Day.Index())
			}
		}
		return n, days
	}

	t.Run("at most N long meals", func(t *testing.T) {
		in := input(longAndMedium(4)...)
		in.Ratings = longRatings(4)
		in.Preferences.Novelty = autopilot.NoveltyAdventurous
		in.Preferences.CookTime = autopilot.CookTimeMix{MaxLongPerWeek: 1}
		res := generate(t, p, in)
		if n, _ := countLong(res); n != 1 {
			t.Errorf("planned %d long meals, want 1: %s", n, describe(res))
		}
		in.Preferences.CookTime.MaxLongPerWeek = -1
		if n, _ := countLong(generate(t, p, in)); n != 5 {
			t.Errorf("without a limit, planned %d long meals; want the 5 rated ones", n)
		}
	})

	t.Run("no back-to-back long meals", func(t *testing.T) {
		catalog := longAndMedium(5)
		in := input(catalog...)
		in.Ratings = longRatings(5)[:3]
		in.Preferences.Novelty = autopilot.NoveltyAdventurous
		in.Preferences.CookTime = autopilot.CookTimeMix{MaxLongPerWeek: -1, AvoidConsecutiveLong: true}
		res := generate(t, p, in)
		n, days := countLong(res)
		for i := 1; i < len(days); i++ {
			if days[i]-days[i-1] == 1 {
				t.Errorf("long meals on consecutive days %v: %s", days, describe(res))
			}
		}
		if n != 3 {
			t.Errorf("planned %d long meals; the 3 rated ones fit on alternate days: %s", n, describe(res))
		}
	})

	t.Run("minimum quick meals", func(t *testing.T) {
		in := input(meal("q1", minutes(15)), meal("q2", minutes(18)), meal("m1", minutes(30)), meal("m2", minutes(30)), meal("m3", minutes(30)))
		in.Ratings = []autopilot.Rating{rate("m1", 5), rate("m2", 5), rate("m3", 5)}
		in.Preferences.MealsPerWeek = 3
		in.Preferences.CookTime = autopilot.CookTimeMix{MaxLongPerWeek: -1, MinQuickPerWeek: 2}
		res := generate(t, p, in)
		quick := 0
		for _, s := range res.Slots {
			if s.TimeBand == autopilot.BandQuick {
				quick++
			}
		}
		if quick < 2 {
			t.Errorf("planned %d quick meals, want at least 2: %s", quick, describe(res))
		}
	})

	t.Run("bands are configurable", func(t *testing.T) {
		in := input(meal("m", minutes(18)))
		in.Preferences.CookTime.Bands = autopilot.TimeBands{QuickMaxMinutes: 15, MediumMaxMinutes: 17}
		if got := rank(t, p, in, autopilot.Monday).Items[0].TimeBand; got != autopilot.BandLong {
			t.Errorf("18 minutes with bands 15/17 = %s, want long", got)
		}
	})
}

func TestFixedMealsAndShortCatalogs(t *testing.T) {
	p := New(Options{})
	in := input(meal("a"), meal("b"), meal("c"), meal("d"), meal("e"), meal("f"))
	in.Fixed = []autopilot.Assignment{{ItemID: "a", Day: autopilot.Tuesday}, {ItemID: "b"}}
	res := generate(t, p, in)
	if res.Planned != 3 || slotOn(res, autopilot.Tuesday) != nil {
		t.Fatalf("with two fixed meals = %s", describe(res))
	}
	if ids := pickedIDs(res); slices.Contains(ids, "a") || slices.Contains(ids, "b") {
		t.Errorf("fixed meals planned again: %v", ids)
	}
	if !slices.ContainsFunc(res.Messages, func(m autopilot.Message) bool { return m.Code == "already_planned" }) {
		t.Errorf("messages = %+v", res.Messages)
	}

	in.Fixed = []autopilot.Assignment{{ItemID: "a"}, {ItemID: "b"}, {ItemID: "c"}, {ItemID: "d"}, {ItemID: "e"}}
	if res := generate(t, p, in); res.Planned != 0 || res.Messages[0].Code != "week_full" {
		t.Errorf("full week = %s", describe(res))
	}

	in = input(meal("a"), meal("b"), meal("c"))
	in.Preferences.PlanDays = autopilot.Days
	in.Preferences.MealsPerWeek = 7
	res = generate(t, p, in)
	want := "Only 3 recipes match your preferences; planned 3 of 7 nights."
	if res.Planned != 3 || len(res.Unfilled) != 4 || res.Messages[0].Text != want {
		t.Errorf("short catalog = %s; want %q", describe(res), want)
	}
	if res := generate(t, p, input()); res.Planned != 0 || res.Messages[0].Code != "empty_catalog" {
		t.Errorf("empty catalog = %s", describe(res))
	}
}

func TestRankMealsForSwap(t *testing.T) {
	p := New(Options{})
	in := input(meal("tacos", cuisine("mexican")), meal("burger", cuisine("american")), meal("enchiladas", cuisine("mexican")),
		meal("pad-thai", cuisine("thai")), meal("long-bbq", cuisine("bbq"), minutes(90)))
	in.Ratings = []autopilot.Rating{rate("burger", 5), rate("enchiladas", 5), rate("pad-thai", 4), rate("long-bbq", 5)}
	in.Fixed = []autopilot.Assignment{{ItemID: "tacos", Day: autopilot.Monday}}
	in.Context.MaxMinutes = 60

	// Swapping Tuesday's burger: the next best that still fits the week.
	res := rank(t, p, in, autopilot.Tuesday, "burger")
	if res.Eligible != 3 || len(res.Items) != 2 {
		t.Fatalf("ranking = %+v; want enchiladas and pad thai (tacos fixed, burger excluded, bbq over the cap)", res)
	}
	if res.Items[0].ItemID != "pad-thai" || res.Items[1].Signals[SignalVariety] != -0.2 {
		t.Errorf("swap candidates = %+v; a second Mexican meal should rank below pad thai", res.Items)
	}

	// Nothing left.
	res = rank(t, p, in, autopilot.Tuesday, "burger", "enchiladas", "pad-thai")
	if len(res.Items) != 0 || len(res.Messages) != 1 || res.Messages[0].Text != "No other recipe is ready within 60 minutes on Tuesday." {
		t.Errorf("exhausted ranking = %+v", res)
	}
}

// TestDeterminism checks that the same request gives the same week, however
// its lists are ordered.
func TestDeterminism(t *testing.T) {
	in := bigInput(120, 600)
	p := New(Options{})
	first := generate(t, p, in)
	if first.Planned != 7 {
		t.Fatalf("planned %d: %s", first.Planned, describe(first))
	}
	again := generate(t, New(Options{}), in)
	if !reflect.DeepEqual(first, again) {
		t.Fatalf("two runs differ:\n%s\n%s", describe(first), describe(again))
	}

	shuffled := in
	shuffled.Catalog = slices.Clone(in.Catalog)
	slices.Reverse(shuffled.Catalog)
	shuffled.Ratings = slices.Clone(in.Ratings)
	slices.Reverse(shuffled.Ratings)
	shuffled.History = slices.Clone(in.History)
	slices.Reverse(shuffled.History)
	if got := generate(t, p, shuffled); !reflect.DeepEqual(first, got) {
		t.Fatalf("reordered input changed the week:\n%s\n%s", describe(first), describe(got))
	}

	res, err := p.GenerateWeek(context.Background(), autopilot.WeekRequest{Input: in, Attempt: 2})
	if err != nil || res.Seed == first.Seed {
		t.Errorf("attempt 2 seed = %d, attempt 1 = %d; the seed must include the attempt", res.Seed, first.Seed)
	}
	versioned := New(Options{ModelVersion: "baseline-test"})
	if res := generate(t, versioned, in); res.Seed == first.Seed || res.ModelVersion != "baseline-test" {
		t.Errorf("model version is not part of the seed: %d", res.Seed)
	}
}

// TestPerformance plans a week for a large household well under a second.
func TestPerformance(t *testing.T) {
	in := bigInput(500, 5000)
	p := New(Options{})
	start := time.Now()
	res := generate(t, p, in)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("GenerateWeek took %v for 500 recipes and 5000 interactions", elapsed)
	}
	if res.Planned != 7 {
		t.Errorf("planned %d: %s", res.Planned, describe(res))
	}
}

func BenchmarkGenerateWeek(b *testing.B) {
	in := bigInput(500, 5000)
	p := New(Options{})
	for b.Loop() {
		if _, err := p.GenerateWeek(context.Background(), autopilot.WeekRequest{Input: in, Attempt: 1}); err != nil {
			b.Fatal(err)
		}
	}
}

// bigInput builds a synthetic household with n meals and h interactions.
func bigInput(n, h int) autopilot.Input {
	cuisines := []string{"italian", "mexican", "thai", "indian", "american", "greek", "japanese", "korean"}
	proteinList := []string{"chicken", "beef", "pork", "tofu", "fish", "shrimp", "beans"}
	var catalog []autopilot.Item
	for i := range n {
		it := meal(fmt.Sprintf("meal-%04d", i),
			cuisine(cuisines[i%len(cuisines)]), proteins(proteinList[(i/3)%len(proteinList)]),
			minutes(10+(i*7)%60), tags(fmt.Sprintf("tag-%d", i%12)),
			ingredients("onion", "garlic", fmt.Sprintf("ingredient %d", i%40), "olive oil"))
		if i%50 == 0 {
			it.Methods = []string{"smoker"}
		}
		catalog = append(catalog, it)
	}
	in := input(catalog...)
	in.Preferences.PlanDays = autopilot.Days
	in.Preferences.MealsPerWeek = 7
	in.Preferences.Weeknights = weekdays[:4]
	in.Preferences.WeeknightMaxMinutes = 35
	in.Preferences.Likes.Cuisines = []string{"thai", "mexican"}
	in.Preferences.Equipment = []string{"smoker"}
	in.Preferences.CookTime = autopilot.CookTimeMix{MaxLongPerWeek: 2, AvoidConsecutiveLong: true}
	in.Preferences.Rules = []autopilot.WeekdayRule{
		{Day: autopilot.Tuesday, Label: "Taco Tuesday", Cuisines: []string{"mexican"}},
		{Day: autopilot.Sunday, Label: "Smoker night", Methods: []string{"smoker"}, TimeBand: autopilot.BandLong, Frequency: autopilot.AtMostOnce},
	}
	in.Context.PantryLow = []string{"ingredient 3"}
	kinds := []autopilot.InteractionKind{autopilot.KindOrdered, autopilot.KindPlanned, autopilot.KindCooked, autopilot.KindSkipped}
	for i := range h {
		in.History = append(in.History, autopilot.Interaction{
			ItemID: fmt.Sprintf("meal-%04d", (i*37)%n), Kind: kinds[i%len(kinds)],
			Week: fmt.Sprintf("2026-W%02d", 1+i%37), Day: autopilot.Days[i%7],
		})
	}
	for i := range h / 4 {
		in.Ratings = append(in.Ratings, autopilot.Rating{
			ItemID: fmt.Sprintf("meal-%04d", (i*13)%n), MemberID: fmt.Sprintf("member-%d", i%3), Score: 1 + i%5,
		})
	}
	return in
}
