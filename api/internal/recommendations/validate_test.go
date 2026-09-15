package recommendations

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

func TestNormalizeProfile(t *testing.T) {
	p := DefaultProfile(hhA)
	p.Taste.Likes.Cuisines = []string{" Mexican ", "thai", "mexican"}
	p.Taste.Likes.Proteins = []string{"Pork", "chicken"}
	p.Restrictions.ExcludedIngredients = []string{"Cilantro", "  blue   cheese"}
	p.Equipment = []string{"grill", "smoker"}
	p.WeekdayRules = []WeekdayRule{
		{Day: "sun", Label: "  Sunday   smoker night ", Proteins: []string{"pork", "chicken"}, Methods: []string{"smoker"}, TimeBand: "long", Frequency: "at_most_once"},
		{Day: "tue", Cuisines: []string{"Mexican"}},
	}
	got, err := normalizeProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Taste.Likes.Cuisines, []string{"mexican", "thai"}) || !slices.Equal(got.Taste.Likes.Proteins, []string{"chicken", "pork"}) {
		t.Errorf("likes = %+v", got.Taste.Likes)
	}
	if !slices.Equal(got.Restrictions.ExcludedIngredients, []string{"blue cheese", "cilantro"}) || !slices.Equal(got.Equipment, []string{"smoker", "grill"}) {
		t.Errorf("restrictions %+v, equipment %v", got.Restrictions, got.Equipment)
	}
	rules := got.WeekdayRules
	if len(rules) != 2 || rules[0].Day != "tue" || rules[0].Label != "Tuesday" || rules[0].Frequency != "every_week" ||
		rules[1].Label != "Sunday smoker night" || !slices.Equal(rules[1].Proteins, []string{"chicken", "pork"}) {
		t.Errorf("rules = %+v", rules)
	}

	tooMany := make([]string, MaxListValues+1)
	for i := range tooMany {
		tooMany[i] = strings.Repeat("x", i+1)
	}
	tests := []struct {
		name    string
		mutate  func(p *Profile)
		wantMsg string
	}{
		{"unknown protein", func(p *Profile) { p.Taste.Likes.Proteins = []string{"unicorn"} }, "taste.likes.proteins"},
		{"liked and disliked", func(p *Profile) { p.Taste.Likes.Tags, p.Taste.Dislikes.Tags = []string{"pasta"}, []string{"Pasta"} }, "both liked and disliked"},
		{"liked and excluded", func(p *Profile) {
			p.Taste.Likes.Cuisines, p.Restrictions.ExcludedCuisines = []string{"thai"}, []string{"thai"}
		}, "liked and excluded"},
		{"too many cuisines", func(p *Profile) { p.Taste.Likes.Cuisines = tooMany }, "at most 30 values"},
		{"long value", func(p *Profile) { p.Taste.Likes.Tags = []string{strings.Repeat("y", MaxValueLength+1)} }, "longer than 40"},
		{"unknown diet", func(p *Profile) { p.Restrictions.Diets = []string{"keto"} }, "restrictions.diets"},
		{"unknown allergen", func(p *Profile) { p.Restrictions.Allergens = []string{"mustard"} }, "restrictions.allergens"},
		{"no plan days", func(p *Profile) { p.Schedule.PlanDays = nil }, "at least one day"},
		{"more meals than days", func(p *Profile) { p.Schedule.PlanDays, p.Schedule.MealsPerWeek = []string{"mon", "tue"}, 3 }, "number of planDays (2)"},
		{"servings", func(p *Profile) { p.Schedule.DefaultServings = 13 }, "defaultServings"},
		{"weeknight limit", func(p *Profile) { p.Schedule.WeeknightMaxMinutes = 3 }, "weeknightMaxMinutes"},
		{"bands out of order", func(p *Profile) { p.CookTime.MediumMaxMinutes = 20 }, "greater than quickMaxMinutes"},
		{"too many long meals", func(p *Profile) { p.CookTime.MaxLongPerWeek = 8 }, "maxLongPerWeek"},
		{"novelty", func(p *Profile) { p.Novelty = "wild" }, "novelty must be one of"},
		{"equipment", func(p *Profile) { p.Equipment = []string{"oven"} }, "equipment"},
		{"method without equipment", func(p *Profile) { p.WeekdayRules = []WeekdayRule{{Day: "sun", Methods: []string{"smoker"}}} }, "add smoker to equipment first"},
		{"two rules on a day", func(p *Profile) {
			p.WeekdayRules = []WeekdayRule{{Day: "fri", Tags: []string{"pizza"}}, {Day: "Fri", Tags: []string{"comfort"}}}
		}, "more than one rule for fri"},
		{"empty rule", func(p *Profile) { p.WeekdayRules = []WeekdayRule{{Day: "mon", Label: "Monday"}} }, "needs at least one"},
		{"rule day", func(p *Profile) { p.WeekdayRules = []WeekdayRule{{Day: "someday", Tags: []string{"x"}}} }, "day must be one of"},
		{"rule band", func(p *Profile) { p.WeekdayRules = []WeekdayRule{{Day: "mon", TimeBand: "forever"}} }, "timeBand"},
		{"rule frequency", func(p *Profile) { p.WeekdayRules = []WeekdayRule{{Day: "mon", TimeBand: "quick", Frequency: "daily"}} }, "frequency"},
		{"rule label", func(p *Profile) {
			p.WeekdayRules = []WeekdayRule{{Day: "mon", TimeBand: "quick", Label: strings.Repeat("z", 41)}}
		}, "label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := DefaultProfile(hhA)
			tt.mutate(&p)
			_, err := normalizeProfile(p)
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("normalizeProfile() error = %v, want %q", err, tt.wantMsg)
			}
		})
	}
}

func TestNormalizeWeekContext(t *testing.T) {
	got, err := normalizeWeekContext(WeekContext{
		Note: "  guests Friday ", Days: []DayOverride{{Day: "fri", Servings: 6}, {Day: "mon"}, {Day: "sat", Skip: true}},
	})
	if err != nil || got.Note != "guests Friday" || len(got.Days) != 2 || got.Days[0].Day != "fri" {
		t.Errorf("normalizeWeekContext() = %+v, %v; empty day overrides are dropped", got, err)
	}
	for name, c := range map[string]WeekContext{
		"meals":         {MealsPerWeek: 8},
		"max minutes":   {MaxMinutes: 2},
		"servings":      {Servings: 20},
		"day":           {Days: []DayOverride{{Day: "funday", Skip: true}}},
		"duplicate day": {Days: []DayOverride{{Day: "mon", Skip: true}, {Day: "mon", Servings: 2}}},
		"day minutes":   {Days: []DayOverride{{Day: "mon", MaxMinutes: 1000}}},
		"note":          {Note: strings.Repeat("n", MaxNoteLength+1)},
	} {
		if _, err := normalizeWeekContext(c); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

func TestDiffs(t *testing.T) {
	old := DefaultProfile(hhA)
	next := old
	next.Taste.Likes.Cuisines = []string{"thai"}
	next.Novelty = "adventurous"
	next.Equipment = []string{"smoker"}
	next.WeekdayRules = []WeekdayRule{{Day: "sun", Label: "Smoker night", Methods: []string{"smoker"}, TimeBand: "long", Frequency: "at_most_once"}}
	diffs := diffProfile(old, next)
	if len(diffs) != 4 {
		t.Fatalf("diffs = %+v", diffs)
	}
	if c := diffs[SectionTaste][0]; c.Field != "taste.likes.cuisines" || !slices.Equal(c.Added, []string{"thai"}) {
		t.Errorf("taste change = %+v", c)
	}
	if c := diffs[SectionNovelty][0]; c.From != "balanced" || c.To != "adventurous" {
		t.Errorf("novelty change = %+v", c)
	}
	if c := diffs[SectionWeekdayRules][0]; c.Field != "weekdayRules.sun" || c.To != "Smoker night · smoker · long · at_most_once" {
		t.Errorf("rule change = %+v", c)
	}
	if len(diffProfile(next, next)) != 0 {
		t.Error("an unchanged profile has diffs")
	}

	changes := diffWeekContext(WeekContext{}, WeekContext{Busy: true, MaxMinutes: 20, Days: []DayOverride{{Day: "fri", Servings: 6, MaxMinutes: 30}}})
	if len(changes) != 3 || changes[2].Field != "days.fri" || changes[2].To != "≤30 min, serves 6" {
		t.Errorf("context changes = %+v", changes)
	}
}

func TestProviderConversion(t *testing.T) {
	p := DefaultProfile(hhA)
	p.CookTime.MaxLongPerWeek = 7
	p.WeekdayRules = []WeekdayRule{{Day: "tue", Label: "Taco Tuesday", Cuisines: []string{"mexican"}, Frequency: "every_week"}}
	prefs := p.preferences(3)
	if prefs.DefaultServings != 3 || prefs.CookTime.MaxLongPerWeek != -1 || len(prefs.Rules) != 1 || prefs.Rules[0].Day != autopilot.Tuesday {
		t.Errorf("preferences = %+v", prefs)
	}
	p.Schedule.DefaultServings, p.Schedule.Weeknights = 5, nil
	if prefs := p.preferences(3); prefs.DefaultServings != 5 || prefs.Weeknights == nil {
		t.Errorf("an explicit servings value wins, and no weeknights stays empty: %+v", prefs)
	}
	c := WeekContext{Busy: true, MaxMinutes: 20, Days: []DayOverride{{Day: "wed", Skip: true}}}.providerContext([]string{"cilantro"})
	if !c.Busy || c.MaxMinutes != 20 || c.Days[0].Day != autopilot.Wednesday || c.PantryLow[0] != "cilantro" {
		t.Errorf("context = %+v", c)
	}
}
