package planning

import (
	"slices"
	"sort"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// GroceryList is a week's aggregated grocery list.
type GroceryList struct {
	Week   Week
	Status Status
	// PantryApplied is true when the household pantry decided item statuses.
	PantryApplied bool
	// SpecialtiesApplied is true when the household's specialty ingredient
	// choices were applied.
	SpecialtiesApplied bool
	// Categories are in grocery.CategoryOrder; empty categories are omitted.
	Categories []GroceryCategory
	// Batches are the house-made specialty ingredients the week uses.
	Batches []grocery.BatchPlan
	// Skipped lists entries that could not contribute.
	Skipped []SkippedEntry
	// SkippedItems are the ingredients the household chose not to buy
	// (docs/grocery-engine.md#skipped-ingredients). They are not in
	// Categories, so nobody is asked to buy them, but they are reported
	// rather than dropped: the recipes still need them, and the list says so.
	//
	// This is a different thing from Skipped, which is about plan ENTRIES that
	// could not contribute at all.
	SkippedItems []grocery.Item
	// Meals are the week's planned recipes in plan order — scheduled days in
	// week order, then unscheduled — each once, so the list can be shown meal
	// by meal: every item's Shares name one of these.
	Meals []GroceryMeal
}

// GroceryMeal is one planned recipe the list can be grouped under.
type GroceryMeal struct {
	RecipeID   string
	RecipeName string
	ImageURL   string
	IsAddon    bool
	// Day is the first day it is planned on, empty when unscheduled.
	Day Day
}

// groceryMeals lists p's recipes once each, in plan order.
func groceryMeals(p Plan) []GroceryMeal {
	type ordered struct {
		meal  GroceryMeal
		order int
		index int
	}
	first := p.First()
	byRecipe := map[string]*ordered{}
	var list []*ordered
	for i, e := range p.Entries {
		order := len(dayOrder)
		if e.Day != "" {
			if o := e.Day.OrderOn(first); o >= 0 {
				order = o
			}
		}
		if o, ok := byRecipe[e.RecipeID]; ok {
			if order < o.order {
				o.order, o.meal.Day = order, e.Day
			}
			continue
		}
		o := &ordered{
			meal:  GroceryMeal{RecipeID: e.RecipeID, RecipeName: e.RecipeName, ImageURL: e.RecipeImageURL, IsAddon: e.RecipeIsAddon, Day: e.Day},
			order: order, index: i,
		}
		byRecipe[e.RecipeID] = o
		list = append(list, o)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].order != list[j].order {
			return list[i].order < list[j].order
		}
		return list[i].index < list[j].index
	})
	out := make([]GroceryMeal, 0, len(list))
	for _, o := range list {
		out = append(out, o.meal)
	}
	return out
}

// GroceryCategory is one aisle of the list.
type GroceryCategory struct {
	Category string
	Items    []grocery.Item
}

// SkipReason says why an entry is missing from the grocery list.
type SkipReason string

// Skip reasons.
const (
	// SkipRecipeUnavailable: the recipe is no longer in the household.
	SkipRecipeUnavailable SkipReason = "recipeUnavailable"
	// SkipServingsUnavailable: the recipe no longer offers the entry's serving
	// size (a re-import changed it). Amounts are never scaled from another size.
	SkipServingsUnavailable SkipReason = "servingsUnavailable"
)

// SkippedEntry is an entry left out of the grocery list.
type SkippedEntry struct {
	EntryID    string
	RecipeID   string
	RecipeName string
	Reason     SkipReason
}

// unnamedKeyPrefix keys ingredient lines that have no catalog ID.
const unnamedKeyPrefix = "name:"

// grocerySelections turns a plan and its live recipes into grocery
// selections. Entries whose recipe isn't in live, or no longer offers the
// entry's serving size, are skipped.
func grocerySelections(p Plan, live []recipes.Recipe) ([]grocery.RecipeSelection, []SkippedEntry) {
	byID := make(map[string]recipes.Recipe, len(live))
	for _, r := range live {
		byID[r.ID] = r
	}
	var selections []grocery.RecipeSelection
	var skipped []SkippedEntry
	for _, e := range p.Entries {
		r, ok := byID[e.RecipeID]
		switch {
		case !ok:
			skipped = append(skipped, SkippedEntry{EntryID: e.ID, RecipeID: e.RecipeID, RecipeName: e.RecipeName, Reason: SkipRecipeUnavailable})
			continue
		case !slices.Contains(r.Servings, e.Servings):
			skipped = append(skipped, SkippedEntry{EntryID: e.ID, RecipeID: e.RecipeID, RecipeName: r.Name, Reason: SkipServingsUnavailable})
			continue
		}
		// The authored amounts are for exactly this size, so no scaling.
		selections = append(selections, grocery.RecipeSelection{
			RecipeID: r.ID, RecipeName: r.Name,
			RecipeServings: e.Servings, TargetServings: e.Servings,
			Lines: groceryLines(r, e.Servings),
		})
	}
	return selections, skipped
}

// aggregateGroceryList aggregates selections and groups the items by
// category. Ingredients the household skips are held out of the categories and
// reported in SkippedItems instead.
func aggregateGroceryList(
	p Plan, selections []grocery.RecipeSelection, skipped []SkippedEntry, pantry grocery.Pantry, skips grocery.Skips,
) (GroceryList, error) {
	out := GroceryList{Week: p.Week, Status: p.Status, Skipped: skipped, Meals: groceryMeals(p)}
	list, err := grocery.AggregateWith(selections, pantry, skips)
	if err != nil {
		return GroceryList{}, err
	}
	out.SkippedItems = list.SkippedItems
	for _, item := range list.Items {
		n := len(out.Categories)
		if n == 0 || out.Categories[n-1].Category != item.Category {
			out.Categories = append(out.Categories, GroceryCategory{Category: item.Category})
			n++
		}
		out.Categories[n-1].Items = append(out.Categories[n-1].Items, item)
	}
	return out, nil
}

// groceryLines converts a recipe's ingredient lines for one serving size.
// A line without an authored amount for that size, or whose stored quantity
// or unit is unusable, stays on the list without a quantity rather than
// being dropped or guessed.
func groceryLines(r recipes.Recipe, servings int) []grocery.Line {
	lines := make([]grocery.Line, 0, len(r.Ingredients))
	for _, ing := range r.Ingredients {
		key := ing.IngredientID
		if key == "" {
			normalized := ingredients.NormalizeName(ing.Name)
			if normalized == "" {
				continue
			}
			key = unnamedKeyPrefix + ingredients.SameIngredientKey(normalized)
		}
		line := grocery.Line{IngredientKey: key, Name: ing.Name, Category: ing.Category, PantryStaple: ing.PantryStaple}
		for _, a := range ing.Amounts {
			if a.Servings != servings {
				continue
			}
			if q, ok := a.ExactQuantity(); ok && !q.IsZero() {
				if _, err := ingredients.LookupUnit(a.Unit); err == nil {
					line.Quantity, line.UnitCode = &q, a.Unit
				}
			}
			break
		}
		lines = append(lines, line)
	}
	return lines
}
