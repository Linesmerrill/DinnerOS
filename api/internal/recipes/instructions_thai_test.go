package recipes

import (
	"strings"
	"testing"
)

func thaiCurry() Recipe {
	return tacoRecipe(
		[]RecipeIngredient{
			tacoLine("ing-rice", "Jasmine Rice", "1/2", "3/4", "cup"),
			tacoLine("ing-bell", "Bell Pepper", "1", "3/2", "count"),
			tacoLine("ing-lime", "Lime", "1", "3/2", "count"),
			tacoLine("ing-cil", "Cilantro", "1/4", "1/4", "oz"),
			tacoLine("ing-chili", "Chili Pepper", "1", "1", "count"),
			tacoLine("ing-chicken", "Chicken Breast Strips", "10", "15", "oz"),
			tacoLine("ing-curry", "Curry Powder", "1", "3/2", "tbsp"),
			tacoLine("ing-coco", "Coconut Milk", "1", "3/2", "count"),
			tacoLine("ing-sauce", "Sweet Thai Chili Sauce", "2", "3", "tbsp"),
			tacoLine("ing-stock", "Chicken Stock Concentrate", "1", "3/2", "tsp"),
			tacoLine("ing-oil", "Vegetable Oil", "2", "3", "tsp"),
			tacoLine("ing-sugar", "Sugar", "1", "3/2", "tsp"),
			tacoLine("ing-butter", "Butter", "1", "3/2", "tbsp"),
			tacoLine("ing-salt", "Salt", "", "", ""),
			tacoLine("ing-pep", "Pepper", "", "", ""),
		},
		"In a small pot, combine rice, ¾ cup water (1½ cups for 4 servings), and a pinch of salt. Bring to a boil, then cover and reduce to a low simmer. Cook until rice is tender, 15-18 minutes. • Keep covered off heat until ready to serve.",
		"While rice cooks, wash and dry all produce.  • Core, deseed, and dice bell pepper into 1-inch pieces. Zest and quarter lime. Mince cilantro. Thinly slice chili.  • Pat chicken* dry with paper towels.",
		"Heat a large drizzle of oil in a medium pan over medium-high heat (use a large pan for 4 servings). Add bell pepper and a big pinch of salt. Cook, stirring occasionally, 5 minutes.",
		"Add chicken, another large drizzle of oil, and a big pinch of salt to pan with bell pepper. Cook, stirring occasionally, until chicken is lightly browned, 3-4 minutes (it’ll finish cooking in the next step).  • Stir in half the curry powder (all for 4 servings); cook for 1 minute.",
		"Thoroughly shake coconut milk in container before opening\nStir ⅔ cup coconut milk (1⅓ cups for 4 servings), chili sauce, stock concentrate, juice from half the lime, and 1 tsp sugar (2 tsp for 4) into pan with chicken mixture. Bring to a simmer, then reduce heat to medium low. Simmer until sauce is thickened, bell pepper is tender, and chicken is cooked through, 4-6 minutes.\nTaste and season with salt and more lime juice if desired. Turn off heat.",
		"Fluff rice with a fork; stir in lime zest, half the cilantro, and 1 TBSP butter (2 TBSP for 4 servings). Season with salt and pepper.  • Divide rice between shallow bowls and top with coconut curry chicken, remaining cilantro, and a pinch of chili if desired. Serve with any remaining lime wedges on the side.")
}

func TestThaiCurryReadsLikeACook(t *testing.T) {
	in := Annotate(thaiCurry(), 2, nil, true)
	c := in.Checklist
	checks := []struct {
		step         int
		name, amount string
		prep         string
	}{
		{2, "Lime", "1", "zested and quartered"},
		{2, "Chicken Breast Strips", "10 oz", "patted dry"},
		{4, "Curry Powder", "1 ½ tsp", ""},
		{5, "Coconut Milk", "⅔ cup", ""},
		{5, "Sweet Thai Chili Sauce", "2 Tbsp", ""},
		{6, "Lime zest", "", ""},
		{6, "Cilantro", "⅛ oz", ""},
	}
	for _, tc := range checks {
		it := item(t, c, tc.step, tc.name)
		if it.AmountText != tc.amount || it.Prep != tc.prep {
			t.Errorf("step %d %s = %q %q, want %q %q", tc.step, tc.name, it.AmountText, it.Prep, tc.amount, tc.prep)
		}
	}
	// Descriptions aren't things to add: "bell pepper is tender", "to pan
	// with bell pepper", and "more lime juice" after the juiced wedges.
	for _, g := range c.ByStep {
		for _, it := range g.Items {
			if (g.Index == 4 || g.Index == 5) && it.Name == "Bell Pepper" || g.Index == 5 && it.Name == "Lime" {
				t.Errorf("step %d lists %s", g.Index, it.Name)
			}
		}
	}
	// The shake comes before the measured pour, which gets the amount.
	if got := joined(in.Steps[4]); !strings.HasPrefix(got, "Thoroughly shake coconut milk") || !strings.Contains(got, "Stir ⅔ cup coconut milk") {
		t.Errorf("step 5 = %q", got)
	}
	if got := joined(in.Steps[3]); !strings.Contains(got, "Stir in 1 ½ tsp curry powder;") {
		t.Errorf("step 4 = %q", got)
	}
	// For 4, the card says all of it.
	if got := joined(Annotate(thaiCurry(), 4, nil, true).Steps[3]); !strings.Contains(got, "Stir in 2 Tbsp curry powder;") {
		t.Errorf("step 4 for 4 = %q", got)
	}
}

func TestHalfOfAnUnmeasuredUnitKeepsTheCardsWords(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-ginger", "Ginger", "1", "2", "thumb")},
		"Add half the ginger and cook until fragrant.")
	if got := joined(Annotate(r, 2, nil, true).Steps[0]); got != "Add half the ginger and cook until fragrant." {
		t.Errorf("step = %q", got)
	}
}
