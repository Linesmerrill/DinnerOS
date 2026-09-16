package baseline

import (
	"context"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

func swappedOut(id, week string) autopilot.Interaction {
	return autopilot.Interaction{ItemID: id, Kind: autopilot.KindSwappedOut, Week: week}
}

func interaction(id string, kind autopilot.InteractionKind, week string) autopilot.Interaction {
	return autopilot.Interaction{ItemID: id, Kind: kind, Week: week}
}

func recommendationFor(t *testing.T, res autopilot.RankResult, id string) autopilot.Recommendation {
	t.Helper()
	for _, r := range res.Items {
		if r.ItemID == id {
			return r
		}
	}
	t.Fatalf("%s not ranked: %+v", id, res.Items)
	return autopilot.Recommendation{}
}

func position(res autopilot.RankResult, id string) int {
	return slices.IndexFunc(res.Items, func(r autopilot.Recommendation) bool { return r.ItemID == id })
}

func TestLearnedMealFeedback(t *testing.T) {
	p := New(Options{})
	in := input(meal("swapped"), meal("left-out"), meal("kept"), meal("viewed"), meal("plain"))
	in.History = []autopilot.Interaction{
		swappedOut("swapped", "2026-W37"), swappedOut("swapped", "2026-W35"),
		interaction("left-out", autopilot.KindRejected, "2026-W36"),
		interaction("kept", autopilot.KindAccepted, "2026-W36"),
		interaction("viewed", autopilot.KindViewed, "2026-W37"), interaction("viewed", autopilot.KindViewed, "2026-W37"),
		interaction("viewed", autopilot.KindViewed, "2026-W36"),
	}
	res := rank(t, p, in, autopilot.Tuesday)

	for id, want := range map[string]string{
		"swapped":  "You swapped this out twice recently",
		"left-out": "You left this out once recently",
		"kept":     "You kept it when Autopilot suggested it",
		"viewed":   "You've looked at it 3 times lately",
	} {
		r := recommendationFor(t, res, id)
		if !hasReason(r.Reasons, want) {
			t.Errorf("%s reasons = %v, want %q", id, reasonTexts(r.Reasons), want)
		}
		if r.Signals[SignalLearned] == 0 {
			t.Errorf("%s has no learned signal: %v", id, r.Signals)
		}
	}
	plain := recommendationFor(t, res, "plain")
	if _, ok := plain.Signals[SignalLearned]; ok {
		t.Errorf("a meal with no feedback has a learned signal: %v", plain.Signals)
	}
	if !(position(res, "kept") < position(res, "plain") && position(res, "plain") < position(res, "left-out") &&
		position(res, "left-out") < position(res, "swapped")) {
		t.Errorf("ranking doesn't follow feedback: %v", pickedRank(res))
	}
	// Learning is bounded by its weight.
	for _, r := range res.Items {
		if v := r.Signals[SignalLearned]; v < -1 || v > 1 {
			t.Errorf("%s learned = %v, outside -1..1", r.ItemID, v)
		}
	}
}

func pickedRank(res autopilot.RankResult) []string {
	var out []string
	for _, r := range res.Items {
		out = append(out, r.ItemID)
	}
	return out
}

func TestLearnedReasonsAreAlwaysShown(t *testing.T) {
	p := New(Options{})
	in := input(meal("favorite", ingredients("cilantro")), meal("other"))
	in.Context.PantryLow = []string{"cilantro"}
	in.Ratings = []autopilot.Rating{rate("favorite", 5, autopilot.FeedbackMakeAgain)}
	for _, w := range []string{"2026-W20", "2026-W22", "2026-W24", "2026-W26"} {
		in.History = append(in.History, interaction("favorite", autopilot.KindOrdered, w))
	}
	in.History = append(in.History, swappedOut("favorite", "2026-W37"))
	r := recommendationFor(t, rank(t, p, in, autopilot.Tuesday), "favorite")
	if !hasReason(r.Reasons, "You swapped this out once recently") || len(r.Reasons) > maxTotalReasons {
		t.Errorf("reasons = %v, want the swap among at most %d", reasonTexts(r.Reasons), maxTotalReasons)
	}
	if !hasReason(r.Reasons, "You rated this 5★") {
		t.Errorf("reasons = %v, want the standard reasons kept", reasonTexts(r.Reasons))
	}
}

func TestLearnedAttributeAffinities(t *testing.T) {
	p := New(Options{})
	catalog := []autopilot.Item{meal("taco-new", cuisine("mexican")), meal("pasta-new", cuisine("italian")), meal("neutral", cuisine("thai"))}
	for _, w := range []string{"2026-W30", "2026-W31", "2026-W32", "2026-W33"} {
		catalog = append(catalog, meal("taco-"+w, cuisine("mexican")), meal("pasta-"+w, cuisine("italian")))
	}
	in := input(catalog...)
	for _, w := range []string{"2026-W30", "2026-W31", "2026-W32", "2026-W33"} {
		in.History = append(in.History,
			interaction("taco-"+w, autopilot.KindCooked, w), interaction("taco-"+w, autopilot.KindCooked, w),
			interaction("pasta-"+w, autopilot.KindSwappedOut, w), interaction("pasta-"+w, autopilot.KindCooked, w))
	}
	res := rank(t, p, in, autopilot.Tuesday)
	if r := recommendationFor(t, res, "taco-new"); !hasReason(r.Reasons, "Lately you favor Mexican") {
		t.Errorf("taco reasons = %v", reasonTexts(r.Reasons))
	}
	if r := recommendationFor(t, res, "pasta-new"); !hasReason(r.Reasons, "Lately you pass on Italian") {
		t.Errorf("pasta reasons = %v", reasonTexts(r.Reasons))
	}
	if position(res, "taco-new") > position(res, "neutral") || position(res, "pasta-new") < position(res, "neutral") {
		t.Errorf("ranking = %v", pickedRank(res))
	}

	// A stated like or dislike wins: the attribute isn't learned on top.
	in.Preferences.Likes.Cuisines = []string{"italian"}
	if r := recommendationFor(t, rank(t, p, in, autopilot.Tuesday), "pasta-new"); hasReason(r.Reasons, "Lately you pass on Italian") {
		t.Errorf("learned a stated like: %v", reasonTexts(r.Reasons))
	}
}

func TestLearnedBusyWeekSkips(t *testing.T) {
	p := New(Options{})
	in := input(meal("slow", minutes(30)), meal("other", minutes(30)))
	in.Preferences.Weeknights = []autopilot.Day{autopilot.Monday, autopilot.Tuesday}
	in.History = []autopilot.Interaction{
		{ItemID: "slow", Kind: autopilot.KindSkipped, Week: "2026-W30", Busy: true},
		{ItemID: "slow", Kind: autopilot.KindSkipped, Week: "2026-W34", Reason: autopilot.SkipNoTime},
	}
	const text = "Often skipped on busy weeks"
	if r := recommendationFor(t, rank(t, p, in, autopilot.Tuesday), "slow"); hasReason(r.Reasons, text) {
		t.Errorf("an ordinary week applies busy skips: %v", reasonTexts(r.Reasons))
	}
	in.Context.Busy = true
	res := rank(t, p, in, autopilot.Tuesday)
	if r := recommendationFor(t, res, "slow"); !hasReason(r.Reasons, text) || position(res, "slow") != 1 {
		t.Errorf("busy week: %v, ranking %v", reasonTexts(r.Reasons), pickedRank(res))
	}
	// A busy week is about weeknights; a busy day from the calendar counts
	// on any day.
	if r := recommendationFor(t, rank(t, p, in, autopilot.Friday), "slow"); hasReason(r.Reasons, text) {
		t.Errorf("busy week applied on a non-weeknight: %v", reasonTexts(r.Reasons))
	}
	in.Context.Busy = false
	in.Context.Days = []autopilot.DayContext{{Day: autopilot.Friday, Signals: autopilot.Signals{autopilot.ContextBusyness: autopilot.Text("busy")}}}
	if r := recommendationFor(t, rank(t, p, in, autopilot.Friday), "slow"); !hasReason(r.Reasons, text) {
		t.Errorf("busy calendar day: %v", reasonTexts(r.Reasons))
	}
}

func TestLearningNeverReversesStatedPreferences(t *testing.T) {
	p := New(Options{})
	in := input(meal("liked", proteins("pork")), meal("neutral-1"), meal("disliked", proteins("lamb")), meal("neutral-2"))
	in.Preferences.Likes.Proteins = []string{"pork"}
	in.Preferences.Dislikes.Proteins = []string{"lamb"}
	for _, w := range []string{"2026-W37", "2026-W36", "2026-W35", "2026-W34", "2026-W33"} {
		in.History = append(in.History, swappedOut("liked", w), interaction("liked", autopilot.KindRejected, w),
			interaction("disliked", autopilot.KindAccepted, w), interaction("disliked", autopilot.KindViewed, w))
	}
	res := rank(t, p, in, autopilot.Tuesday)
	liked, disliked := recommendationFor(t, res, "liked"), recommendationFor(t, res, "disliked")
	for _, neutral := range []string{"neutral-1", "neutral-2"} {
		n := recommendationFor(t, res, neutral)
		if liked.Score <= n.Score {
			t.Errorf("learning reversed a like: liked %.3f <= %s %.3f", liked.Score, neutral, n.Score)
		}
		if disliked.Score >= n.Score {
			t.Errorf("learning reversed a dislike: disliked %.3f >= %s %.3f", disliked.Score, neutral, n.Score)
		}
	}
	if liked.Signals[SignalLearned] >= 0 || disliked.Signals[SignalLearned] <= 0 {
		t.Errorf("learning should still apply, shrunk: liked %v, disliked %v", liked.Signals, disliked.Signals)
	}
}

func TestLearningCannotReintroduceHardConstraints(t *testing.T) {
	p := New(Options{})
	in := input(meal("allergen", allergens("peanuts")), meal("never"), meal("ok"))
	in.Preferences.Allergens = []string{"peanuts"}
	in.Ratings = []autopilot.Rating{rate("never", 4, autopilot.FeedbackNeverAgain)}
	for _, w := range []string{"2026-W37", "2026-W36", "2026-W35"} {
		for _, id := range []string{"allergen", "never"} {
			in.History = append(in.History, interaction(id, autopilot.KindAccepted, w), interaction(id, autopilot.KindCooked, w),
				interaction(id, autopilot.KindViewed, w))
		}
	}
	res := generate(t, p, in)
	for _, id := range pickedIDs(res) {
		if id != "ok" {
			t.Errorf("picked %s: %s", id, describe(res))
		}
	}
}

func TestLearningSinceReset(t *testing.T) {
	p := New(Options{})
	in := input(meal("a"), meal("b"))
	reset := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	in.History = []autopilot.Interaction{
		{ItemID: "a", Kind: autopilot.KindSwappedOut, Week: "2026-W36", At: reset.Add(-time.Hour)},
		{ItemID: "a", Kind: autopilot.KindSwappedOut, Week: "2026-W37"},
		{ItemID: "b", Kind: autopilot.KindSwappedOut, Week: "2026-W37", At: reset.Add(time.Hour)},
	}
	in.LearningSince = reset
	res := rank(t, p, in, autopilot.Tuesday)
	if r := recommendationFor(t, res, "a"); r.Signals[SignalLearned] != 0 {
		t.Errorf("interactions before the reset (or without a time) were learned: %v %v", r.Signals, reasonTexts(r.Reasons))
	}
	if r := recommendationFor(t, res, "b"); !hasReason(r.Reasons, "You swapped this out once recently") {
		t.Errorf("interactions after the reset weren't learned: %v", reasonTexts(r.Reasons))
	}
	learned, err := p.Learning(context.Background(), in)
	if err != nil || len(learned.Adjustments) != 1 || learned.Adjustments[0].Key != "b" || learned.Interactions != 1 {
		t.Errorf("Learning() = %+v, %v", learned, err)
	}
}

func TestLearningOldEvidenceFades(t *testing.T) {
	p := New(Options{})
	in := input(meal("recent"), meal("old"), meal("ancient"))
	in.History = []autopilot.Interaction{swappedOut("recent", "2026-W37"), swappedOut("old", "2026-W22"), swappedOut("ancient", "2025-W50")}
	res := rank(t, p, in, autopilot.Tuesday)
	recent, old := recommendationFor(t, res, "recent").Signals[SignalLearned], recommendationFor(t, res, "old").Signals[SignalLearned]
	if !(recent < old && old < 0) {
		t.Errorf("recent %v, old %v: want recent swaps to weigh more", recent, old)
	}
	if v := recommendationFor(t, res, "ancient").Signals[SignalLearned]; v != 0 {
		t.Errorf("a swap outside the window still counts: %v", v)
	}
}

func TestLearningReport(t *testing.T) {
	p := New(Options{})
	catalog := []autopilot.Item{meal("swapped", cuisine("thai"))}
	var history []autopilot.Interaction
	for _, w := range []string{"2026-W30", "2026-W31", "2026-W32", "2026-W33"} {
		catalog = append(catalog, meal("taco-"+w, cuisine("mexican"), minutes(15)))
		history = append(history, interaction("taco-"+w, autopilot.KindCooked, w), interaction("taco-"+w, autopilot.KindRated, w))
		history[len(history)-1].Score = 5
	}
	history = append(history, swappedOut("swapped", "2026-W37"), swappedOut("swapped", "2026-W36"),
		autopilot.Interaction{ItemID: "swapped", Kind: autopilot.KindSkipped, Week: "2026-W35", Busy: true},
		autopilot.Interaction{ItemID: "swapped", Kind: autopilot.KindSkipped, Week: "2026-W34", Busy: true})
	in := input(catalog...)
	in.History = history
	res, err := p.Learning(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[string]autopilot.LearnedAdjustment{}
	for _, a := range res.Adjustments {
		byKind[a.Kind+"/"+a.Key] = a
	}
	for key, text := range map[string]string{
		"item/swapped":      "You swapped this out twice recently",
		"busySkips/swapped": "Often skipped on busy weeks",
		"cuisine/mexican":   "Lately you favor Mexican",
		"cuisine/thai":      "Lately you pass on Thai",
		"timeBand/quick":    "Lately you favor quick meals",
	} {
		if a, ok := byKind[key]; !ok || a.Text != text || a.Evidence == 0 {
			t.Errorf("%s = %+v, want %q (all: %+v)", key, a, text, res.Adjustments)
		}
	}
	for i := 1; i < len(res.Adjustments); i++ {
		if abs64(res.Adjustments[i].Value) > abs64(res.Adjustments[i-1].Value) {
			t.Errorf("adjustments aren't strongest first: %+v", res.Adjustments)
		}
	}
	if res.ModelVersion != ModelVersion || res.Interactions != len(history) {
		t.Errorf("result = %+v", res)
	}
	empty, err := p.Learning(context.Background(), input(meal("a")))
	if err != nil || len(empty.Adjustments) != 0 || empty.Interactions != 0 {
		t.Errorf("Learning() without history = %+v, %v", empty, err)
	}
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// TestLearningAndContextAreDeterministic shuffles history and catalog order:
// the same inputs must give the same week.
func TestLearningAndContextAreDeterministic(t *testing.T) {
	p := New(Options{})
	var catalog []autopilot.Item
	cuisines := []string{"mexican", "italian", "thai", "indian"}
	cats := []string{"soup", "pasta", "salad", "tacos"}
	for i := range 60 {
		id := string(rune('a'+i%26)) + string(rune('a'+i/26))
		catalog = append(catalog, meal(id, cuisine(cuisines[i%4]), minutes(10+i%50), func(it *autopilot.Item) {
			it.MealCategories = []string{cats[(i/3)%4]}
			if i%5 == 0 {
				it.Methods = []string{"grill"}
			}
		}))
	}
	in := input(catalog...)
	in.Context.Signals = autopilot.Signals{autopilot.ContextSeason: autopilot.Text("winter"), autopilot.ContextOrderDate: autopilot.Text("2026-09-12")}
	in.Context.Days = []autopilot.DayContext{
		{Day: autopilot.Tuesday, Signals: autopilot.Signals{autopilot.ContextTemperatureBand: autopilot.Text("cold"), autopilot.ContextPrecipitation: autopilot.Text("rain")}},
		{Day: autopilot.Wednesday, Signals: autopilot.Signals{autopilot.ContextBusyness: autopilot.Text("busy")}},
		{Day: autopilot.Thursday, Signals: autopilot.Signals{autopilot.ContextHoliday: autopilot.Text("Harvest Day"), autopilot.ContextHolidayKind: autopilot.Text("feast")}},
	}
	kinds := []autopilot.InteractionKind{autopilot.KindCooked, autopilot.KindSkipped, autopilot.KindSwappedOut, autopilot.KindAccepted, autopilot.KindRejected, autopilot.KindViewed, autopilot.KindRated}
	for i := range 400 {
		h := autopilot.Interaction{ItemID: catalog[(i*7)%len(catalog)].ID, Kind: kinds[i%len(kinds)], Week: []string{"2026-W30", "2026-W33", "2026-W36", "2026-W37"}[i%4], Score: 1 + i%5, Busy: i%3 == 0}
		in.History = append(in.History, h)
	}
	want := generate(t, p, in)
	if len(want.Slots) != 5 {
		t.Fatalf("week = %s", describe(want))
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 5 {
		shuffled := in
		shuffled.History = slices.Clone(in.History)
		shuffled.Catalog = slices.Clone(in.Catalog)
		rng.Shuffle(len(shuffled.History), func(i, j int) { shuffled.History[i], shuffled.History[j] = shuffled.History[j], shuffled.History[i] })
		rng.Shuffle(len(shuffled.Catalog), func(i, j int) { shuffled.Catalog[i], shuffled.Catalog[j] = shuffled.Catalog[j], shuffled.Catalog[i] })
		if got := generate(t, p, shuffled); !reflect.DeepEqual(got, want) {
			t.Fatalf("week changed with input order:\n got %s\nwant %s", describe(got), describe(want))
		}
		a, _ := p.Learning(context.Background(), in)
		b, _ := p.Learning(context.Background(), shuffled)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("Learning() changed with input order")
		}
	}
}

func TestLearningOffWithZeroWeight(t *testing.T) {
	w := DefaultWeights()
	w.Learned = 0
	p := New(Options{Weights: w})
	in := input(meal("a"), meal("b"))
	in.History = []autopilot.Interaction{swappedOut("a", "2026-W37")}
	if r := recommendationFor(t, rank(t, p, in, autopilot.Tuesday), "a"); r.Signals[SignalLearned] != 0 || len(r.Reasons) != 1 {
		t.Errorf("learning with zero weight: %v %v", r.Signals, reasonTexts(r.Reasons))
	}
}
