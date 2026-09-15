package planning

import (
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// GroceryList is a week's aggregated grocery list.
type GroceryList struct {
	Week   Week
	Status Status
	// Categories are in grocery.CategoryOrder; empty categories are omitted.
	Categories []GroceryCategory
	// Skipped lists entries that could not contribute.
	Skipped []SkippedEntry
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

// buildGroceryList turns a plan and its live recipes into grocery selections
// and aggregates them. Recipes not in live are skipped.
func buildGroceryList(p Plan, live []recipes.Recipe, pantry grocery.Pantry) (GroceryList, error) {
	byID := make(map[string]recipes.Recipe, len(live))
	for _, r := range live {
		byID[r.ID] = r
	}
	out := GroceryList{Week: p.Week, Status: p.Status}
	var selections []grocery.RecipeSelection
	for _, e := range p.Entries {
		r, ok := byID[e.RecipeID]
		switch {
		case !ok:
			out.Skipped = append(out.Skipped, SkippedEntry{EntryID: e.ID, RecipeID: e.RecipeID, RecipeName: e.RecipeName, Reason: SkipRecipeUnavailable})
			continue
		case !slices.Contains(r.Servings, e.Servings):
			out.Skipped = append(out.Skipped, SkippedEntry{EntryID: e.ID, RecipeID: e.RecipeID, RecipeName: r.Name, Reason: SkipServingsUnavailable})
			continue
		}
		// The authored amounts are for exactly this size, so no scaling.
		selections = append(selections, grocery.RecipeSelection{
			RecipeID: r.ID, RecipeName: r.Name,
			RecipeServings: e.Servings, TargetServings: e.Servings,
			Lines: groceryLines(r, e.Servings),
		})
	}
	list, err := grocery.Aggregate(selections, pantry)
	if err != nil {
		return GroceryList{}, err
	}
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
			key = unnamedKeyPrefix + normalized
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
