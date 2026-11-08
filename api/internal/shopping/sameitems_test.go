package shopping

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// The product picked for "Chipotle Powder" buys "Chipotle Chili Powder" once
// the household says they're the same, unless the kept name has its own.
func TestSameItemProductsUseTheOtherNamesProduct(t *testing.T) {
	merged := grocery.Item{
		IngredientKey: "ing-ccp", Name: "Chipotle Chili Powder",
		AlsoCalled: []grocery.AlsoCalled{{LinkID: "link-1", Key: "name:chipotle powder", Name: "Chipotle Powder"}},
	}
	g := planning.GroceryList{Categories: []planning.GroceryCategory{{Category: "spices", Items: []grocery.Item{merged}}}}

	byKey := map[string]Preference{"name:chipotle powder": {ID: "p-variant", ProductID: "100000001"}}
	sameItemProducts(byKey, g)
	if byKey["ing-ccp"].ProductID != "100000001" {
		t.Errorf("merged item's product = %+v; want the one saved for Chipotle Powder", byKey["ing-ccp"])
	}

	byKey = map[string]Preference{
		"name:chipotle powder": {ID: "p-variant", ProductID: "100000001"},
		"ing-ccp":              {ID: "p-kept", ProductID: "100000002"},
	}
	sameItemProducts(byKey, g)
	if byKey["ing-ccp"].ProductID != "100000002" {
		t.Errorf("merged item's product = %+v; want its own", byKey["ing-ccp"])
	}

	// Skipped items get it too, so putting one back shows its product.
	g = planning.GroceryList{SkippedItems: []grocery.Item{merged}}
	byKey = map[string]Preference{"name:chipotle powder": {ProductID: "100000001"}}
	sameItemProducts(byKey, g)
	if byKey["ing-ccp"].ProductID != "100000001" {
		t.Error("a skipped merged item didn't get the other name's product")
	}
}
