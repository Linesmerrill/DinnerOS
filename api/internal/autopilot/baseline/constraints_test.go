package baseline

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// TestHardConstraintsCannotBeOverridden gives a forbidden meal every possible
// advantage (top ratings, make-again, liked cuisine, a matching rule every
// day, the largest business boost) and checks it is never planned or ranked.
func TestHardConstraintsCannotBeOverridden(t *testing.T) {
	tests := []struct {
		name      string
		forbidden autopilot.Item
		setup     func(in *autopilot.Input)
	}{
		{"allergen", meal("bad", allergens("Peanuts")), func(in *autopilot.Input) { in.Preferences.Allergens = []string{"peanuts"} }},
		{"diet", meal("bad", diets("pescatarian")), func(in *autopilot.Input) {
			in.Preferences.Diets = []string{"vegetarian"}
			in.Catalog[1].Diets, in.Catalog[2].Diets = []string{"vegetarian"}, []string{"vegetarian", "vegan"}
		}},
		{"excluded cuisine", meal("bad", cuisine("Mexican")), func(in *autopilot.Input) { in.Preferences.Exclusions.Cuisines = []string{"mexican"} }},
		{"excluded protein", meal("bad", proteins("pork")), func(in *autopilot.Input) { in.Preferences.Exclusions.Proteins = []string{"pork"} }},
		{"excluded tag", meal("bad", tags("Deep Fried")), func(in *autopilot.Input) { in.Preferences.Exclusions.Tags = []string{"deep fried"} }},
		{"excluded ingredient (plural)", meal("bad", ingredients("cremini mushrooms", "garlic")), func(in *autopilot.Input) {
			in.Preferences.Exclusions.Ingredients = []string{"Mushroom"}
		}},
		{"spicy flag", meal("bad", spicy()), func(in *autopilot.Input) { in.Preferences.Exclusions.Spicy = true }},
		{"spicy tag", meal("bad", tags("Spicy")), func(in *autopilot.Input) { in.Preferences.Exclusions.Spicy = true }},
		{"never again", meal("bad"), func(in *autopilot.Input) {
			in.Ratings = append(in.Ratings, autopilot.Rating{ItemID: "bad", MemberID: "member-2", Score: 5, Tags: []string{"never-again"}})
		}},
		{"no serving sizes", meal("bad", servings()), func(*autopilot.Input) {}},
		{"week cook-time cap", meal("bad", minutes(45)), func(in *autopilot.Input) { in.Context.MaxMinutes = 30 }},
		{"unknown cook time under a cap", meal("bad", minutes(0)), func(in *autopilot.Input) { in.Context.MaxMinutes = 30 }},
	}
	p := New(Options{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := tt.forbidden
			bad.Cuisines = append(bad.Cuisines, "loved")
			in := input(bad, meal("ok-1", minutes(20)), meal("ok-2", minutes(25)))
			in.Preferences.PlanDays = autopilot.Days
			in.Preferences.MealsPerWeek = 7
			in.Preferences.Likes.Cuisines = []string{"loved"}
			in.Ratings = []autopilot.Rating{rate("bad", 5, "make-again", "kid-favorite")}
			in.Objectives = []autopilot.Objective{{ItemID: "bad", Boost: 100}}
			for _, d := range autopilot.Days {
				in.Preferences.Rules = append(in.Preferences.Rules, autopilot.WeekdayRule{Day: d, Label: "Loved night", Cuisines: []string{"loved"}})
			}
			tt.setup(&in)

			res := generate(t, p, in)
			if slices.Contains(pickedIDs(res), "bad") {
				t.Fatalf("forbidden meal was planned: %s", describe(res))
			}
			if res.Planned != 2 || res.Candidates > 2 {
				t.Errorf("planned %d of %d candidates; want the 2 allowed meals: %s", res.Planned, res.Candidates, describe(res))
			}
			for _, d := range autopilot.Days {
				for _, rec := range rank(t, p, in, d).Items {
					if rec.ItemID == "bad" {
						t.Fatalf("RankMeals(%s) returned the forbidden meal", d)
					}
				}
			}
		})
	}
}

func TestBusyWeekCaps(t *testing.T) {
	p := New(Options{})
	catalog := []autopilot.Item{
		meal("quick-1", minutes(10)), meal("quick-2", minutes(15)), meal("quick-3", minutes(20)),
		meal("long-1", minutes(45)), meal("long-2", minutes(60)), meal("unknown", minutes(0)),
	}

	t.Run("week cap explains the shortfall", func(t *testing.T) {
		in := input(catalog...)
		in.Preferences.Weeknights = weeknights
		in.Context.MaxMinutes = 20
		res := generate(t, p, in)
		if res.Planned != 3 || res.Requested != 5 || len(res.Unfilled) != 2 {
			t.Fatalf("planned %d of %d with %d unfilled: %s", res.Planned, res.Requested, len(res.Unfilled), describe(res))
		}
		for _, s := range res.Slots {
			if !slices.Contains([]string{"quick-1", "quick-2", "quick-3"}, s.ItemID) || s.TimeBand != autopilot.BandQuick {
				t.Errorf("slot %s = %s (%s)", s.Day, s.ItemID, s.TimeBand)
			}
			// The busy-week wording is for weeknights; other days name the day.
			want := "dayLimit"
			if slices.Contains(weeknights, s.Day) {
				want = "busyWeek"
			}
			if s.Reasons[0].Code != want {
				t.Errorf("%s first reason = %+v, want %s", s.Day, s.Reasons, want)
			}
		}
		want := "Only 3 quick recipes (≤20 min) match; planned 3 of 5 nights."
		if len(res.Messages) == 0 || res.Messages[0].Text != want {
			t.Errorf("messages = %+v, want %q", res.Messages, want)
		}
		if res.Unfilled[0].Code != "no_quick_candidates" {
			t.Errorf("unfilled = %+v", res.Unfilled)
		}
	})

	t.Run("day cap applies to that day only", func(t *testing.T) {
		in := input(catalog...)
		in.Context.Days = []autopilot.DayContext{{Day: autopilot.Wednesday, MaxMinutes: 12}}
		res := generate(t, p, in)
		if s := slotOn(res, autopilot.Wednesday); s == nil || s.ItemID != "quick-1" {
			t.Fatalf("wednesday = %+v; want the only meal ready within 12 minutes: %s", s, describe(res))
		}
		if res.Planned != 5 {
			t.Errorf("planned %d, want 5: %s", res.Planned, describe(res))
		}
	})

	t.Run("unknown cook time is a medium meal without a cap", func(t *testing.T) {
		res := rank(t, p, input(catalog...), autopilot.Monday)
		for _, rec := range res.Items {
			if rec.ItemID == "unknown" && rec.TimeBand != autopilot.BandMedium {
				t.Errorf("unknown cook time band = %s", rec.TimeBand)
			}
		}
		if res.Eligible != len(catalog) {
			t.Errorf("eligible = %d, want %d", res.Eligible, len(catalog))
		}
	})

	t.Run("soft busy week prefers quick meals on weeknights", func(t *testing.T) {
		in := input(catalog...)
		in.Preferences.Weeknights = weekdays
		in.Preferences.MealsPerWeek = 3
		in.Context.Busy = true
		res := generate(t, p, in)
		for _, s := range res.Slots {
			if s.TimeBand != autopilot.BandQuick {
				t.Errorf("busy week planned %s (%s): %s", s.ItemID, s.TimeBand, describe(res))
			}
		}
	})
}

var weeknights = []autopilot.Day{autopilot.Monday, autopilot.Tuesday, autopilot.Wednesday, autopilot.Thursday}

// TestBusyWeekIsAboutWeeknights: a busy week favors quick meals on weeknights,
// a weekend long-cook rule keeps its long cook, and only an explicit cap
// limits the weekend.
func TestBusyWeekIsAboutWeeknights(t *testing.T) {
	p := New(Options{})
	catalog := []autopilot.Item{
		meal("quick-1", minutes(10)), meal("quick-2", minutes(12)), meal("quick-3", minutes(15)),
		meal("quick-4", minutes(18)), meal("quick-5", minutes(20)),
		meal("medium-1", minutes(30)), meal("medium-2", minutes(32)),
		meal("long-1", minutes(50)), meal("long-2", minutes(70)),
		meal("smoked-pork", proteins("pork"), methods("smoker"), minutes(240)),
		meal("pork-chops", proteins("pork"), methods("smoker"), minutes(20)),
	}
	busyWeek := func() autopilot.Input {
		in := input(catalog...)
		in.Preferences.PlanDays = append(slices.Clone(weekdays), autopilot.Sunday)
		in.Preferences.Weeknights = weeknights
		in.Preferences.MealsPerWeek = 6
		in.Preferences.CookTime = autopilot.CookTimeMix{MaxLongPerWeek: 2, AvoidConsecutiveLong: true}
		in.Preferences.Equipment = []string{"smoker"}
		in.Preferences.Rules = []autopilot.WeekdayRule{{
			Day: autopilot.Sunday, Label: "Smoker night", Proteins: []string{"chicken", "pork"}, Methods: []string{"smoker"},
			TimeBand: autopilot.BandLong, Frequency: autopilot.EveryWeek,
		}}
		in.Context.Busy = true
		return in
	}
	hasCode := func(reasons []autopilot.Reason, code string) bool {
		return slices.ContainsFunc(reasons, func(r autopilot.Reason) bool { return r.Code == code })
	}

	t.Run("sunday keeps its long cook and weeknights are quick", func(t *testing.T) {
		res := generate(t, p, busyWeek())
		if res.Planned != 6 {
			t.Fatalf("planned %d, want 6: %s", res.Planned, describe(res))
		}
		sun := slotOn(res, autopilot.Sunday)
		if sun == nil || sun.ItemID != "smoked-pork" || sun.TimeBand != autopilot.BandLong {
			t.Fatalf("sunday = %+v; want the long smoker cook: %s", sun, describe(res))
		}
		if want := "Smoker night · Pork · Long cook OK"; sun.Reasons[0].Text != want {
			t.Errorf("sunday reasons = %v, want %q first", reasonTexts(sun.Reasons), want)
		}
		for _, s := range res.Slots {
			busy := hasCode(s.Reasons, "busyWeek")
			switch weeknight := slices.Contains(weeknights, s.Day); {
			case weeknight && (s.TimeBand != autopilot.BandQuick || !busy):
				t.Errorf("busy weeknight %s = %s (%s, %v); want quick for the busy week", s.Day, s.ItemID, s.TimeBand, reasonTexts(s.Reasons))
			case !weeknight && busy:
				t.Errorf("%s = %s says %v; the busy week is about weeknights", s.Day, s.ItemID, reasonTexts(s.Reasons))
			}
		}
		if r := rank(t, p, busyWeek(), autopilot.Sunday); len(r.Items) == 0 || r.Items[0].ItemID != "smoked-pork" {
			t.Errorf("sunday alternatives = %+v; swapping keeps the long cook first", r.Items)
		}
	})

	for name, cap := range map[string]func(*autopilot.Input){
		"week cap still caps sunday": func(in *autopilot.Input) { in.Context.MaxMinutes = 25 },
		"day cap caps sunday": func(in *autopilot.Input) {
			in.Context.Days = []autopilot.DayContext{{Day: autopilot.Sunday, MaxMinutes: 25}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := busyWeek()
			cap(&in)
			res := generate(t, p, in)
			sun := slotOn(res, autopilot.Sunday)
			if sun == nil || sun.ItemID != "pork-chops" {
				t.Fatalf("sunday = %+v; want the smoker meal ready within 25 minutes: %s", sun, describe(res))
			}
			if !hasCode(sun.Reasons, "dayLimit") || hasCode(sun.Reasons, "busyWeek") {
				t.Errorf("sunday reasons = %+v; want the day's limit, not the busy week", sun.Reasons)
			}
		})
	}
}

func TestSkippedWeeksAndDays(t *testing.T) {
	p := New(Options{})
	catalog := []autopilot.Item{meal("a"), meal("b"), meal("c"), meal("d"), meal("e"), meal("f")}

	in := input(catalog...)
	in.Context.Skip = true
	res := generate(t, p, in)
	if len(res.Slots) != 0 || res.Requested != 0 || len(res.Messages) != 1 || res.Messages[0].Code != "week_skipped" {
		t.Errorf("skipped week = %s", describe(res))
	}

	in = input(catalog...)
	in.Context.Days = []autopilot.DayContext{{Day: autopilot.Wednesday, Skip: true}}
	res = generate(t, p, in)
	if slotOn(res, autopilot.Wednesday) != nil || res.Planned != 4 {
		t.Fatalf("skipping wednesday = %s", describe(res))
	}
	want := "Only 4 days are open this week; planned 4 of 5 meals."
	if !slices.ContainsFunc(res.Messages, func(m autopilot.Message) bool { return m.Text == want }) {
		t.Errorf("messages = %+v, want %q", res.Messages, want)
	}

	// Fewer meals than open days: rule days first, then week order.
	in = input(catalog...)
	in.Preferences.MealsPerWeek = 2
	in.Preferences.Rules = []autopilot.WeekdayRule{{Day: autopilot.Thursday, Label: "Thursday", Cuisines: []string{"cuisine-a"}}}
	res = generate(t, p, in)
	if res.Planned != 2 || slotOn(res, autopilot.Thursday) == nil || slotOn(res, autopilot.Monday) == nil {
		t.Errorf("two meals = %s; want Thursday (rule) and Monday", describe(res))
	}
}

func TestGuestServings(t *testing.T) {
	p := New(Options{})
	catalog := []autopilot.Item{meal("small", servings(2, 4)), meal("large", servings(2, 4, 6)), meal("huge", servings(8))}

	in := input(catalog...)
	in.Preferences.MealsPerWeek = 1
	in.Preferences.PlanDays = []autopilot.Day{autopilot.Friday}
	in.Context.Servings = 6
	res := generate(t, p, in)
	s := slotOn(res, autopilot.Friday)
	if s == nil || s.ItemID == "small" {
		t.Fatalf("friday = %s; a meal that serves 6 should win", describe(res))
	}
	if s.Servings < 6 || !hasReason(s.Reasons, "Serves "+itoa(s.Servings)+" for your guests") {
		t.Errorf("friday servings = %d, reasons %v", s.Servings, reasonTexts(s.Reasons))
	}

	// A day override beats the week's servings; sizes round up.
	in.Context.Days = []autopilot.DayContext{{Day: autopilot.Friday, Servings: 3}}
	r := rank(t, p, in, autopilot.Friday)
	for _, rec := range r.Items {
		want := map[string]int{"small": 4, "large": 4, "huge": 8}[rec.ItemID]
		if rec.Servings != want {
			t.Errorf("%s servings = %d, want %d", rec.ItemID, rec.Servings, want)
		}
	}
	// Without guests, sizes follow the household default.
	if got := rank(t, p, input(catalog...), autopilot.Monday); got.Items[0].Servings != 2 && got.Items[0].ItemID != "huge" {
		t.Errorf("default servings = %+v", got.Items[0])
	}
	small := rank(t, p, func() autopilot.Input { in := input(catalog...); in.Context.Servings = 6; return in }(), autopilot.Monday)
	for _, rec := range small.Items {
		if rec.ItemID == "small" && rec.Signals[SignalServingsFit] != -0.333 {
			t.Errorf("small servingsFit = %v, want -0.333", rec.Signals[SignalServingsFit])
		}
	}
}

func TestInvalidRequests(t *testing.T) {
	p := New(Options{})
	ctx := context.Background()
	for name, mutate := range map[string]func(*autopilot.Input){
		"week":     func(in *autopilot.Input) { in.Week = "2026-38" },
		"plan day": func(in *autopilot.Input) { in.Preferences.PlanDays = []autopilot.Day{"funday"} },
		"rule day": func(in *autopilot.Input) { in.Preferences.Rules = []autopilot.WeekdayRule{{Day: "x"}} },
		"rule band": func(in *autopilot.Input) {
			in.Preferences.Rules = []autopilot.WeekdayRule{{Day: "mon", TimeBand: "forever"}}
		},
		"rule freq": func(in *autopilot.Input) {
			in.Preferences.Rules = []autopilot.WeekdayRule{{Day: "mon", Frequency: "daily"}}
		},
		"novelty":      func(in *autopilot.Input) { in.Preferences.Novelty = "wild" },
		"context day":  func(in *autopilot.Input) { in.Context.Days = []autopilot.DayContext{{Day: "x"}} },
		"fixed day":    func(in *autopilot.Input) { in.Fixed = []autopilot.Assignment{{ItemID: "a", Day: "x"}} },
		"item id":      func(in *autopilot.Input) { in.Catalog = append(in.Catalog, autopilot.Item{}) },
		"weeknight":    func(in *autopilot.Input) { in.Preferences.Weeknights = []autopilot.Day{"x"} },
		"week number":  func(in *autopilot.Input) { in.Week = "2025-W53" },
		"context week": func(in *autopilot.Input) { in.Week = "" },
	} {
		in := input(meal("a"))
		mutate(&in)
		if _, err := p.GenerateWeek(ctx, autopilot.WeekRequest{Input: in}); !errors.Is(err, autopilot.ErrInvalidRequest) {
			t.Errorf("%s: GenerateWeek() error = %v", name, err)
		}
	}
	if _, err := p.RankMeals(ctx, autopilot.RankRequest{Input: input(meal("a")), Day: "someday"}); !errors.Is(err, autopilot.ErrInvalidRequest) {
		t.Errorf("RankMeals(bad day) error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.GenerateWeek(canceled, autopilot.WeekRequest{Input: input(meal("a"))}); !errors.Is(err, context.Canceled) {
		t.Errorf("GenerateWeek(canceled) error = %v", err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
