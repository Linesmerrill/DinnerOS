package recipes

import (
	"testing"
)

// Each ingredient is listed once on the cooking screen (build 122, Hark's
// review item 5): butter readied in Have Ready isn't listed again in its
// steps, and "for steps" names only the steps that measure it out.
func TestHaveReadyButterIsNotRepeatedInItsSteps(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-butter", "Butter", "3", "6", "tbsp"),
		tacoLine("ing-pita", "Whole Wheat Pitas", "2", "4", "count"),
	},
		"In a small pot, melt 1 Tbsp butter over medium heat.",
		"Meanwhile, bring 2 Tbsp butter to room temperature.",
		"Toast pitas until warm.",
		"Season the rice.",
		"Spread pitas with softened butter, then cut each pita into quarters.")
	c := Annotate(r, 2, nil, false).Checklist
	if b := item(t, c, 0, "Butter"); b.Prep != "for steps 1 and 2" {
		t.Errorf("Have Ready butter = %+v", b)
	}
	for _, g := range c.ByStep {
		for _, it := range g.Items {
			if g.Index > 0 && it.Name == "Butter" {
				t.Errorf("step %d lists the butter again: %+v", g.Index, it)
			}
		}
	}
}

// Pasta boiled in step 1 and tossed in step 2 is the same pasta: step 2's
// row says where it comes from rather than reading like more to measure.
// Half a lemon used in a squeeze is that half, not a wedge the list doesn't
// have (Hark's review item 4).
func TestReusedIngredientSaysWhichStepItComesFrom(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-pasta", "Bow tie pasta", "8", "16", "oz"),
		tacoLine("ing-pesto", "Basil pesto", "1/3", "2/3", "cup"),
		tacoLine("ing-lemon", "Lemon", "1/2", "1", "count"),
	},
		"Boil the pasta in salted water until tender, about 11 minutes. Save a splash of the water, then drain.",
		"Toss the hot pasta with the pesto and the saved water until glossy.",
		"Fold in a squeeze of lemon.")
	in := Annotate(r, 2, nil, true)
	c := in.Checklist
	if it := item(t, c, 1, "Bow tie pasta"); it.AmountText != "8 oz" || it.Prep != "" {
		t.Errorf("step 1 pasta = %+v", it)
	}
	if it := item(t, c, 2, "Bow tie pasta"); it.AmountText != "" || it.Prep != "from step 1" {
		t.Errorf("step 2 pasta = %+v", it)
	}
	if got := joined(in.Steps[2]); got != "Fold in a squeeze of ½ lemon." {
		t.Errorf("step 3 = %q", got)
	}
	if it := item(t, c, 3, "Lemon"); it.AmountText != "½" {
		t.Errorf("step 3 lemon = %+v", it)
	}
}
