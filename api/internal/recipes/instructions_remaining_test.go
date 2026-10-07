package recipes

import (
	"strings"
	"testing"
)

// A meal kit's "half the Italian Seasoning (you'll use the rest later)" and
// then "remaining Italian Seasoning": the second step says how much is left,
// 1½ tsp of the tablespoon, in the step and its checklist. "the rest" with no
// amount is a failure (decision 631).
func TestRemainingSaysHowMuchIsLeft(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-seasoning", "Italian Seasoning", "1", "2", "tbsp"),
		tacoLine("ing-zucchini", "Zucchini", "1", "2", "count"),
		tacoLine("ing-paste", "Tomato Paste", "1.5", "3", "oz"),
	},
		"Toss zucchini with a drizzle of oil, half the Italian Seasoning (you'll use the rest later), and a pinch of salt.",
		"Add tomato paste and remaining Italian Seasoning to pan. Cook until fragrant, 1 minute.")
	for _, tc := range []struct {
		servings int
		share    string
	}{{2, "1 ½ tsp"}, {3, "1 Tbsp"}} {
		in := Annotate(r, tc.servings, nil, false)
		var got string
		for _, seg := range ingredientSegments(in.Steps[1]) {
			if seg.Name == "Italian Seasoning" && seg.Amount != nil {
				got = seg.Amount.Text()
			}
		}
		if got != tc.share {
			t.Errorf("%d servings: remaining seasoning = %q, want %q (step: %q)", tc.servings, got, tc.share, joined(in.Steps[1]))
		}
		for _, step := range in.Checklist.ByStep {
			for _, item := range step.Items {
				if strings.Contains(item.Prep, "the rest") {
					t.Errorf("%d servings: checklist says %q for %s", tc.servings, item.Prep, item.Name)
				}
			}
		}
	}
}

// Tomato paste is spooned, not weighed: a card's "1.5 oz" reads as spoons in
// the steps, the checklist, and the ingredients to have ready, never ounces
// (decision 631).
func TestTomatoPasteIsMeasuredInSpoons(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-paste", "Tomato Paste", "1.5", "3", "oz"),
		tacoLine("ing-curry", "Red Curry Paste", "1", "2", "oz"),
		tacoLine("ing-cheese", "Cheddar Cheese", "2", "4", "oz"),
	},
		"Stir in tomato paste and red curry paste.",
		"Top with cheddar cheese.")
	in := Annotate(r, 2, nil, false)
	texts := map[string]string{}
	for _, step := range in.Steps {
		for _, seg := range ingredientSegments(step) {
			if seg.Amount != nil {
				texts[seg.Name] = seg.Amount.Text()
			}
		}
	}
	for _, name := range []string{"Tomato Paste", "Red Curry Paste"} {
		if got := texts[name]; got == "" || strings.Contains(got, "oz") || !(strings.Contains(got, "Tbsp") || strings.Contains(got, "tsp")) {
			t.Errorf("%s reads %q, want spoons", name, got)
		}
	}
	// 1.5 oz of tomato paste is about 2½ Tbsp.
	if got := texts["Tomato Paste"]; got != "2 ½ Tbsp" {
		t.Errorf("tomato paste = %q, want 2 ½ Tbsp", got)
	}
	// Cheese is weighed and sold by weight: it stays in ounces.
	if got := texts["Cheddar Cheese"]; got != "2 oz" {
		t.Errorf("cheddar = %q, want 2 oz", got)
	}
	for _, ing := range in.Ingredients {
		if ing.Amount != nil && strings.Contains(ing.Amount.Text(), "oz") && strings.Contains(ing.Name, "Paste") {
			t.Errorf("ingredient list: %s %s", ing.Amount.Text(), ing.Name)
		}
	}
	for _, step := range in.Checklist.ByStep {
		for _, item := range step.Items {
			if strings.Contains(item.Name, "Paste") && strings.Contains(item.AmountText, "oz") {
				t.Errorf("checklist: %s %s", item.AmountText, item.Name)
			}
		}
	}
}
