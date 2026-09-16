package grocery

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func skipSelection() []RecipeSelection {
	return []RecipeSelection{{
		RecipeID: "r1", RecipeName: "Tacos", RecipeServings: 2, TargetServings: 2,
		Lines: []Line{
			line("name:cilantro", "Cilantro", "produce", q(1, 1), "count"),
			line("name:onion", "Yellow Onion", "produce", q(1, 1), "count"),
		},
	}}
}

func TestAggregateWithHoldsBackSkippedItemsInsteadOfDroppingThem(t *testing.T) {
	list, err := AggregateWith(skipSelection(), nil, SkipSet{"name:cilantro": SkipAlways})
	if err != nil {
		t.Fatalf("AggregateWith() error = %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].IngredientKey != "name:onion" {
		t.Fatalf("Items = %s, want only the onion", render(list))
	}
	if len(list.SkippedItems) != 1 {
		t.Fatalf("SkippedItems = %d, want 1", len(list.SkippedItems))
	}
	skipped := list.SkippedItems[0]
	if skipped.IngredientKey != "name:cilantro" {
		t.Errorf("skipped item = %q, want cilantro", skipped.IngredientKey)
	}
	if skipped.Status != StatusSkipped {
		t.Errorf("skipped status = %q, want %q", skipped.Status, StatusSkipped)
	}
	if skipped.SkipScope != SkipAlways {
		t.Errorf("skipped scope = %q, want %q", skipped.SkipScope, SkipAlways)
	}
	// The recipe still needs it, and the list still says how much and for what.
	if amountString(skipped) != "1 count" {
		t.Errorf("skipped amount = %q, want the amount the recipe asked for", amountString(skipped))
	}
	if len(skipped.Sources) != 1 || skipped.Sources[0].RecipeName != "Tacos" {
		t.Errorf("skipped sources = %+v, want the recipe that needed it", skipped.Sources)
	}
}

func TestAggregateWithoutSkipsBuysEverything(t *testing.T) {
	list, err := AggregateWith(skipSelection(), nil, nil)
	if err != nil {
		t.Fatalf("AggregateWith() error = %v", err)
	}
	if len(list.Items) != 2 || len(list.SkippedItems) != 0 {
		t.Errorf("without skips: %d items, %d skipped; want 2 and 0", len(list.Items), len(list.SkippedItems))
	}
	// Aggregate is AggregateWith without skips.
	plain, err := Aggregate(skipSelection(), nil)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}
	if render(plain) != render(list) {
		t.Errorf("Aggregate and AggregateWith(nil) disagree:\n%s\n%s", render(plain), render(list))
	}
}

// An ingredient the household never wants stays off the list even when the
// pantry happens to have some: the two states answer different questions.
func TestAggregateWithSkipOutranksThePantry(t *testing.T) {
	pantry := PantryStock{InStock: map[string]bool{"name:cilantro": true}, OutOfStock: map[string]bool{}}
	list, err := AggregateWith(skipSelection(), pantry, SkipSet{"name:cilantro": SkipThisWeek})
	if err != nil {
		t.Fatalf("AggregateWith() error = %v", err)
	}
	if len(list.SkippedItems) != 1 || list.SkippedItems[0].Status != StatusSkipped {
		t.Fatalf("SkippedItems = %+v, want the cilantro as skipped", list.SkippedItems)
	}
	for _, it := range list.Items {
		if it.IngredientKey == "name:cilantro" {
			t.Error("a skipped ingredient must not be on the list, even when the pantry has it")
		}
	}
}

// Skipping an ingredient a store alternative introduced drops only that
// component; the rest of the alternative is still bought.
func TestAggregateWithSkippedStoreAlternativeComponentLeavesTheRest(t *testing.T) {
	spec := &Specialty{
		ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste",
		Choice: &Choice{
			Type: ChoiceStoreAlternative, OptionID: "tex-mex-paste.store", OptionName: "Store mix",
			Per: Measure{Quantity: ingredients.NewQuantity(1, 1), Unit: "tbsp"},
			Components: []Component{
				{IngredientKey: "name:chili powder", Name: "Chili Powder", Category: "spices", Quantity: q(3, 2), Unit: "tsp"},
				{IngredientKey: "name:cilantro", Name: "Cilantro", Category: "produce", Quantity: q(1, 1), Unit: "tsp"},
			},
		},
	}
	selections := []RecipeSelection{{
		RecipeID: "r1", RecipeName: "Smoky Pork Tacos", RecipeServings: 2, TargetServings: 2,
		Lines: []Line{line("name:tex mex paste", "Tex-Mex Paste", "condiments", q(1, 1), "tbsp")},
	}}
	applied, _, err := ApplySpecialties(selections, Specialties{"name:tex mex paste": spec})
	if err != nil {
		t.Fatalf("ApplySpecialties() error = %v", err)
	}

	list, err := AggregateWith(applied, nil, SkipSet{"name:cilantro": SkipAlways})
	if err != nil {
		t.Fatalf("AggregateWith() error = %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].IngredientKey != "name:chili powder" {
		t.Fatalf("Items = %s, want only the chili powder", render(list))
	}
	if len(list.SkippedItems) != 1 || list.SkippedItems[0].IngredientKey != "name:cilantro" {
		t.Fatalf("SkippedItems = %+v, want the cilantro", list.SkippedItems)
	}
	// The skipped component still explains why it was ever on the list.
	via := list.SkippedItems[0].Via
	if len(via) != 1 || via[0].SpecialtyName != "Tex-Mex Paste" || via[0].Kind != ViaStoreAlternative {
		t.Errorf("skipped component Via = %+v, want the store alternative it came from", via)
	}
}
