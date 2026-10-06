package pantry

import (
	"context"
	"fmt"
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
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
// against the catalog now, so later imports still match it. A default staple
// is registered under its aliases too, resolved the same way: the pantry's
// "Cooking Oil" answers for a recipe's "Vegetable Oil".
//
// Status decides where an item goes. in_stock items are in InStock, so their
// lines are inPantry. low and out items are in OutOfStock, so their lines are
// toBuy even when the recipe source flags them as staples: "running low" means
// buy more, and a member's status always beats the source's staple hint.
// Quantities are not compared with what the recipes need.
//
// An in-stock item in the freezer goes to InFreezer instead, so its line is
// fromFreezer: still on the list, so somebody remembers to take it out, but
// not bought again (freezer.go).
func (s *Service) GroceryPantry(ctx context.Context, householdID string) (grocery.PantryStock, error) {
	if householdID == "" {
		return grocery.PantryStock{}, errHouseholdRequired
	}
	items, err := s.store.ListItems(ctx, householdID, ListFilter{})
	if err != nil {
		return grocery.PantryStock{}, fmt.Errorf("list pantry items: %w", err)
	}

	var lookup []string
	for _, item := range items {
		for _, key := range pantryKeys(item) {
			// An item linked to the catalog already carries its own ID.
			if key != item.Key || item.IngredientID == "" {
				lookup = append(lookup, key)
			}
		}
	}
	catalogIDs := map[string]string{}
	if len(lookup) > 0 {
		found, err := s.catalog.IngredientsByKey(ctx, lookup)
		if err != nil {
			return grocery.PantryStock{}, fmt.Errorf("resolve pantry items in the catalog: %w", err)
		}
		for _, ing := range found {
			catalogIDs[ing.Key] = ing.ID
		}
	}

	stock := grocery.PantryStock{InStock: map[string]bool{}, OutOfStock: map[string]bool{}, InFreezer: map[string]bool{}}
	settings, err := s.Settings(ctx, householdID)
	if err != nil {
		return grocery.PantryStock{}, err
	}
	// How much of each in-stock ingredient is at home, summed across its
	// items; a key with any item of unknown amount has none, so the list
	// never calls it short on a guess.
	onHand := map[string]*grocery.Amount{}
	unknown := map[string]bool{}
	for _, item := range items {
		var set map[string]bool
		switch item.Status {
		case StatusInStock:
			if item.Storage == StorageFreezer {
				set = stock.InFreezer
			} else {
				set = stock.InStock
			}
		case StatusLow, StatusOut:
			set = stock.OutOfStock
		default:
			continue
		}
		keys := []string{}
		if id := item.IngredientID; id != "" {
			keys = append(keys, id)
		}
		for _, key := range pantryKeys(item) {
			keys = append(keys, UnresolvedKeyPrefix+key)
			if id := catalogIDs[key]; id != "" {
				keys = append(keys, id)
			}
		}
		amount, known := s.onHand(item, settings)
		for _, key := range keys {
			set[key] = true
			if item.Status != StatusInStock {
				continue
			}
			addOnHand(onHand, unknown, key, amount, known)
		}
	}
	stock.Amounts = map[string]grocery.Amount{}
	for key, a := range onHand {
		if !unknown[key] {
			stock.Amounts[key] = *a
		}
	}
	return stock, nil
}

// pantryKeys are the normalized names an item answers for: its own key, plus,
// when it is a default staple under any of that staple's names, all of them.
func pantryKeys(item Item) []string {
	name := ingredients.NormalizeName(item.DisplayName)
	for _, d := range defaultStaples {
		keys := d.keys()
		if slices.Contains(keys, item.Key) || slices.Contains(keys, name) {
			if !slices.Contains(keys, item.Key) {
				keys = append(keys, item.Key)
			}
			return keys
		}
	}
	return []string{item.Key}
}

// onHand is how much of an item is at home: the usage estimate when it's
// tracked, else the amount recorded, when that's a weight or volume.
func (s *Service) onHand(item Item, settings Settings) (grocery.Amount, bool) {
	quantity, unit := item.Quantity, item.Unit
	if est := s.Estimate(item, settings); est != nil && est.Remaining != nil {
		quantity, unit = est.Remaining.RatString(), est.Unit
	}
	if quantity == "" || unit == "" {
		return grocery.Amount{}, false
	}
	u, err := ingredients.LookupUnit(unit)
	if err != nil || u.Discrete() {
		return grocery.Amount{}, false
	}
	q, err := ingredients.ParseQuantity(quantity)
	if err != nil {
		return grocery.Amount{}, false
	}
	return grocery.Amount{Quantity: q, Unit: u}, true
}

// addOnHand adds one item's amount to a key's total; an unknown amount, or
// one that doesn't convert to the total's unit, makes the key unknown.
func addOnHand(totals map[string]*grocery.Amount, unknown map[string]bool, key string, a grocery.Amount, known bool) {
	if !known {
		unknown[key] = true
		return
	}
	total, ok := totals[key]
	if !ok {
		copied := a
		totals[key] = &copied
		return
	}
	converted, err := ingredients.Convert(a.Quantity, a.Unit, total.Unit)
	if err != nil {
		unknown[key] = true
		return
	}
	total.Quantity = total.Quantity.Add(converted)
}
