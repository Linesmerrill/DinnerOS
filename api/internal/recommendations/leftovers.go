package recommendations

import (
	"context"
	"errors"
	"fmt"
	"slices"

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
	day := openDay(plan, in.WeekStart)
	if day == "" {
		// Every day is taken, so there is nowhere to put a second meal. The
		// member can still plan one by hand; this just has nothing to add.
		return nil, nil
	}
	res, err := s.provider.RankMeals(ctx, autopilot.RankRequest{Input: in, Day: day, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("rank meals: %w", err)
	}
	out := make([]LeftoverPick, 0, len(res.Items))
	for _, item := range res.Items {
		r := data.byID[item.ItemID]
		pick := LeftoverPick{
			RecipeID: item.ItemID, RecipeName: r.Name, ImageURL: r.ImageURL, Day: string(day),
			Servings: item.Servings, CookMinutes: r.CookMinutes(), Score: item.Score,
		}
		for _, reason := range item.Reasons {
			pick.Reasons = append(pick.Reasons, Reason{Code: reason.Code, Text: reason.Text})
		}
		out = append(out, pick)
	}
	return out, nil
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

// openDay is the first day of the week with no meal planned, or "" when the
// week is full. Days run in the household's own order, so a Sunday-start
// household is offered Sunday first.
func openDay(plan planning.Plan, first autopilot.Day) autopilot.Day {
	taken := map[string]bool{}
	for _, e := range plan.Entries {
		taken[string(e.Day)] = true
	}
	for _, code := range planning.DaysFrom(planning.Day(first)) {
		if !taken[string(code)] {
			return autopilot.Day(code)
		}
	}
	return ""
}
