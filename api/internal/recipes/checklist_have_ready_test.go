package recipes

import "testing"

// Every recipe's checklist starts with Have Ready: every ingredient, with the
// whole recipe's amount where it has one, so the cook gathers it all before
// the first step. A recipe without it, or with an ingredient missing from it
// or missing its amount, fails (decision 635).
func TestEveryRecipeStartsWithHaveReady(t *testing.T) {
	check := func(name string, in Instructions) {
		t.Helper()
		groups := in.Checklist.ByStep
		if len(groups) == 0 || groups[0].Index != 0 {
			t.Errorf("%s: checklist doesn't start with Have Ready", name)
			return
		}
		ready := map[int]CookStepItem{}
		for _, it := range groups[0].Items {
			ready[it.IngredientIndex] = it
		}
		for _, ci := range in.Checklist.All {
			it, ok := ready[ci.Index]
			if !ok {
				t.Errorf("%s: Have Ready is missing %s", name, ci.Name)
				continue
			}
			if ci.AmountText != "" && it.AmountText != ci.AmountText {
				t.Errorf("%s: Have Ready %s = %q, want the whole recipe's %q", name, ci.Name, it.AmountText, ci.AmountText)
			}
		}
	}
	// The cases written for these tests.
	check("butter across steps", Annotate(tacoRecipe([]RecipeIngredient{
		tacoLine("ing-butter", "Butter", "3", "6", "tbsp"),
		tacoLine("ing-rice", "Jasmine Rice", "1/2", "1", "cup"),
		tacoLine("ing-salt", "Salt", "", "", ""),
	}, "Melt 1 Tbsp butter; stir in rice and salt.", "Bring 2 Tbsp butter to room temperature."), 2, nil, false))
	check("one step", Annotate(tacoRecipe([]RecipeIngredient{
		tacoLine("ing-pasta", "Spaghetti", "6", "9", "oz"),
	}, "Cook spaghetti until al dente."), 2, nil, false))

	// And every recipe in the step corpus, at each size it's written for.
	for _, c := range loadCorpus(t) {
		r := c.recipe()
		for _, n := range r.Servings {
			check(r.Name, Annotate(r, n, nil, len(r.Servings) == 1))
		}
	}
}
