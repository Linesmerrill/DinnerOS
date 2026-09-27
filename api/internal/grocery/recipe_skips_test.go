package grocery

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Two meals need cilantro and onion; only the curry leaves cilantro out.
func sharedCilantroWeek() []RecipeSelection {
	return []RecipeSelection{
		{
			RecipeID: "curry", RecipeName: "Thai Coconut Curry Chicken", RecipeServings: 2, TargetServings: 2,
			Lines: []Line{
				line("name:cilantro", "Cilantro", "produce", q(1, 4), "oz"),
				line("name:onion", "Yellow Onion", "produce", q(1, 1), "count"),
			},
		},
		{
			RecipeID: "tacos", RecipeName: "Pork Tacos", RecipeServings: 2, TargetServings: 2,
			Lines: []Line{line("name:cilantro", "Cilantro", "produce", q(1, 2), "oz")},
		},
	}
}

func skippedItem(t *testing.T, l List, key string, scope SkipScope) Item {
	t.Helper()
	for _, it := range l.SkippedItems {
		if it.IngredientKey == key && it.SkipScope == scope {
			return it
		}
	}
	t.Fatalf("no skipped %q with scope %q in %+v", key, scope, l.SkippedItems)
	return Item{}
}

// The heart of a per-dish skip: the list line shrinks to the other recipe's
// amount instead of vanishing, and the left-out share is reported, crossed out,
// for the recipe that left it out.
func TestRecipeSkipShrinksASharedIngredientToTheOtherRecipesAmount(t *testing.T) {
	rules := SkipRules{Recipes: map[string]map[string]bool{"curry": {"name:cilantro": true}}}
	list, err := AggregateWith(sharedCilantroWeek(), nil, rules)
	if err != nil {
		t.Fatalf("AggregateWith() error = %v", err)
	}
	cilantro := find(t, list, "name:cilantro")
	if got := amountString(cilantro); got != "1/2 oz" {
		t.Errorf("cilantro on the list = %q, want only the tacos' 1/2 oz", got)
	}
	if len(cilantro.Sources) != 1 || cilantro.Sources[0].RecipeID != "tacos" {
		t.Errorf("cilantro sources = %+v, want only the tacos", cilantro.Sources)
	}
	if cilantro.Status != StatusToBuy {
		t.Errorf("cilantro status = %q, want toBuy", cilantro.Status)
	}
	left := skippedItem(t, list, "name:cilantro", SkipRecipe)
	if got := amountString(left); got != "1/4 oz" {
		t.Errorf("left-out cilantro = %q, want the curry's 1/4 oz", got)
	}
	if left.Status != StatusSkipped || len(left.Sources) != 1 || left.Sources[0].RecipeID != "curry" {
		t.Errorf("left-out cilantro = %+v, want skipped, for the curry", left)
	}
	// The curry's onion is untouched.
	if got := amountString(find(t, list, "name:onion")); got != "1 count" {
		t.Errorf("onion = %q, want 1 count", got)
	}
}

func TestRecipeSkipOfTheOnlyRecipeLeavesNothingToBuy(t *testing.T) {
	rules := SkipRules{Recipes: map[string]map[string]bool{"curry": {"name:onion": true}}}
	list, err := AggregateWith(sharedCilantroWeek(), nil, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range list.Items {
		if it.IngredientKey == "name:onion" {
			t.Fatalf("onion is on the list although its only recipe leaves it out: %s", render(list))
		}
	}
	skippedItem(t, list, "name:onion", SkipRecipe)
}

// Always beats week, and an ingredient-wide skip beats a recipe skip: the item
// is held back whole, under the ingredient-wide scope, and not split.
func TestSkipScopePrecedence(t *testing.T) {
	if !SkipAlways.Outranks(SkipThisWeek) || !SkipThisWeek.Outranks(SkipRecipe) || !SkipRecipe.Outranks("") {
		t.Fatal("want always > week > recipe > none")
	}
	if SkipRecipe.Outranks(SkipAlways) || SkipThisWeek.Outranks(SkipAlways) {
		t.Fatal("nothing outranks always")
	}
	for _, scope := range []SkipScope{SkipAlways, SkipThisWeek} {
		rules := SkipRules{
			Ingredients: SkipSet{"name:cilantro": scope},
			Recipes:     map[string]map[string]bool{"curry": {"name:cilantro": true}},
		}
		list, err := AggregateWith(sharedCilantroWeek(), nil, rules)
		if err != nil {
			t.Fatal(err)
		}
		var cilantro []Item
		for _, it := range list.SkippedItems {
			if it.IngredientKey == "name:cilantro" {
				cilantro = append(cilantro, it)
			}
		}
		if len(cilantro) != 1 || cilantro[0].SkipScope != scope || amountString(cilantro[0]) != "3/4 oz" {
			t.Errorf("%s + recipe: skipped cilantro = %+v, want one item, %s, all 3/4 oz", scope, cilantro, scope)
		}
		for _, it := range list.Items {
			if it.IngredientKey == "name:cilantro" {
				t.Errorf("%s: cilantro still on the list", scope)
			}
		}
	}
}

// A plain SkipSet has no recipe skips, and a recipe skip for a recipe that
// isn't planned changes nothing.
func TestRecipeSkipForAnotherRecipeChangesNothing(t *testing.T) {
	rules := SkipRules{Recipes: map[string]map[string]bool{"lasagna": {"name:cilantro": true}}}
	list, err := AggregateWith(sharedCilantroWeek(), nil, rules)
	if err != nil {
		t.Fatal(err)
	}
	plain := mustAggregate(t, sharedCilantroWeek(), nil)
	if render(plain) != render(list) || len(list.SkippedItems) != 0 {
		t.Errorf("unrelated recipe skip changed the list:\n%s\n%s", render(plain), render(list))
	}
}

func TestSharesSplitAnItemByMeal(t *testing.T) {
	list := mustAggregate(t, sharedCilantroWeek(), nil)
	cilantro := find(t, list, "name:cilantro")
	if len(cilantro.Shares) != 2 {
		t.Fatalf("shares = %+v, want one per meal", cilantro.Shares)
	}
	want := map[string]string{"curry": "1/4 oz", "tacos": "1/2 oz"}
	for _, sh := range cilantro.Shares {
		got := amountString(Item{Amounts: sh.Amounts, Unquantified: sh.Unquantified})
		if got != want[sh.RecipeID] {
			t.Errorf("share for %s = %q, want %q", sh.RecipeID, got, want[sh.RecipeID])
		}
	}
	// In Sources order.
	if cilantro.Shares[0].RecipeID != "tacos" || cilantro.Shares[1].RecipeID != "curry" {
		t.Errorf("share order = %s, %s; want Sources' order (by name)", cilantro.Shares[0].RecipeID, cilantro.Shares[1].RecipeID)
	}
}

// crema is a specialty ingredient whose store alternative is sour cream,
// roasted red peppers, and smoked paprika: a component of the meal.
func cremaSpecialty() *Specialty {
	return &Specialty{
		ID: "smoky-red-pepper-crema", Key: "smoky red pepper crema", Name: "Smoky Red Pepper Crema",
		Choice: &Choice{
			Type: ChoiceStoreAlternative, OptionID: "smoky-red-pepper-crema.store", OptionName: "Sour cream with roasted peppers",
			Per: Measure{Quantity: ingredients.NewQuantity(1, 1), Unit: "tbsp"},
			Components: []Component{
				{IngredientKey: "name:sour cream", Name: "Sour Cream", Category: "dairy-eggs", Quantity: q(2, 1), Unit: "tsp"},
				{IngredientKey: "name:roasted red peppers", Name: "Roasted Red Peppers", Category: "pantry", Quantity: q(1, 1), Unit: "tsp"},
				{IngredientKey: "name:smoked paprika", Name: "Smoked Paprika", Category: "spices", Quantity: q(1, 8), Unit: "tsp"},
			},
		},
	}
}

// The tacos use the crema and a dollop of sour cream of their own; the bowls
// use sour cream too.
func cremaWeek(t *testing.T) []RecipeSelection {
	t.Helper()
	selections := []RecipeSelection{
		{
			RecipeID: "tacos", RecipeName: "Smoky Pork Tacos", RecipeServings: 2, TargetServings: 2,
			Lines: []Line{
				line("cat-crema", "Smoky Red Pepper Crema", "condiments", q(2, 1), "tbsp"),
				line("name:sour cream", "Sour Cream", "dairy-eggs", q(1, 1), "tbsp"),
			},
		},
		{
			RecipeID: "bowls", RecipeName: "Chili Bowls", RecipeServings: 2, TargetServings: 2,
			Lines: []Line{line("name:sour cream", "Sour Cream", "dairy-eggs", q(1, 1), "tbsp")},
		},
	}
	applied, _, err := ApplySpecialties(selections, Specialties{"cat-crema": cremaSpecialty()})
	if err != nil {
		t.Fatalf("ApplySpecialties() error = %v", err)
	}
	return applied
}

func TestSharesKeepAComponentApartFromTheMealsOwnUse(t *testing.T) {
	list := mustAggregate(t, cremaWeek(t), nil)
	sourCream := find(t, list, "name:sour cream")
	var own, crema, bowls bool
	for _, sh := range sourCream.Shares {
		amount := amountString(Item{Amounts: sh.Amounts})
		switch {
		case sh.RecipeID == "tacos" && sh.Component == nil:
			own = amount == "1 tbsp"
		case sh.RecipeID == "tacos" && sh.Component != nil:
			crema = sh.Component.SpecialtyName == "Smoky Red Pepper Crema" && sh.Component.LineKey == "cat-crema" && amount == "4 tsp"
		case sh.RecipeID == "bowls":
			bowls = amount == "1 tbsp"
		}
	}
	if !own || !crema || !bowls {
		t.Errorf("sour cream shares = %+v; want the tacos' own 1 tbsp, the crema's 4 tsp, and the bowls' 1 tbsp", sourCream.Shares)
	}
}

// "We don't make the crema": leaving the component out of the tacos removes
// every ingredient it became, and only its share — the tacos' own sour cream
// and the bowls' stay on the list.
func TestLeavingAComponentOutOfARecipeRemovesOnlyItsShare(t *testing.T) {
	rules := SkipRules{Recipes: map[string]map[string]bool{"tacos": {"cat-crema": true}}}
	list, err := AggregateWith(cremaWeek(t), nil, rules)
	if err != nil {
		t.Fatal(err)
	}
	if got := amountString(find(t, list, "name:sour cream")); got != "2 tbsp" {
		t.Errorf("sour cream = %q, want 2 tbsp (the tacos' own and the bowls'), without the crema's 4 tsp", got)
	}
	for _, key := range []string{"name:roasted red peppers", "name:smoked paprika"} {
		for _, it := range list.Items {
			if it.IngredientKey == key {
				t.Errorf("%s is on the list although the crema is left out", key)
			}
		}
		left := skippedItem(t, list, key, SkipRecipe)
		if len(left.Via) != 1 || left.Via[0].SpecialtyName != "Smoky Red Pepper Crema" {
			t.Errorf("left-out %s Via = %+v, want the crema", key, left.Via)
		}
	}
	left := skippedItem(t, list, "name:sour cream", SkipRecipe)
	if amountString(left) != "4 tsp" {
		t.Errorf("left-out sour cream = %q, want the crema's 4 tsp", amountString(left))
	}
}

// Skipping the crema itself for good also stops buying what it became, and the
// name spelling of the skip matches the catalog line.
func TestSkippingAComponentEverywhereLeavesOutItsIngredients(t *testing.T) {
	rules := SkipRules{Ingredients: SkipSet{"name:smoky red pepper crema": SkipAlways}}
	list, err := AggregateWith(cremaWeek(t), nil, rules)
	if err != nil {
		t.Fatal(err)
	}
	skippedItem(t, list, "name:roasted red peppers", SkipAlways)
	if got := amountString(find(t, list, "name:sour cream")); got != "2 tbsp" {
		t.Errorf("sour cream = %q, want 2 tbsp", got)
	}
}

// A batch made for two meals stays while either still wants it.
func TestRecipeSkipOfABatchComponentNeedsEveryMeal(t *testing.T) {
	week := batchWeek()
	applied, _, err := ApplySpecialties(week, Specialties{keySouthwest: southwestSpecialty(nil)})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, sel := range week {
		ids = append(ids, sel.RecipeID)
	}
	one := SkipRules{Recipes: map[string]map[string]bool{ids[0]: {"name:southwest spice blend": true}}}
	list, err := AggregateWith(applied, nil, one)
	if err != nil {
		t.Fatal(err)
	}
	find(t, list, keyCumin)

	all := SkipRules{Recipes: map[string]map[string]bool{}}
	for _, id := range ids {
		all.Recipes[id] = map[string]bool{"name:southwest spice blend": true}
	}
	list, err = AggregateWith(applied, nil, all)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range list.Items {
		if it.IngredientKey == keyCumin {
			t.Errorf("cumin for the batch is on the list although every meal leaves the blend out")
		}
	}
	skippedItem(t, list, keyCumin, SkipRecipe)
}

func TestRecipeSkipsAreOrderIndependent(t *testing.T) {
	rules := SkipRules{Recipes: map[string]map[string]bool{"curry": {"name:cilantro": true}}}
	week := sharedCilantroWeek()
	a, err := AggregateWith(week, nil, rules)
	if err != nil {
		t.Fatal(err)
	}
	reversed := []RecipeSelection{week[1], week[0]}
	b, err := AggregateWith(reversed, nil, rules)
	if err != nil {
		t.Fatal(err)
	}
	if render(a) != render(b) || render(List{Items: a.SkippedItems}) != render(List{Items: b.SkippedItems}) {
		t.Errorf("order changed the result:\n%s\n%s", render(a), render(b))
	}
}
