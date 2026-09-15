package baseline

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// Fixtures are made-up meals; nothing here comes from a real catalog.

const testWeek = "2026-W38"

type opt func(*autopilot.Item)

func meal(id string, opts ...opt) autopilot.Item {
	it := autopilot.Item{ID: id, Cuisines: []string{"cuisine-" + id}, Servings: []int{2, 4}, CookMinutes: 30}
	for _, o := range opts {
		o(&it)
	}
	return it
}

func cuisine(c ...string) opt   { return func(it *autopilot.Item) { it.Cuisines = c } }
func proteins(p ...string) opt  { return func(it *autopilot.Item) { it.Proteins = p } }
func tags(t ...string) opt      { return func(it *autopilot.Item) { it.Tags = t } }
func methods(m ...string) opt   { return func(it *autopilot.Item) { it.Methods = m } }
func minutes(n int) opt         { return func(it *autopilot.Item) { it.CookMinutes = n } }
func servings(s ...int) opt     { return func(it *autopilot.Item) { it.Servings = s } }
func allergens(a ...string) opt { return func(it *autopilot.Item) { it.Allergens = a } }
func diets(d ...string) opt     { return func(it *autopilot.Item) { it.Diets = d } }
func spicy() opt                { return func(it *autopilot.Item) { it.Spicy = true } }
func ingredients(names ...string) opt {
	return func(it *autopilot.Item) { it.Ingredients = names }
}

var weekdays = []autopilot.Day{autopilot.Monday, autopilot.Tuesday, autopilot.Wednesday, autopilot.Thursday, autopilot.Friday}

func input(catalog ...autopilot.Item) autopilot.Input {
	return autopilot.Input{
		HouseholdID: "household-1",
		Week:        testWeek,
		Catalog:     catalog,
		Preferences: autopilot.Preferences{
			PlanDays:        weekdays,
			Weeknights:      []autopilot.Day{},
			MealsPerWeek:    5,
			DefaultServings: 2,
			Novelty:         autopilot.NoveltyBalanced,
			CookTime:        autopilot.CookTimeMix{MaxLongPerWeek: -1},
		},
	}
}

func rate(itemID string, score int, tags ...string) autopilot.Rating {
	return autopilot.Rating{ItemID: itemID, MemberID: "member-1", Score: score, Tags: tags}
}

func generate(t *testing.T, p *Provider, in autopilot.Input) autopilot.WeekResult {
	t.Helper()
	res, err := p.GenerateWeek(context.Background(), autopilot.WeekRequest{Input: in, Attempt: 1})
	if err != nil {
		t.Fatalf("GenerateWeek() error = %v", err)
	}
	return res
}

func rank(t *testing.T, p *Provider, in autopilot.Input, day autopilot.Day, exclude ...string) autopilot.RankResult {
	t.Helper()
	res, err := p.RankMeals(context.Background(), autopilot.RankRequest{Input: in, Day: day, Exclude: exclude, Limit: autopilot.MaxRankLimit})
	if err != nil {
		t.Fatalf("RankMeals() error = %v", err)
	}
	return res
}

func slotOn(res autopilot.WeekResult, day autopilot.Day) *autopilot.Slot {
	for i := range res.Slots {
		if res.Slots[i].Day == day {
			return &res.Slots[i]
		}
	}
	return nil
}

func pickedIDs(res autopilot.WeekResult) []string {
	var ids []string
	for _, s := range res.Slots {
		ids = append(ids, s.ItemID)
	}
	return ids
}

func reasonTexts(reasons []autopilot.Reason) []string {
	var out []string
	for _, r := range reasons {
		out = append(out, r.Text)
	}
	return out
}

func hasReason(reasons []autopilot.Reason, text string) bool {
	return slices.ContainsFunc(reasons, func(r autopilot.Reason) bool { return r.Text == text })
}

func describe(res autopilot.WeekResult) string {
	var b strings.Builder
	for _, s := range res.Slots {
		fmt.Fprintf(&b, "%s=%s(%.3f %v) ", s.Day, s.ItemID, s.Score, reasonTexts(s.Reasons))
	}
	for _, m := range res.Messages {
		fmt.Fprintf(&b, "[%s: %s] ", m.Code, m.Text)
	}
	return b.String()
}
