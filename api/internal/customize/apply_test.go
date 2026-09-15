package customize

import (
	"reflect"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func qty(s string) *ingredients.Quantity {
	q, err := ingredients.ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return &q
}

func pick(t *testing.T, name, key, choiceID string, target Target) Pick {
	t.Helper()
	table := DefaultTable()
	p, ok := table.Match(name)
	if !ok {
		t.Fatalf("no protein %q", name)
	}
	c, ok := table.Resolve(p, name, choiceID)
	if !ok {
		t.Fatalf("no choice %q for %q", choiceID, name)
	}
	return Pick{IngredientKey: key, Choice: c, Target: target}
}

var beefTarget = Target{IngredientID: ingBeef, Name: "Ground Beef", Category: "meat-seafood"}

func tacosSelection() grocery.RecipeSelection {
	return grocery.RecipeSelection{
		RecipeID: "r-tacos", RecipeName: "One-Pan Pork Tacos", RecipeServings: 2, TargetServings: 2,
		Lines: []grocery.Line{
			{IngredientKey: ingPork, Name: "Ground Pork", Category: "meat-seafood", Quantity: qty("10"), UnitCode: "oz", PantryStaple: true},
			{IngredientKey: ingOnion, Name: "Yellow Onion", Category: "produce", Quantity: qty("1"), UnitCode: "count"},
			{IngredientKey: "name:tex mex paste", Name: "Tex-Mex Paste", Category: "condiments", Quantity: qty("2"), UnitCode: "tbsp"},
		},
	}
}

func TestApplyGrocery(t *testing.T) {
	sel := tacosSelection()
	before := tacosSelection()
	for _, tc := range []struct {
		name     string
		pick     Pick
		wantKey  string
		wantName string
		wantQty  string
		wantVia  *grocery.Via
	}{
		{"original", pick(t, "Ground Pork", ingPork, "original", Target{}), ingPork, "Ground Pork", "10", nil},
		{"double", pick(t, "Ground Pork", ingPork, "double", Target{}), ingPork, "Ground Pork", "20",
			&grocery.Via{Kind: grocery.ViaCustomized, SpecialtyKey: ingPork, SpecialtyName: "Ground Pork", OptionID: "double", OptionName: "2x Ground Pork"}},
		{"swap", pick(t, "Ground Pork", ingPork, "swap:ground-beef", beefTarget), ingBeef, "Ground Beef", "10",
			&grocery.Via{Kind: grocery.ViaCustomized, SpecialtyKey: ingPork, SpecialtyName: "Ground Pork", OptionID: "swap:ground-beef", OptionName: "Ground Beef"}},
		{"swap double without a catalog ingredient", pick(t, "Ground Pork", ingPork, "swap:ground-turkey:double", Target{Name: "Ground Turkey", Category: "meat-seafood"}),
			"name:ground turkey", "Ground Turkey", "20",
			&grocery.Via{Kind: grocery.ViaCustomized, SpecialtyKey: ingPork, SpecialtyName: "Ground Pork", OptionID: "swap:ground-turkey:double", OptionName: "2x Ground Turkey"}},
	} {
		got := ApplyGrocery(sel, []Pick{tc.pick})
		if len(got.Lines) != 3 || got.RecipeID != "r-tacos" {
			t.Fatalf("%s: lines = %+v", tc.name, got.Lines)
		}
		l := got.Lines[0]
		if l.IngredientKey != tc.wantKey || l.Name != tc.wantName || l.Quantity.String() != tc.wantQty || l.UnitCode != "oz" || !reflect.DeepEqual(l.Via, tc.wantVia) {
			t.Errorf("%s: line = %+v (via %+v)", tc.name, l, l.Via)
		}
		if tc.pick.Choice.IsSwap() && l.PantryStaple {
			t.Errorf("%s: a swapped line kept the staple hint", tc.name)
		}
		if !reflect.DeepEqual(got.Lines[1:], sel.Lines[1:]) {
			t.Errorf("%s: other lines changed: %+v", tc.name, got.Lines[1:])
		}
	}
	if !reflect.DeepEqual(sel, before) {
		t.Errorf("ApplyGrocery changed its input: %+v", sel)
	}

	// A line without an amount stays without one; an unknown key changes nothing.
	sel.Lines[0].Quantity, sel.Lines[0].UnitCode = nil, ""
	got := ApplyGrocery(sel, []Pick{pick(t, "Ground Pork", ingPork, "swap:ground-beef:double", beefTarget), pick(t, "Ground Beef", "missing", "double", Target{})})
	if l := got.Lines[0]; l.Quantity != nil || l.IngredientKey != ingBeef || l.Via == nil {
		t.Errorf("unquantified swap = %+v", l)
	}
}

// TestApplyGroceryComposesWithSpecialtiesAndAggregation runs the planner's
// pipeline: customizations, then specialty ingredients, then Aggregate.
func TestApplyGroceryComposesWithSpecialtiesAndAggregation(t *testing.T) {
	chili := grocery.RecipeSelection{
		RecipeID: "r-chili", RecipeName: "Beef Chili", RecipeServings: 2, TargetServings: 2,
		Lines: []grocery.Line{{IngredientKey: ingBeef, Name: "Ground Beef", Category: "meat-seafood", Quantity: qty("10"), UnitCode: "oz"}},
	}
	tacos := ApplyGrocery(tacosSelection(), []Pick{pick(t, "Ground Pork", ingPork, "swap:ground-beef", beefTarget)})
	specs := grocery.Specialties{
		"name:tex mex paste": {
			ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste",
			Choice: &grocery.Choice{
				Type: grocery.ChoiceStoreAlternative, OptionID: "tex-mex-paste.store", OptionName: "Tomato paste and chili spices",
				Per:        grocery.Measure{Quantity: *qty("1"), Unit: "tbsp"},
				Components: []grocery.Component{{IngredientKey: "name:tomato paste", Name: "Tomato Paste", Category: "condiments", Quantity: qty("1"), Unit: "tbsp"}},
			},
		},
	}
	selections, _, err := grocery.ApplySpecialties([]grocery.RecipeSelection{tacos, chili}, specs)
	if err != nil {
		t.Fatal(err)
	}
	list, err := grocery.Aggregate(selections, grocery.PantrySet{})
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]grocery.Item{}
	for _, it := range list.Items {
		items[it.Name] = it
	}
	if _, ok := items["Ground Pork"]; ok {
		t.Error("the swapped-out pork is still on the list")
	}
	beef := items["Ground Beef"]
	if len(beef.Amounts) != 1 || beef.Amounts[0].Quantity.String() != "20" || beef.Amounts[0].Unit.Code != "oz" || len(beef.Sources) != 2 {
		t.Errorf("beef = %+v", beef)
	}
	if len(beef.Via) != 1 || beef.Via[0].Kind != grocery.ViaCustomized || beef.Via[0].SpecialtyName != "Ground Pork" ||
		len(beef.Via[0].Recipes) != 1 || beef.Via[0].Recipes[0].RecipeName != "One-Pan Pork Tacos" {
		t.Errorf("beef via = %+v", beef.Via)
	}
	paste := items["Tomato Paste"]
	if len(paste.Via) != 1 || paste.Via[0].Kind != grocery.ViaStoreAlternative || paste.Amounts[0].Quantity.String() != "2" {
		t.Errorf("tomato paste = %+v", paste)
	}
	if _, ok := items["Yellow Onion"]; !ok {
		t.Error("onion missing")
	}
}

func TestApplyRecipe(t *testing.T) {
	r := tacosRecipe()
	before := tacosRecipe()
	chicken := Target{IngredientID: "66e5a1f2c3b4a5d6e7f8a009", Name: "Chopped Chicken Breast", Category: "meat-seafood"}
	got := ApplyRecipe(r, []Pick{pick(t, "Ground Pork", ingPork, "swap:chopped-chicken-breast:double", chicken)})
	first := got.Ingredients[0]
	if first.IngredientID != chicken.IngredientID || first.Name != "Chopped Chicken Breast" || len(first.Amounts) != 2 ||
		first.Amounts[0].Quantity != "20" || first.Amounts[1].Quantity != "40" || first.Amounts[0].Unit != "oz" {
		t.Errorf("customized line = %+v", first)
	}
	// Every line with the key changes; other lines don't.
	if last := got.Ingredients[len(got.Ingredients)-1]; last.Name != "Chopped Chicken Breast" || last.Amounts[0].Quantity != "8" {
		t.Errorf("repeated line = %+v", last)
	}
	if !reflect.DeepEqual(got.Ingredients[1:6], r.Ingredients[1:6]) {
		t.Errorf("other lines changed")
	}
	if !reflect.DeepEqual(r, before) {
		t.Error("ApplyRecipe changed its input")
	}
	doubled := ApplyRecipe(r, []Pick{pick(t, "Ground Pork", ingPork, "double", Target{})})
	if l := doubled.Ingredients[0]; l.IngredientID != ingPork || l.Amounts[1].Quantity != "40" {
		t.Errorf("doubled line = %+v", l)
	}
	unquantified := recipes.Recipe{Ingredients: []recipes.RecipeIngredient{{Name: "Shrimp", Amounts: []recipes.Amount{{Servings: 2}}}}}
	if got := ApplyRecipe(unquantified, []Pick{pick(t, "Shrimp", "name:shrimp", "double", Target{})}); got.Ingredients[0].Amounts[0].Quantity != "" {
		t.Errorf("unquantified = %+v", got.Ingredients[0])
	}
}
