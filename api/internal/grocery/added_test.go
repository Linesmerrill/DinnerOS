package grocery

import "testing"

// addedMilk is an item a member added by hand: for no meal.
func addedMilk() Line {
	l := line("milk", "Milk", "dairy", q(2, 1), "count")
	l.Extra = &Extra{ID: "a1", Origin: OriginAdded, Text: "Added"}
	return l
}

func TestAnAddedItemIsForNoMeal(t *testing.T) {
	list := mustAggregate(t, []RecipeSelection{{RecipeID: "extras", RecipeServings: 1, TargetServings: 1, Lines: []Line{addedMilk()}}}, PantrySet{})
	milk := find(t, list, "milk")
	if len(milk.Sources) != 0 || len(milk.Shares) != 0 {
		t.Errorf("an added item names no meal: sources %+v, shares %+v", milk.Sources, milk.Shares)
	}
	if len(milk.Extras) != 1 || milk.Extras[0].Origin != OriginAdded || milk.Status != StatusToBuy {
		t.Errorf("milk = %+v", milk)
	}
	if len(milk.Amounts) != 1 || milk.Amounts[0].Quantity.String() != "2" || milk.Amounts[0].Unit.Code != "count" {
		t.Errorf("amounts = %+v", milk.Amounts)
	}
}

func TestAnAddedItemIsBoughtWhateverThePantrySays(t *testing.T) {
	recipeMilk := line("milk", "Milk", "dairy", q(1, 1), "cup")
	list := mustAggregate(t, []RecipeSelection{
		{RecipeID: "pancakes", RecipeName: "Pancakes", RecipeServings: 2, TargetServings: 2, Lines: []Line{recipeMilk}},
		{RecipeID: "extras", RecipeServings: 1, TargetServings: 1, Lines: []Line{addedMilk()}},
	}, PantrySet{"milk": true})
	milk := find(t, list, "milk")
	if milk.Status != StatusToBuy {
		t.Errorf("status = %q, want toBuy: a member added it", milk.Status)
	}
	if len(milk.Sources) != 1 || milk.Sources[0].RecipeID != "pancakes" {
		t.Errorf("the recipe still names itself: %+v", milk.Sources)
	}

	// Without the added line the pantry keeps it off the list, as before.
	list = mustAggregate(t, []RecipeSelection{
		{RecipeID: "pancakes", RecipeServings: 2, TargetServings: 2, Lines: []Line{recipeMilk}},
	}, PantrySet{"milk": true})
	if got := find(t, list, "milk").Status; got != StatusInPantry {
		t.Errorf("status = %q, want inPantry", got)
	}
}

func TestAPairingStillTakesItsMeal(t *testing.T) {
	l := line("crackers", "Crackers", "pantry", q(1, 1), "package")
	l.Extra = &Extra{ID: "p1", Origin: OriginPairing}
	list := mustAggregate(t, []RecipeSelection{{RecipeID: "soup", RecipeName: "Soup", RecipeServings: 1, TargetServings: 1, Lines: []Line{l}}}, PantrySet{})
	if got := find(t, list, "crackers").Sources; len(got) != 1 || got[0].RecipeID != "soup" {
		t.Errorf("sources = %+v", got)
	}
}
