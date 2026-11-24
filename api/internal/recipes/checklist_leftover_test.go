package recipes

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// haveReady is the Have Ready row for name, or a zero item.
func haveReady(in Instructions, name string) CookStepItem {
	for _, g := range in.Checklist.ByStep {
		if g.Index != 0 {
			continue
		}
		for _, it := range g.Items {
			if it.Name == name {
				return it
			}
		}
	}
	return CookStepItem{}
}

// A meal kit's 1 tsp turmeric packet with "Stir in ¼ tsp turmeric": the
// recipe page says 1 tsp and step 1 says ¼ tsp, so Have Ready says where it
// goes and what's left rather than the rest vanishing (build 123, Hark's
// review item 10).
func TestHaveReadySaysWhatTheStepsLeaveOver(t *testing.T) {
	turmeric := RecipeIngredient{IngredientID: "ing-turmeric", Name: "Turmeric", Amounts: []Amount{
		{Servings: 2, Quantity: "1", Unit: "tsp"}, {Servings: 4, Quantity: "1", Unit: "tsp"},
	}}
	r := instructionRecipe([]RecipeIngredient{turmeric, instructionLine("ing-rice", "Jasmine Rice", "1/2", "cup")},
		"Melt butter. Stir in ¼ tsp turmeric (½ tsp for 4 servings); cook until fragrant. Stir in rice.")
	for _, tc := range []struct {
		servings int
		want     string
	}{
		{2, "¼ tsp for step 1, ¾ tsp left over"},
		{4, "½ tsp for step 1, ½ tsp left over"},
	} {
		in := Annotate(r, tc.servings, nil, false)
		row := haveReady(in, "Turmeric")
		if row.AmountText != "1 tsp" || row.Prep != tc.want {
			t.Errorf("%d servings: Have Ready turmeric = %q, %q; want 1 tsp, %q", tc.servings, row.AmountText, row.Prep, tc.want)
		}
		if got := in.Ingredients[0].LeftOver; got == nil {
			t.Errorf("%d servings: no left over amount on the ingredient", tc.servings)
		}
		if row := haveReady(in, "Jasmine Rice"); row.Prep != "" {
			t.Errorf("%d servings: rice = %q, want no note: the step uses all of it", tc.servings, row.Prep)
		}
	}
}

// When a later step takes the rest, or the steps use all of it, nothing is
// left over.
func TestHaveReadyHasNoLeftOverWhenTheRestIsUsed(t *testing.T) {
	for name, steps := range map[string][]string{
		"remaining": {"Stir in ¼ tsp turmeric.", "Season chicken with remaining turmeric."},
		"all":       {"Stir in ½ tsp turmeric.", "Season chicken with ½ tsp turmeric."},
		"no amount": {"Stir in ¼ tsp turmeric.", "Sprinkle with turmeric."},
		"whole":     {"Stir in turmeric."},
	} {
		r := instructionRecipe([]RecipeIngredient{instructionLine("ing-turmeric", "Turmeric", "1", "tsp")}, steps...)
		in := Annotate(r, 2, nil, false)
		if lo := in.Ingredients[0].LeftOver; lo != nil {
			t.Errorf("%s: left over = %q, want none", name, lo.Text())
		}
		if row := haveReady(in, "Turmeric"); row.Prep != "" && name != "all" {
			t.Errorf("%s: Have Ready turmeric = %q, want no note", name, row.Prep)
		}
	}
}

// A crumb left by rounding a step's amount isn't worth a note.
func TestLeftOverIgnoresACrumb(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{instructionLine("ing-oil", "Olive Oil", "3", "tbsp")},
		"Heat 2 ¾ Tbsp olive oil.")
	if lo := Annotate(r, 2, nil, false).Ingredients[0].LeftOver; lo != nil {
		t.Errorf("left over = %q, want none for a crumb", lo.Text())
	}
}

func stockSpecialty(t *testing.T) grocery.Specialties {
	t.Helper()
	one := ingredients.NewQuantity(1, 1)
	return grocery.Specialties{"ing-stock": {
		ID: "chicken-stock-concentrate", Key: "chicken stock concentrate", Name: "Chicken Stock Concentrate",
		UnitSizes: []grocery.UnitSize{{Unit: "count", Quantity: *quantityPtr(t, "2"), SizeUnit: "tsp"}},
		Choice: &grocery.Choice{
			Type: grocery.ChoiceStoreAlternative, OptionID: "o", OptionName: "Concentrated chicken bouillon base",
			Per:        grocery.Measure{Quantity: one, Unit: "count"},
			Components: []grocery.Component{{Name: "Chicken Bouillon Base", Quantity: &one, Unit: "tsp"}},
		},
	}}
}

// Stock concentrate bought as a jar of bouillon base: the steps and the
// cooking list say "Chicken Bouillon Base", so the recipe page does too,
// with its amount and no "Made with" line repeating the name (build 123,
// Hark's review item 10).
func TestRecipePageNamesTheStoreIngredientTheStepsUse(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		instructionLine("ing-rice", "Jasmine Rice", "1/2", "cup"),
		instructionLine("ing-stock", "Chicken Stock Concentrate", "1", "count"),
	}, "Stir in rice, ¾ cup water, and stock concentrate.")
	for _, tc := range []struct {
		servings int
		amount   string
	}{{2, "1 tsp"}, {4, "2 tsp"}} {
		in := Annotate(r, tc.servings, stockSpecialty(t), true)
		resp := newInstructionsResponse(in).Ingredients[1]
		if resp.Name != "Chicken Stock Concentrate" || resp.SwapName != "Chicken Bouillon Base" || resp.AmountText != tc.amount {
			t.Errorf("%d servings: recipe page = %q as %q, %q; want Chicken Bouillon Base, %s", tc.servings, resp.Name, resp.SwapName, resp.AmountText, tc.amount)
		}
		if resp.Component == nil || len(resp.Component.Parts) != 0 {
			t.Errorf("%d servings: component = %+v, want it kept with no parts", tc.servings, resp.Component)
		}
		ci := in.Checklist.All[1]
		if ci.Name != resp.SwapName || ci.AmountText != resp.AmountText {
			t.Errorf("%d servings: cooking list = %q %q, recipe page = %q %q", tc.servings, ci.AmountText, ci.Name, resp.AmountText, resp.SwapName)
		}
		var seg Segment
		for _, s := range ingredientSegments(in.Steps[0]) {
			if s.Ingredient == 1 {
				seg = s
			}
		}
		if seg.Name != "Chicken Bouillon Base" {
			t.Errorf("%d servings: step names %q", tc.servings, seg.Name)
		}
	}
}

// Left out, it keeps its own name on the recipe page, crossed out.
func TestLeftOutStoreIngredientKeepsItsOwnName(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{instructionLine("ing-stock", "Chicken Stock Concentrate", "1", "count")},
		"Stir in stock concentrate.")
	left := grocery.LeftOutSet{"ing-stock": {SkipID: "s1", Scope: grocery.SkipRecipe}}
	in := AnnotateWith(r, 2, stockSpecialty(t), true, left, true)
	resp := newInstructionsResponse(in).Ingredients[0]
	if resp.SwapName != "" {
		t.Errorf("swap name = %q, want none for a left-out ingredient", resp.SwapName)
	}
}
