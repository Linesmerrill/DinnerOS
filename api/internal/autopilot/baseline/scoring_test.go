package baseline

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// TestSignals checks each meal signal on a one-meal catalog.
func TestSignals(t *testing.T) {
	history := func(kind autopilot.InteractionKind, week string, day autopilot.Day) autopilot.Interaction {
		return autopilot.Interaction{ItemID: "m", Kind: kind, Week: week, Day: day}
	}
	tests := []struct {
		name       string
		day        autopilot.Day
		item       autopilot.Item
		setup      func(in *autopilot.Input)
		signal     string
		want       float64
		wantReason string
	}{
		{"rating", autopilot.Monday, meal("m"), func(in *autopilot.Input) { in.Ratings = []autopilot.Rating{rate("m", 5)} },
			SignalRating, 1, "You rated this 5★"},
		{"household average", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			in.Ratings = []autopilot.Rating{rate("m", 5), {ItemID: "m", MemberID: "member-2", Score: 4}}
		}, SignalRating, 0.75, "Rated 4.5★ by your household"},
		{"low rating", autopilot.Monday, meal("m"), func(in *autopilot.Input) { in.Ratings = []autopilot.Rating{rate("m", 1)} },
			SignalRating, -1, ""},
		{"make again", autopilot.Monday, meal("m"), func(in *autopilot.Input) { in.Ratings = []autopilot.Rating{rate("m", 4, "make-again")} },
			SignalFeedback, 0.6, "Marked make-again"},
		{"too much work on a weeknight", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			in.Preferences.Weeknights = []autopilot.Day{autopilot.Monday}
			in.Ratings = []autopilot.Rating{rate("m", 3, "too-much-work")}
		}, SignalFeedback, -0.6, ""},
		{"had it last week", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			in.History = []autopilot.Interaction{history(autopilot.KindOrdered, "2026-W37", "")}
		}, SignalRecency, -1, ""},
		{"had it three weeks ago", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			in.History = []autopilot.Interaction{history(autopilot.KindCooked, "2026-W35", "")}
		}, SignalRecency, -0.4, ""},
		{"a favorite not had in a while", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			in.History = []autopilot.Interaction{history(autopilot.KindOrdered, "2026-W20", ""), history(autopilot.KindOrdered, "2026-W25", "")}
		}, SignalRecency, 0.3, "Haven't had it in 13 weeks"},
		{"skips don't count as having it", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			in.History = []autopilot.Interaction{history(autopilot.KindSkipped, "2026-W37", "")}
		}, SignalRecency, 0, ""},
		{"familiarity", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			for _, w := range []string{"2026-W01", "2026-W05", "2026-W09", "2026-W13", "2026-W17", "2026-W21", "2026-W25", "2026-W29"} {
				in.History = append(in.History, history(autopilot.KindOrdered, w, ""))
			}
		}, SignalFamiliarity, 1, "A household regular (8 times)"},
		{"conversion", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			for _, w := range []string{"2026-W10", "2026-W20", "2026-W28"} {
				in.History = append(in.History, history(autopilot.KindCooked, w, ""))
			}
		}, SignalConversion, 0.6, "You usually cook it when it's planned"},
		{"skipped more than cooked", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			in.History = []autopilot.Interaction{history(autopilot.KindSkipped, "2026-W10", ""), history(autopilot.KindSkipped, "2026-W20", "")}
		}, SignalConversion, -0.5, ""},
		{"weekday affinity", autopilot.Tuesday, meal("m"), func(in *autopilot.Input) {
			in.History = []autopilot.Interaction{history(autopilot.KindPlanned, "2026-W20", autopilot.Tuesday), history(autopilot.KindPlanned, "2026-W26", autopilot.Tuesday)}
		}, SignalWeekdayAffinity, 1, "Often on Tuesdays"},
		{"other weekday", autopilot.Friday, meal("m"), func(in *autopilot.Input) {
			in.History = []autopilot.Interaction{history(autopilot.KindPlanned, "2026-W20", autopilot.Tuesday), history(autopilot.KindPlanned, "2026-W26", autopilot.Tuesday)}
		}, SignalWeekdayAffinity, 0, ""},
		{"liked cuisine", autopilot.Monday, meal("m", cuisine("Mexican")), func(in *autopilot.Input) { in.Preferences.Likes.Cuisines = []string{"mexican"} },
			SignalTaste, 0.4, "You like Mexican"},
		{"liked and disliked", autopilot.Monday, meal("m", cuisine("thai"), proteins("shrimp")), func(in *autopilot.Input) {
			in.Preferences.Likes.Cuisines = []string{"thai"}
			in.Preferences.Dislikes.Proteins = []string{"shrimp"}
		}, SignalTaste, -0.1, ""},
		{"weeknight time limit met", autopilot.Monday, meal("m", minutes(15)), func(in *autopilot.Input) {
			in.Preferences.Weeknights = []autopilot.Day{autopilot.Monday}
			in.Preferences.WeeknightMaxMinutes = 30
		}, SignalTimeFit, 0.75, "15 min, easy for a weeknight"},
		{"weeknight time limit missed", autopilot.Monday, meal("m", minutes(45)), func(in *autopilot.Input) {
			in.Preferences.Weeknights = []autopilot.Day{autopilot.Monday}
			in.Preferences.WeeknightMaxMinutes = 30
		}, SignalTimeFit, -1, ""},
		{"weekend has no weeknight limit", autopilot.Saturday, meal("m", minutes(45)), func(in *autopilot.Input) {
			in.Preferences.Weeknights = []autopilot.Day{autopilot.Monday}
			in.Preferences.WeeknightMaxMinutes = 30
		}, SignalTimeFit, 0, ""},
		{"new meal for a favorites household", autopilot.Monday, meal("m"), func(in *autopilot.Input) { in.Preferences.Novelty = autopilot.NoveltyFavorites },
			SignalNovelty, -0.6, ""},
		{"new meal for an adventurous household", autopilot.Monday, meal("m"), func(in *autopilot.Input) { in.Preferences.Novelty = autopilot.NoveltyAdventurous },
			SignalNovelty, 0.6, "Something new to try"},
		{"pantry running low", autopilot.Monday, meal("m", ingredients("Fresh Cilantro", "lime")), func(in *autopilot.Input) {
			in.Context.PantryLow = []string{"cilantro"}
		}, SignalPantry, 0.5, "Uses up the cilantro running low"},
		{"avoid", autopilot.Monday, meal("m"), func(in *autopilot.Input) { in.Avoid = []string{"m"} }, SignalAvoid, -1, ""},
		{"objective is bounded", autopilot.Monday, meal("m"), func(in *autopilot.Input) {
			in.Objectives = []autopilot.Objective{{ItemID: "m", Boost: 5}}
		}, SignalObjective, autopilot.MaxObjectiveBoost, ""},
	}
	p := New(Options{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := input(tt.item)
			tt.setup(&in)
			res := rank(t, p, in, tt.day)
			if len(res.Items) != 1 {
				t.Fatalf("RankMeals() = %+v", res)
			}
			rec := res.Items[0]
			if got := rec.Signals[tt.signal]; got != tt.want {
				t.Errorf("signal %s = %v, want %v (signals %v)", tt.signal, got, tt.want, rec.Signals)
			}
			if tt.wantReason != "" && !hasReason(rec.Reasons, tt.wantReason) {
				t.Errorf("reasons = %v, want %q", reasonTexts(rec.Reasons), tt.wantReason)
			}
			if len(rec.Reasons) == 0 || len(rec.Reasons) > 3 {
				t.Errorf("a pick needs 1–3 reasons, got %v", rec.Reasons)
			}
			for _, name := range []string{SignalRating, SignalFeedback, SignalFamiliarity, SignalConversion, SignalRecency,
				SignalWeekdayAffinity, SignalTaste, SignalRule, SignalTimeFit, SignalNovelty, SignalServingsFit, SignalPantry, SignalAvoid} {
				if _, ok := rec.Signals[name]; !ok {
					t.Errorf("signal %s missing", name)
				}
			}
		})
	}
}

func TestRankingFollowsScores(t *testing.T) {
	p := New(Options{})
	in := input(meal("loved"), meal("fine"), meal("disliked"))
	in.Ratings = []autopilot.Rating{rate("loved", 5, "make-again"), rate("fine", 3), rate("disliked", 2)}
	res := rank(t, p, in, autopilot.Monday)
	if len(res.Items) != 3 || res.Items[0].ItemID != "loved" || res.Items[2].ItemID != "disliked" {
		t.Fatalf("ranking = %+v", res.Items)
	}
	if res.Items[0].Score <= res.Items[1].Score || res.Items[0].Reasons[0].Text != "You rated this 5★" {
		t.Errorf("top pick = %+v", res.Items[0])
	}
	if res.ModelVersion != ModelVersion || res.Eligible != 3 {
		t.Errorf("result = %+v", res)
	}
}

func TestColdStartLeansOnTasteProfile(t *testing.T) {
	p := New(Options{})
	in := input(meal("thai", cuisine("thai")), meal("other-1"), meal("other-2"))
	in.Preferences.Likes.Cuisines = []string{"thai"}
	in.Preferences.MealsPerWeek = 1
	res := generate(t, p, in)
	if !res.ColdStart || res.Slots[0].ItemID != "thai" {
		t.Fatalf("cold start week = %s", describe(res))
	}
	if res.Messages[len(res.Messages)-1].Code != "cold_start" {
		t.Errorf("messages = %+v", res.Messages)
	}

	// With plenty of history, the same like counts for less.
	cold := rank(t, p, in, autopilot.Monday).Items[0].Score
	for i := range 40 {
		in.History = append(in.History, autopilot.Interaction{ItemID: "other-1", Kind: autopilot.KindCooked, Week: "2025-W10", Day: autopilot.Days[i%7]})
	}
	warm := rank(t, p, in, autopilot.Monday)
	for _, rec := range warm.Items {
		if rec.ItemID == "thai" && rec.Score >= cold {
			t.Errorf("taste score with history = %v, cold start = %v; history should lower the taste weight", rec.Score, cold)
		}
	}
	if generate(t, p, in).ColdStart {
		t.Error("a household with 40 cooked meals is not in cold start")
	}
}

// TestCuisineRegions: likes and exclusions match an item's cuisine regions,
// and variety counts a shared region as a partial repeat.
func TestCuisineRegions(t *testing.T) {
	regions := func(r ...string) opt { return func(it *autopilot.Item) { it.CuisineRegions = r } }
	in := input(
		meal("pasta", cuisine("italian"), regions("southern european", "european")),
		meal("crepes", cuisine("french"), regions("western european", "european")),
		meal("ramen", cuisine("japanese"), regions("east asian", "asian")),
		meal("stew", cuisine("klingon")),
	)
	in.Preferences.MealsPerWeek = 3
	in.Preferences.Likes.Cuisines = []string{"European"}
	in.Preferences.Exclusions.Cuisines = []string{"asian"}
	res := generate(t, New(Options{}), in)
	want := map[string]bool{"pasta": true, "crepes": true, "stew": true}
	if res.Planned != 3 {
		t.Fatalf("planned %d, want every non-Asian meal: %s", res.Planned, describe(res))
	}
	for _, s := range res.Slots {
		if !want[s.ItemID] {
			t.Errorf("picked %s; the Asian region is excluded: %s", s.ItemID, describe(res))
		}
		if s.ItemID != "stew" && !hasReason(s.Reasons, "You like European") {
			t.Errorf("%s reasons = %v", s.ItemID, reasonTexts(s.Reasons))
		}
	}
	// Italian and French are different cuisines that share the European
	// region: a partial repeat, not a full one.
	if res.Score.Variety != -DefaultWeights().CuisineRegionRepeat {
		t.Errorf("variety = %v; Italian and French share only a region", res.Score.Variety)
	}
}
