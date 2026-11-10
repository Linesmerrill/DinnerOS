package grocery

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func unit(t *testing.T, code string) ingredients.Unit {
	t.Helper()
	u, err := ingredients.LookupUnit(code)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// This week's meals come out of the pantry before next week's order counts
// on it (decision 662).
func TestPantryWithoutAssumedMeals(t *testing.T) {
	cup, tbsp, count := unit(t, "cup"), unit(t, "tbsp"), unit(t, "count")
	stock := PantryStock{
		InStock:   map[string]bool{"cream": true, "rice": true, "salt": true, "eggs": true, "beef": true},
		InFreezer: map[string]bool{"peas": true},
		Amounts: map[string]Amount{
			"cream": {Quantity: ingredients.NewQuantity(1, 1), Unit: cup},
			"rice":  {Quantity: ingredients.NewQuantity(2, 1), Unit: cup},
			"eggs":  {Quantity: ingredients.NewQuantity(6, 1), Unit: count},
			"peas":  {Quantity: ingredients.NewQuantity(1, 1), Unit: cup},
			"beef":  {Quantity: ingredients.NewQuantity(1, 1), Unit: count},
		},
	}
	got := stock.Without([]Item{
		{IngredientKey: "cream", Name: "Cream", Amounts: []Amount{{Quantity: ingredients.NewQuantity(4, 1), Unit: tbsp}}},
		{IngredientKey: "rice", Name: "Rice", Amounts: []Amount{{Quantity: ingredients.NewQuantity(3, 1), Unit: cup}}},
		{IngredientKey: "peas", Name: "Peas", Amounts: []Amount{{Quantity: ingredients.NewQuantity(1, 1), Unit: cup}}},
		{IngredientKey: "beef", Name: "Beef", Amounts: []Amount{{Quantity: ingredients.NewQuantity(8, 1), Unit: unit(t, "oz")}}},
		{IngredientKey: "salt", Name: "Salt", Unquantified: true},
		{IngredientKey: "garlic", Name: "Garlic", Amounts: []Amount{{Quantity: ingredients.NewQuantity(2, 1), Unit: count}}},
	})
	// 4 Tbsp is a quarter cup: three quarters left.
	if a := got.Amounts["cream"]; !a.Quantity.Equal(ingredients.NewQuantity(3, 4)) || a.Unit.Code != "cup" || !got.InStock["cream"] {
		t.Errorf("cream = %+v, in stock %v", a, got.InStock["cream"])
	}
	// Used up: no longer at home, so the list buys it in full.
	if _, ok := got.Amounts["rice"]; ok || got.InStock["rice"] {
		t.Errorf("rice still at home: %+v", got.Amounts["rice"])
	}
	if got.InFreezer["peas"] {
		t.Error("the frozen peas this week uses up are still in the freezer")
	}
	// Ounces of a count with nothing to convert by: unsure, not guessed.
	if !got.Uncertain["beef"] || !got.InStock["beef"] {
		t.Errorf("beef uncertain = %v, in stock %v", got.Uncertain["beef"], got.InStock["beef"])
	}
	// No amount, or no item: left alone.
	if !got.InStock["salt"] || !got.InStock["eggs"] || !got.Amounts["eggs"].Quantity.Equal(ingredients.NewQuantity(6, 1)) {
		t.Errorf("untouched items changed: %+v", got)
	}
	// The pantry itself is unchanged.
	if !stock.InStock["rice"] || !stock.Amounts["cream"].Quantity.Equal(ingredients.NewQuantity(1, 1)) || stock.Uncertain["beef"] {
		t.Errorf("Without changed the stock it was given: %+v", stock)
	}
	if same := stock.Without(nil); !same.InStock["rice"] {
		t.Error("Without(nil) changed the stock")
	}
}
