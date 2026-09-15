package pantry

import (
	"context"
	"fmt"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// UnresolvedKeyPrefix prefixes the grocery.Line.IngredientKey of a recipe line
// that has no catalog ingredient ID: "name:" + ingredients.NormalizeName(name).
// Lines with a catalog ID use the ID itself.
const UnresolvedKeyPrefix = "name:"

// GroceryPantry returns the household's pantry in the form grocery.Aggregate
// takes.
//
// Each item is registered under the keys a grocery line for that ingredient
// can carry: its catalog ingredient ID, and UnresolvedKeyPrefix + its key. An
// item added as free text before the catalog knew the ingredient is resolved
// against the catalog now, so later imports still match it.
//
// Status decides where an item goes. in_stock items are in InStock, so their
// lines are inPantry. out items are in OutOfStock, so their lines are toBuy
// even when the recipe source flags them as staples. low items are in neither,
// so they get the engine's default: pantryHint when every source flags them
// as staples, otherwise toBuy. Quantities are not compared with what the
// recipes need.
func (s *Service) GroceryPantry(ctx context.Context, householdID string) (grocery.PantryStock, error) {
	if householdID == "" {
		return grocery.PantryStock{}, errHouseholdRequired
	}
	items, err := s.store.ListItems(ctx, householdID, ListFilter{})
	if err != nil {
		return grocery.PantryStock{}, fmt.Errorf("list pantry items: %w", err)
	}

	var unlinked []string
	for _, item := range items {
		if item.IngredientID == "" && item.Status != StatusLow {
			unlinked = append(unlinked, item.Key)
		}
	}
	catalogIDs := map[string]string{}
	if len(unlinked) > 0 {
		found, err := s.catalog.IngredientsByKey(ctx, unlinked)
		if err != nil {
			return grocery.PantryStock{}, fmt.Errorf("resolve pantry items in the catalog: %w", err)
		}
		for _, ing := range found {
			catalogIDs[ing.Key] = ing.ID
		}
	}

	stock := grocery.PantryStock{InStock: map[string]bool{}, OutOfStock: map[string]bool{}}
	for _, item := range items {
		var set map[string]bool
		switch item.Status {
		case StatusInStock:
			set = stock.InStock
		case StatusOut:
			set = stock.OutOfStock
		default:
			continue
		}
		set[UnresolvedKeyPrefix+item.Key] = true
		if id := item.IngredientID; id != "" {
			set[id] = true
		} else if id := catalogIDs[item.Key]; id != "" {
			set[id] = true
		}
	}
	return stock, nil
}
