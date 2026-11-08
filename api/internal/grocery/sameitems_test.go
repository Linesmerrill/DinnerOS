package grocery

import (
	"math/big"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// chipotle links the recipe's "Chipotle Powder" (free text, or its catalog
// ID) to the household's "Chipotle Chili Powder".
var chipotle = SameItems{{
	LinkID: "link-1", VariantKeys: []string{"name:chipotle powder", "ing-cp"}, VariantName: "Chipotle Powder",
	KeptKey: "ing-ccp", KeptName: "Chipotle Chili Powder",
}}

func chipotleWeek() []RecipeSelection {
	return []RecipeSelection{
		{RecipeID: "r-tacos", RecipeName: "Tacos", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			line("name:chipotle powder", "Chipotle Powder", "spices", q(1, 1), "tsp"),
			line("name:onion", "Onion", "produce", q(1, 1), "count"),
		}},
		{RecipeID: "r-chili", RecipeName: "Chili", RecipeServings: 2, TargetServings: 2, Lines: []Line{
			line("ing-ccp", "Chipotle Chili Powder", "spices", q(2, 1), "tsp"),
		}},
	}
}

func TestSameItemsBuyTwoNamesAsOneItem(t *testing.T) {
	selections, also := chipotle.Apply(chipotleWeek())
	list, err := AggregateWith(selections, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("Items = %s; want chipotle once and the onion", render(list))
	}
	var item Item
	for _, it := range list.Items {
		if it.IngredientKey == "ing-ccp" {
			item = it
		}
	}
	if item.Name != "Chipotle Chili Powder" || amountString(item) != "3 tsp" || len(item.Sources) != 2 {
		t.Errorf("merged item = %s %s from %d recipes; want 3 tsp of Chipotle Chili Powder for both",
			item.Name, amountString(item), len(item.Sources))
	}
	got := also["ing-ccp"]
	if len(got) != 1 || got[0].Name != "Chipotle Powder" || got[0].LinkID != "link-1" || got[0].Key != "name:chipotle powder" {
		t.Errorf("alsoCalled = %+v; want Chipotle Powder with its link", got)
	}
	// The recipes themselves are untouched.
	if chipotleWeek()[0].Lines[0].Name != "Chipotle Powder" {
		t.Error("Apply changed its input")
	}
}

func TestSameItemsSayNothingWhenTheOtherNameIsntUsed(t *testing.T) {
	week := chipotleWeek()[1:]
	_, also := chipotle.Apply(week)
	if len(also) != 0 {
		t.Errorf("alsoCalled = %+v; want none when no meal says Chipotle Powder", also)
	}
}

func TestSameItemsLeaveSpecialtyLinesAlone(t *testing.T) {
	week := chipotleWeek()
	week[0].Lines[0].Specialty = &LineSpecialty{}
	selections, also := chipotle.Apply(week)
	if selections[0].Lines[0].IngredientKey != "name:chipotle powder" || len(also) != 0 {
		t.Errorf("a specialty line was renamed: %+v", selections[0].Lines[0])
	}
}

func TestSameItemsPantryAnswersForTheKeptName(t *testing.T) {
	tsp, err := ingredients.LookupUnit("tsp")
	if err != nil {
		t.Fatal(err)
	}
	stock := PantryStock{
		InStock: map[string]bool{"name:chipotle powder": true},
		Amounts: map[string]Amount{"name:chipotle powder": {Quantity: ingredients.NewQuantity(6, 1), Unit: tsp}},
		Convert: func(key, _ string, _ Amount, _ ingredients.Unit) (*big.Rat, bool) {
			return big.NewRat(1, 1), key == "name:chipotle powder"
		},
	}
	got := chipotle.Pantry(stock)
	if !got.Has("ing-ccp") {
		t.Error("the jar kept as Chipotle Powder doesn't count for Chipotle Chili Powder")
	}
	if a, ok := got.OnHand("ing-ccp"); !ok || a.Quantity.String() != "6" {
		t.Errorf("OnHand(kept) = %+v, %v; want the variant's 6 tsp", a, ok)
	}
	if _, ok := got.ConvertNeed("ing-ccp", "Chipotle Chili Powder", Amount{}, tsp); !ok {
		t.Error("ConvertNeed for the kept name doesn't use the variant's pantry item")
	}
	if stock.InStock["ing-ccp"] {
		t.Error("Pantry changed its input")
	}

	// The kept name's own pantry entry wins: it's out, so it's bought.
	stock.OutOfStock = map[string]bool{"ing-ccp": true}
	got = chipotle.Pantry(stock)
	if got.Has("ing-ccp") || !got.Out("ing-ccp") {
		t.Error("a pantry entry for the kept name was overridden by the variant's")
	}
}

func TestSameItemsSkipsCoverTheKeptName(t *testing.T) {
	rules := SkipRules{
		Ingredients: SkipSet{"name:chipotle powder": SkipThisWeek},
		Recipes:     map[string]map[string]bool{"r-tacos": {"ing-cp": true}},
	}
	got := chipotle.Skips(rules)
	if scope, ok := got.Skip("ing-ccp"); !ok || scope != SkipThisWeek {
		t.Errorf("Skip(kept) = %q, %v; want the variant's week skip", scope, ok)
	}
	if !got.SkipInRecipe("r-tacos", "ing-ccp") {
		t.Error("leaving Chipotle Powder out of Tacos doesn't leave the merged line out of Tacos")
	}
	if _, ok := rules.Ingredients["ing-ccp"]; ok {
		t.Error("Skips changed its input")
	}

	// A stronger skip of the kept name itself stays.
	rules.Ingredients["ing-ccp"] = SkipAlways
	if scope, _ := chipotle.Skips(rules).Skip("ing-ccp"); scope != SkipAlways {
		t.Errorf("Skip(kept) = %q; want its own always skip to outrank the variant's week skip", scope)
	}

	// End to end: the merged chipotle is held back, the onion isn't.
	selections, _ := chipotle.Apply(chipotleWeek())
	list, err := AggregateWith(selections, nil, chipotle.Skips(SkipRules{Ingredients: SkipSet{"ing-cp": SkipAlways}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.SkippedItems) != 1 || list.SkippedItems[0].IngredientKey != "ing-ccp" || len(list.Items) != 1 {
		t.Errorf("list = %s, skipped %d; want the merged chipotle skipped", render(list), len(list.SkippedItems))
	}
}

func TestNoSameItemsChangeNothing(t *testing.T) {
	var none SameItems
	week := chipotleWeek()
	if got, also := none.Apply(week); &got[0] != &week[0] || also != nil {
		t.Error("Apply without links should hand back its input")
	}
	stock := PantryStock{InStock: map[string]bool{"x": true}}
	if !none.Pantry(stock).Has("x") {
		t.Error("Pantry without links lost the stock")
	}
}
