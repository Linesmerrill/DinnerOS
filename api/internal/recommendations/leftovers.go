package recommendations

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// This file answers one question for the bulk-pack assistant
// (internal/shopping/bulkpack.go): the store's smallest pork loin is four
// pounds and Thursday uses ten ounces of it — what else could the household
// cook this week that uses the rest?
//
// It is deliberately not a new recommender. The candidates are the
// household's own recipes that call for the ingredient, and the ranking is
// Autopilot's: the same Input the planner builds, with the week's planned
// meals fixed so variety, cook-time balance and weekday rules still apply,
// and the catalog narrowed to the candidates. Whatever Autopilot learns
// about this household, a leftover suggestion learns too.

// MaxLeftoverPicks bounds how many second meals one ingredient suggests.
const MaxLeftoverPicks = 3

// LeftoverPick is a recipe worth planning later in the week because it uses
// an ingredient the week already bought too much of.
type LeftoverPick struct {
	RecipeID   string
	RecipeName string
	// ImageURL is the recipe's picture, or "".
	ImageURL string
	// Day is the open day Autopilot ranked it for ("thu").
	Day string
	// Servings is the authored serving size to cook.
	Servings int
	// CookMinutes is the recipe's total time, or 0 when unknown.
	CookMinutes int
	Score       float64
	// Reasons explain the pick in words, Autopilot's own.
	Reasons []Reason
}

// LeftoverPicks ranks the household's recipes that use an ingredient and
// aren't already in the week's plan, as meals for the week's first open day.
// The ingredient is named by its catalog ID, its name, or both, because a
// grocery line may carry either.
//
// It returns at most limit picks (MaxLeftoverPicks when limit is 0), best
// first, and no error when there simply aren't any: a bulk pack with nothing
// to pair it with is an ordinary outcome, not a failure.
func (s *Service) LeftoverPicks(
	ctx context.Context, householdID, week, ingredientID, ingredientName string, limit int,
) ([]LeftoverPick, error) {
	if householdID == "" {
		return nil, errors.New("recommendations: household id is required")
	}
	nameKey := ingredients.NormalizeName(ingredientName)
	if ingredientID == "" && nameKey == "" {
		return nil, invalidf("a leftover suggestion needs an ingredient")
	}
	if limit <= 0 || limit > MaxLeftoverPicks {
		limit = MaxLeftoverPicks
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return nil, err
	}
	plan, err := s.plans.Get(ctx, householdID, w.String())
	if err != nil {
		return nil, fmt.Errorf("load plan: %w", err)
	}
	in, data, err := s.prepareInput(ctx, householdID, w, plan, DeviceSignals{})
	if err != nil {
		return nil, err
	}
	planned := map[string]bool{}
	for _, e := range plan.Entries {
		planned[e.RecipeID] = true
	}
	// The input already carries the household's whole recipe catalog, so
	// which recipes use the ingredient is a question about data in hand.
	candidates := map[string]bool{}
	for id, r := range data.byID {
		if !planned[id] && usesIngredient(r, ingredientID, nameKey) {
			candidates[id] = true
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	// Narrow the catalog rather than rank everything and filter: the day's
	// hard constraints and the week's variety penalties then apply to the
	// candidates themselves, which is the whole point of reusing the ranker.
	in.Catalog = slices.DeleteFunc(slices.Clone(in.Catalog), func(item autopilot.Item) bool {
		return !candidates[item.ID]
	})
	if len(in.Catalog) == 0 {
		return nil, nil
	}
	loc := data.loc
	if loc == nil {
		loc = time.UTC
	}
	days := openDays(plan, in.WeekStart, s.now().In(loc).Format(time.DateOnly))
	if len(days) == 0 {
		// Every day left is taken, so there is nowhere to put a second meal.
		// The member can still plan one by hand; this just has nothing to add.
		return nil, nil
	}
	if len(in.Preferences.PlanDays) > 0 {
		// Only the days the household plans dinner on.
		days = slices.DeleteFunc(days, func(d autopilot.Day) bool { return !slices.Contains(in.Preferences.PlanDays, d) })
		if len(days) == 0 {
			return nil, nil
		}
	}
	// Autopilot scores every candidate for every open day, with the day's
	// weekday rules, cook-time limits, and the week's variety. Then each
	// recipe goes on the day it fits best: taco night's tacos land on taco
	// night, not on whichever open day happened to come first. Best pairs
	// first, one recipe per day while there are days to spare.
	ranked := make(map[autopilot.Day][]autopilot.Recommendation, len(days))
	for _, day := range days {
		res, err := s.provider.RankMeals(ctx, autopilot.RankRequest{Input: in, Day: day, Limit: autopilot.MaxRankLimit})
		if err != nil {
			return nil, fmt.Errorf("rank meals: %w", err)
		}
		ranked[day] = res.Items
	}
	out := make([]LeftoverPick, 0, limit)
	for _, p := range placePicks(days, ranked, limit) {
		out = append(out, leftoverPick(data.byID[p.item.ItemID], p.item, p.day))
	}
	return out, nil
}

type placedPick struct {
	day  autopilot.Day
	item autopilot.Recommendation
}

// placePicks puts each recipe on the open day it scores best on, best pairs
// first and one recipe per day while there are days to spare, so taco
// night's tacos land on taco night rather than on the first open day. The
// picks come back in the week's day order.
func placePicks(days []autopilot.Day, ranked map[autopilot.Day][]autopilot.Recommendation, limit int) []placedPick {
	var options []placedPick
	for _, day := range days {
		for _, item := range ranked[day] {
			options = append(options, placedPick{day: day, item: item})
		}
	}
	slices.SortStableFunc(options, func(a, b placedPick) int { return cmp.Compare(b.item.Score, a.item.Score) })
	var out []placedPick
	picked, used := map[string]bool{}, map[autopilot.Day]int{}
	for perDay := 1; len(out) < limit && perDay <= limit; perDay++ {
		for _, o := range options {
			if len(out) == limit {
				break
			}
			if picked[o.item.ItemID] || used[o.day] >= perDay {
				continue
			}
			picked[o.item.ItemID] = true
			used[o.day]++
			out = append(out, o)
		}
	}
	slices.SortStableFunc(out, func(a, b placedPick) int {
		return cmp.Compare(slices.Index(days, a.day), slices.Index(days, b.day))
	})
	return out
}

func leftoverPick(r recipes.Recipe, item autopilot.Recommendation, day autopilot.Day) LeftoverPick {
	pick := LeftoverPick{
		RecipeID: item.ItemID, RecipeName: r.Name, ImageURL: r.ImageURL, Day: string(day),
		Servings: item.Servings, CookMinutes: r.CookMinutes(), Score: item.Score,
	}
	for _, reason := range item.Reasons {
		pick.Reasons = append(pick.Reasons, Reason{Code: reason.Code, Text: reason.Text})
	}
	return pick
}

// usesIngredient reports whether r calls for the ingredient, by catalog ID
// when it has one and by normalized name otherwise — the same two keys a
// grocery line can carry.
func usesIngredient(r recipes.Recipe, ingredientID, nameKey string) bool {
	for _, ing := range r.Ingredients {
		if ingredientID != "" && ing.IngredientID == ingredientID {
			return true
		}
		if nameKey != "" && ingredients.NormalizeName(ing.Name) == nameKey {
			return true
		}
	}
	return false
}

// openDays are the week's days with no meal planned, from today on (a day
// already gone is no place for a second meal), in the household's own order.
// today is a date ("2026-09-27") in the household's time zone.
func openDays(plan planning.Plan, first autopilot.Day, today string) []autopilot.Day {
	taken := map[string]bool{}
	for _, e := range plan.Entries {
		taken[string(e.Day)] = true
	}
	var out []autopilot.Day
	for _, code := range planning.DaysFrom(planning.Day(first)) {
		if taken[string(code)] {
			continue
		}
		if date := plan.DateOf(code); date != "" && date < today {
			continue
		}
		out = append(out, autopilot.Day(code))
	}
	return out
}
