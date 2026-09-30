package recipes

import (
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
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
		// Half of ¼ oz of herbs isn't something to weigh: the card's words stay.
		{6, "Cilantro", "", ""},
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

// HelloFresh lists salt as the whole recipe's total and says in the step how
// much of it this step takes: the step shows that share for the size cooked.
func TestSaltFollowsTheCardsWeUsedNote(t *testing.T) {
	salt := RecipeIngredient{IngredientID: "ing-salt", Name: "Salt", Amounts: []Amount{
		{Servings: 2, Quantity: "2", Unit: "tsp"}, {Servings: 3, Quantity: "3", Unit: "tsp"},
		{Servings: 4, Quantity: "4", Unit: "tsp"}, {Servings: 6, Quantity: "6", Unit: "tsp"},
	}}
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-beef", "Ground Beef", "10", "15", "oz"), salt},
		"In a large bowl, combine beef, salt (we used ½ tsp; 1 tsp for 4), and pepper.",
		"Season all over with salt (we used 1⁄4 tsp salt; 1⁄2 tsp for 4 servings) and pepper.")
	for servings, want := range map[int][2]string{
		2: {"½ tsp salt,", "¼ tsp salt and"},
		3: {"¾ tsp salt,", "⅜ tsp salt and"},
		4: {"1 tsp salt,", "½ tsp salt and"},
		6: {"1 ½ tsp salt,", "¾ tsp salt and"},
	} {
		in := Annotate(r, servings, nil, true)
		for k, w := range want {
			got := joined(in.Steps[k])
			if !strings.Contains(got, w) || strings.Contains(got, "we used") {
				t.Errorf("%d servings, step %d = %q, want it to contain %q", servings, k+1, got, w)
			}
		}
	}
}

func TestOtherStepsDontGetTheTotalOnceACardMeasuresAShare(t *testing.T) {
	salt := RecipeIngredient{IngredientID: "ing-salt", Name: "Salt", Amounts: []Amount{{Servings: 2, Quantity: "2", Unit: "tsp"}, {Servings: 4, Quantity: "4", Unit: "tsp"}}}
	r := instructionRecipe([]RecipeIngredient{salt}, "Toss potatoes with salt and pepper.", "Combine beef and salt (we used ½ tsp; 1 tsp for 4).")
	r.Servings = []int{2, 4}
	in := Annotate(r, 2, nil, true)
	if got := joined(in.Steps[0]); got != "Toss potatoes with salt and pepper." {
		t.Errorf("step 1 = %q, want no amount on the salt", got)
	}
	if got := joined(in.Steps[1]); got != "Combine beef and ½ tsp salt." {
		t.Errorf("step 2 = %q", got)
	}
}

// Phrasings the library scan turned up, each once.
func TestLibraryPhrasings(t *testing.T) {
	flakes := tacoLine("ing-flakes", "Chili Flakes", "1", "3/2", "tsp")
	for _, tc := range []struct {
		name  string
		lines []RecipeIngredient
		step  string
		want  string
	}{
		{"a tip in the note keeps the card's words", []RecipeIngredient{flakes},
			"Add tomato and chili flakes (we used ½ tsp; add a pinch more if you like things spicy); cook.",
			"Add tomato and chili flakes (we used ½ tsp; add a pinch more if you like things spicy); cook."},
		{"'more' is to taste", []RecipeIngredient{flakes},
			"Add a pinch of chili flakes. (TIP: Add more chili flakes if you like things spicy!)",
			"Add a pinch of chili flakes. (TIP: Add more chili flakes if you like things spicy!)"},
		{"to taste keeps the card's words", []RecipeIngredient{flakes},
			"Add chili flakes to taste (we used ⅛ tsp).", "Add chili flakes to taste (we used ⅛ tsp)."},
		{"packaging isn't a name", []RecipeIngredient{tacoLine("ing-marinara", "Marinara Cup", "1", "3/2", "count")},
			"Add marinara and 2 cups water (4 cups for 4 servings).", "Add marinara and 2 cups water."},
		{"cheese and breadcrumbs are dropped", []RecipeIngredient{
			tacoLine("ing-parm", "Parmesan Cheese", "1/4", "3/8", "cup"), tacoLine("ing-panko", "Panko Breadcrumbs", "1/4", "3/8", "cup")},
			"Combine panko and Parmesan.", "Combine ¼ cup panko and ¼ cup Parmesan."},
		{"the last word is the food", []RecipeIngredient{tacoLine("ing-mush", "Button Mushrooms", "4", "6", "oz")},
			"Slice mushrooms.", "Slice 4 oz mushrooms."},
		{"a note with a name in it for the other box goes", []RecipeIngredient{tacoLine("ing-salt", "Salt", "", "", "")},
			"Stir in 1 tsp water and ½ tsp salt (2 tsp water and 1½ tsp salt for 4 servings).",
			"Stir in 1 tsp water and ½ tsp salt."},
		{"a division slash reads as a fraction", nil,
			"Add ¼ cup water (1∕3 cup for 4 servings).", "Add ¼ cup water."},
	} {
		r := tacoRecipe(tc.lines, tc.step)
		if got := joined(Annotate(r, 2, nil, true).Steps[0]); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
	// ⅛ oz for 3 servings is 0.1875: a kitchen measure, not a decimal.
	if got := (Measure{Quantity: ingredients.NewQuantity(3, 16), Unit: "oz"}).Text(); got != "¼ oz" {
		t.Errorf("3/16 oz = %q, want ¼ oz", got)
	}
	if got := (Measure{Quantity: ingredients.NewQuantity(2, 3), Unit: "cup"}).Text(); got != "⅔ cup" {
		t.Errorf("2/3 cup = %q", got)
	}
}

func TestReservedLiquidIsTheLiquidAndGrowsWithTheBox(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-pot", "Potatoes", "12", "18", "oz"), tacoLine("ing-lob", "Lobster Tails", "7", "10", "oz")},
		"Dice potatoes. Boil until tender. Reserve ½ cup potato cooking liquid, then drain.",
		"Cut along the tail. Add lobster tails and a splash of water.")
	for servings, want := range map[int]string{2: "Reserve ½ cup potato cooking liquid", 4: "Reserve 1 cup potato cooking liquid"} {
		in := Annotate(r, servings, nil, true)
		if got := joined(in.Steps[0]); !strings.Contains(got, want) || !strings.HasPrefix(got, "Dice ") || strings.Count(got, " oz") != 1 {
			t.Errorf("%d servings: %q", servings, got)
		}
		if got := joined(in.Steps[1]); !strings.Contains(got, "Cut along the tail. Add") || !strings.Contains(got, "oz lobster tails") {
			t.Errorf("%d servings lobster: %q (\"tail\" isn't the lobster)", servings, got)
		}
	}
}

func TestHerbsByTheQuarterOunceAndOilAsAVerbKeepTheirWords(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{
			tacoLine("ing-rosemary", "Rosemary", "1/4", "1/4", "oz"),
			tacoLine("ing-oil", "Cooking Oil", "2", "3", "tsp"),
			tacoLine("ing-parm", "Parmesan Cheese", "3", "9/2", "tbsp"),
		},
		"Line a baking sheet with foil and lightly oil. Toss with half the chopped rosemary.",
		"Stir in half the Parmesan.")
	in := Annotate(r, 2, nil, true)
	if got := joined(in.Steps[0]); got != "Line a baking sheet with foil and lightly oil. Toss with half the chopped rosemary." {
		t.Errorf("step 1 = %q", got)
	}
	// A share of something measured by the spoon still gets its amount.
	if got := joined(in.Steps[1]); got != "Stir in 1 ½ Tbsp Parmesan." {
		t.Errorf("step 2 = %q", got)
	}
}
