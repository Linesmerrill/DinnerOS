package recipes

import (
	"strings"
	"testing"
)

// The stir-fry card says "half the coconut milk (you'll use more later)" and
// "splashes of remaining coconut milk" for a carton counted as "1 unit". The
// steps say how much that is: a meal kit's carton is ⅔ cup, so half is ⅓ cup
// and the rest is ⅓ cup, never half of a store can.
func TestHalfTheCoconutMilkSaysHowMuch(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-coco", "Coconut Milk", "1", "3/2", "count"),
		tacoLine("ing-curry", "Curry Powder", "1", "3/2", "tbsp"),
	},
		"Stir in chili sauce, stock concentrate, half the coconut milk (you’ll use more later), and half the curry powder (all for 4 servings)",
		"Add drained noodles. If needed, stir in splashes of remaining coconut milk until everything is thoroughly coated in sauce.")
	for _, tc := range []struct {
		servings          int
		whole, half, step string
	}{
		{2, "⅔ cup", "⅓ cup", "⅓ cup coconut milk (you’ll use more later)"},
		// The card leaves 4 servings blank; it's twice the 2-serving box.
		{4, "1 ⅓ cups", "⅔ cup", "⅔ cup coconut milk (you’ll use more later)"},
	} {
		in := Annotate(r, tc.servings, nil, false)
		if got := joined(in.Steps[0]); !strings.Contains(got, tc.step) || strings.Contains(got, "half the coconut") {
			t.Errorf("%d servings: step 1 = %q, want %q", tc.servings, got, tc.step)
		}
		for _, ing := range in.Ingredients {
			if ing.Name == "Coconut Milk" && (ing.Amount == nil || ing.Amount.Text() != tc.whole) {
				t.Errorf("%d servings: ingredient = %+v, want %s", tc.servings, ing.Amount, tc.whole)
			}
		}
		amounts := map[int]string{}
		for _, g := range in.Checklist.ByStep {
			for _, it := range g.Items {
				if it.Name == "Coconut Milk" {
					amounts[g.Index] = it.AmountText
				}
			}
		}
		if amounts[1] != tc.half || amounts[2] != tc.half {
			t.Errorf("%d servings: checklist = %v, want %s in steps 1 and 2", tc.servings, amounts, tc.half)
		}
	}
}
