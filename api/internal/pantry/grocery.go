package pantry

import (
	"context"
	"fmt"
	"math/big"
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
	// uncertain keys are at home in an amount that can't be trusted; byKey
	// is an item for each key, for converting a recipe's amount to it.
	uncertain := map[string]bool{}
	byKey := map[string]Item{}
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
		// A cooked use since the amount was set that couldn't be counted
		// means the estimate is too high by an unknown amount.
		trusted := known && (item.Tracking == nil || item.Tracking.SegmentSkippedUses == 0)
		for _, key := range keys {
			set[key] = true
			if item.Status != StatusInStock {
				continue
			}
			addOnHand(onHand, unknown, key, amount, known)
			if !trusted && !item.IsStaple {
				uncertain[key] = true
			}
			if _, ok := byKey[key]; !ok {
				byKey[key] = item
			}
		}
	}
	stock.Amounts = map[string]grocery.Amount{}
	for key, a := range onHand {
		if !unknown[key] {
			stock.Amounts[key] = *a
		}
	}
	stock.Uncertain = uncertain
	stock.Convert = func(key, name string, need grocery.Amount, to ingredients.Unit) (*big.Rat, bool) {
		item, ok := byKey[key]
		if !ok {
			return nil, false
		}
		return convertNeed(item, name, need.Quantity.Rat(), need.Unit.Code, to.Code)
	}
	return stock, nil
}

// pantryKeys are the normalized names an item answers for: its own key; when
// it is a default staple under any of that staple's names, all of them; every
// name the household said it also counts as (decision 641); and for each, the
// names that are the same ingredient ("onion" and "yellow onion", decision
// 640). Its own key comes first.
func pantryKeys(item Item) []string {
	keys := []string{item.Key}
	add := func(k string) {
		for _, same := range ingredients.SameIngredientKeys(k) {
			if same != "" && !slices.Contains(keys, same) {
				keys = append(keys, same)
			}
		}
	}
	add(item.Key)
	name := ingredients.NormalizeName(item.DisplayName)
	for _, d := range defaultStaples {
		staple := d.keys()
		if slices.Contains(staple, item.Key) || slices.Contains(staple, name) {
			for _, k := range staple {
				add(k)
			}
			break
		}
	}
	for _, n := range item.AlsoCountsAs {
		add(ingredients.NormalizeName(n))
	}
	return keys
}

// onHand is how much of an item is at home: the usage estimate when it's
// tracked, else the amount recorded.
func (s *Service) onHand(item Item, settings Settings) (grocery.Amount, bool) {
	quantity, unit := item.Quantity, item.Unit
	if est := s.Estimate(item, settings); est != nil && est.Remaining != nil {
		quantity, unit = est.Remaining.RatString(), est.Unit
	}
	if quantity == "" || unit == "" {
		return grocery.Amount{}, false
	}
	// A count is an amount too: 4 poblanos cover a recipe's 1, and a count
	// the recipe can't be compared with is caught by the list (Unsure).
	u, err := ingredients.LookupUnit(unit)
	if err != nil {
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

// convertNeed puts a recipe amount in unit to for item the way cooking
// deducts it (matchNeeds): exactly through the item's package size, else a
// meal kit's packet as what it holds ("1 tomato paste" is 2 Tbsp, "1 black
// beans" a 13.4 oz carton), else by the ingredient's typical density.
func convertNeed(item Item, name string, q *big.Rat, from, to string) (*big.Rat, bool) {
	if converted, ok := convertAmount(q, from, to, item.UnitSize); ok {
		return converted, true
	}
	if size, unit, found := ingredients.PacketSizeFor(name, from); found {
		q, from = new(big.Rat).Mul(q, size), unit
		if converted, ok := convertAmount(q, from, to, item.UnitSize); ok {
			return converted, true
		}
	}
	return estimateAmount(q, from, to, item)
}
