package recipes

import (
	"strings"
	"testing"
)

// Recipes people add themselves: one serving size, amounts in the steps, and
// names as a pasted line imports them.

// homeLine is one ingredient line of a home recipe, for one serving size.
// quantity "1" with no unit is how a pasted "1 onion" imports.
func homeLine(name, quantity, unit string) RecipeIngredient {
	line := RecipeIngredient{Name: name, Amounts: []Amount{{Servings: 4}}}
	if quantity != "" {
		line.Amounts[0].Quantity, line.Amounts[0].Unit = quantity, unit
	}
	return line
}

func homeRecipe(lines []RecipeIngredient, steps ...string) Recipe {
	r := Recipe{ID: "home", Name: "Family Dinner", Servings: []int{4}, Ingredients: lines}
	for i, text := range steps {
		r.Steps = append(r.Steps, Step{Index: i + 1, Text: text})
	}
	return r
}

func homeSteps(t *testing.T, r Recipe) Instructions {
	t.Helper()
	in := Annotate(r, 4, nil, true)
	for _, f := range CheckSteps(in) {
		t.Errorf("step %d reads wrong: %s %q", f.Step, f.Code, f.Detail)
	}
	return in
}

func wantStep(t *testing.T, in Instructions, step int, want string) {
	t.Helper()
	if got := joined(in.Steps[step-1]); got != want {
		t.Errorf("step %d = %q, want %q", step, got, want)
	}
}

func TestHomeStepsReadSpelledOutUnitsAsTheStepsOwnAmount(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("olive oil", "3", "tbsp"),
		homeLine("spaghetti", "450", "g"),
		homeLine("ground beef", "2", "lb"),
	},
		"Heat 2 tablespoons olive oil in a pot.",
		"Cook 450 grams spaghetti. Brown 2 pounds ground beef.",
		"Toss with 1 1/2 tablespoons of the olive oil.")
	in := homeSteps(t, r)
	wantStep(t, in, 1, "Heat 2 Tbsp olive oil in a pot.")
	wantStep(t, in, 2, "Cook 450 g spaghetti. Brown 2 lb ground beef.")
	// "of the" between the amount and the name is still the step's share.
	wantStep(t, in, 3, "Toss with 1 ½ Tbsp olive oil.")
	if segs := ingredientSegments(in.Steps[2]); len(segs) != 1 || !segs[0].Part {
		t.Errorf("segments = %+v, want the share marked as part of the oil", segs)
	}
}

func TestHomeStepsKeepARangeAsWritten(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{homeLine("garlic", "3", "clove")},
		"Add 2-3 cloves garlic and stir.",
		"Add 1 to 2 tablespoons of the garlic oil.")
	in := homeSteps(t, r)
	wantStep(t, in, 1, "Add 2-3 cloves garlic and stir.")
	if segs := ingredientSegments(in.Steps[0]); len(segs) != 1 || segs[0].Amount != nil {
		t.Errorf("segments = %+v, want garlic marked with no amount", segs)
	}
	// The recipe's total doesn't land on a later mention either.
	wantStep(t, in, 2, "Add 1 to 2 tablespoons of the garlic oil.")
}

func TestHomeStepsPutTheAmountBeforeHowItsReadied(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("bananas", "3", ""),
		homeLine("butter", "1/3", "cup"),
		homeLine("egg", "1", ""),
		homeLine("onion", "1", ""),
	},
		"Mash the bananas.",
		"Stir in the melted butter, beaten egg, and the peeled and diced onion.")
	in := homeSteps(t, r)
	wantStep(t, in, 2, "Stir in ⅓ cup melted butter, 1 beaten egg, and 1 peeled and diced onion.")
	var group CookStepGroup
	for _, g := range in.Checklist.ByStep {
		if g.Index == 0 {
			t.Errorf("have-ready group = %+v, want nothing: melted butter isn't cut into pieces", g.Items)
		}
		if g.Index == 2 {
			group = g
		}
	}
	preps := map[string]string{}
	for _, it := range group.Items {
		preps[it.Name] = it.Prep
	}
	if preps["butter"] != "melted" || preps["onion"] != "peeled and diced" {
		t.Errorf("checklist preps = %v, want the words that moved after the amount", preps)
	}
}

func TestHomeStepsFindNamesWithoutTheirTails(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("salt and pepper to taste", "", ""),
		homeLine("salt to taste", "", ""),
		homeLine("fresh parsley (optional)", "", ""),
		homeLine("shredded cheddar cheese, for serving", "1", "cup"),
	},
		"Season with salt and pepper to taste. Taste again.",
		"Stir in 1 teaspoon salt.",
		"Top with cheddar and parsley.")
	in := homeSteps(t, r)
	if segs := ingredientSegments(in.Steps[0]); len(segs) != 1 || segs[0].Text != "salt and pepper" {
		t.Errorf("segments = %+v, want only \"salt and pepper\": \"to taste\" and \"Taste\" are the step's words", segs)
	}
	// A line with no amount takes the one the step writes.
	wantStep(t, in, 2, "Stir in 1 tsp salt.")
	if segs := ingredientSegments(in.Steps[1]); len(segs) != 1 || segs[0].Amount == nil || segs[0].Ingredient != 1 {
		t.Errorf("segments = %+v, want 1 tsp of the salt line", segs)
	}
	wantStep(t, in, 3, "Top with 1 cup cheddar and parsley.")
}

func TestHomeStepsSkipIngredientHeadings(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("For the sauce:", "", ""),
		homeLine("ketchup", "1", "cup"),
	}, "Whisk the ketchup.")
	in := homeSteps(t, r)
	for _, ci := range in.Checklist.All {
		if strings.HasSuffix(ci.Name, ":") {
			t.Errorf("checklist has the heading %q", ci.Name)
		}
	}
	if len(in.Checklist.All) != 1 {
		t.Errorf("checklist = %+v, want only the ketchup", in.Checklist.All)
	}
}

func TestHomeStepsCountAPastedLineAndItsCans(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("(14.5 oz) can diced tomatoes", "2", ""),
		homeLine("onion", "1", ""),
		homeLine("limes", "3", ""),
	},
		"Add the tomatoes with their juices.",
		"Dice half the onion. Juice the limes.")
	in := homeSteps(t, r)
	wantStep(t, in, 1, "Add 2 cans tomatoes with their juices.")
	// A bare "1" is a count: half the onion is a share to have ready.
	if segs := ingredientSegments(in.Steps[1]); len(segs) != 2 || segs[0].Amount == nil || segs[0].Amount.Text() != "½" {
		t.Errorf("segments = %+v, want half the onion counted as ½", segs)
	}
	wantStep(t, in, 2, "Dice half the onion. Juice 3 limes.")
}

func TestHomeStepsCarryAShareDownAList(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("garlic powder", "1", "tsp"),
		homeLine("smoked paprika", "1", "tsp"),
		homeLine("salt", "1", "tsp"),
		homeLine("black pepper", "1/2", "tsp"),
	}, "Toss with half of the garlic powder, paprika, salt and pepper. Add the rest later.")
	in := homeSteps(t, r)
	wantStep(t, in, 1, "Toss with ½ tsp garlic powder, ½ tsp paprika, ½ tsp salt and ¼ tsp pepper. Add the rest later.")
}

// A meal-kit card's "half the X, <amount> Y, and Z" halves only X.
func TestMealKitShareStopsAtTheNextName(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		instructionLine("", "Ground Beef", "10", "oz"),
		instructionLine("", "Beef Stock Concentrate", "1", "count"),
		instructionLine("", "Salt", "11", "tsp"),
		instructionLine("", "Pepper", "", ""),
		instructionLine("", "Penne Pasta", "6", "oz"),
		instructionLine("", "Parmesan Cheese", "1/4", "cup"),
		instructionLine("", "Butter", "1", "tbsp"),
		instructionLine("", "Yellow Onion", "1", "count"),
		instructionLine("", "Lime", "1", "count"),
		instructionLine("", "Sugar", "1", "tsp"),
	},
		"Combine beef, half the stock concentrate, ¾ tsp salt (1¼ tsp for 4 servings), and pepper.",
		"Stir drained penne, half the Parmesan, and 1 TBSP butter (2 TBSP for 4 servings) into pan.",
		"Combine ¼ of the onion, juice from half the lime, ¼ tsp sugar (½ tsp for 4 servings), and a pinch of salt.")
	for n, want := range map[int][]string{2: {"¾ tsp salt", "1 Tbsp butter", "¼ tsp sugar"}, 4: {"1 ¼ tsp salt", "2 Tbsp butter", "½ tsp sugar"}} {
		in := Annotate(r, n, nil, true)
		for i, w := range want {
			if got := joined(in.Steps[i]); !strings.Contains(got, w) {
				t.Errorf("for %d, step %d = %q, want it to contain %q", n, i+1, got, w)
			}
		}
	}
	// A home list with an amount in it doesn't share either.
	home := homeRecipe([]RecipeIngredient{
		homeLine("garlic powder", "1", "tsp"),
		homeLine("smoked paprika", "1", "tsp"),
		homeLine("salt", "1", "tsp"),
	}, "Toss with half of the garlic powder, 1 tsp paprika and salt.")
	wantStep(t, homeSteps(t, home), 1, "Toss with ½ tsp garlic powder, 1 tsp paprika and 1 tsp salt.")
}

// On a meal-kit card, "the cheese" is never the Parmesan: the card names it.
func TestMealKitCheeseStaysNamed(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		instructionLine("", "Butter", "1", "tbsp"),
		instructionLine("", "Parmesan Cheese", "3", "tbsp"),
	},
		"Melt butter and stir until the cheese sauce thickens.",
		"Add Parmesan.")
	in := Annotate(r, 2, nil, true)
	wantStep(t, in, 1, "Melt 1 Tbsp butter and stir until the cheese sauce thickens.")
	wantStep(t, in, 2, "Add 3 Tbsp Parmesan.")
}

// "a drizzle of oil" is the cooking oil when there is one, wherever the
// olive oil is listed; olive oil answers to "oil" only when it's the only oil.
func TestPlainOilIsTheCookingOil(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		instructionLine("", "Olive Oil", "1", "tbsp"),
		instructionLine("", "Cooking Oil", "2", "tsp"),
	}, "Heat a drizzle of oil. Toss greens with olive oil.")
	in := Annotate(r, 2, nil, true)
	if segs := ingredientSegments(in.Steps[0]); len(segs) != 2 || segs[0].Name != "Cooking Oil" || segs[1].Name != "Olive Oil" {
		t.Errorf("segments = %+v, want the cooking oil, then the olive oil", segs)
	}
	only := homeRecipe([]RecipeIngredient{homeLine("extra-virgin olive oil", "3", "tbsp")}, "Warm the oil.")
	wantStep(t, homeSteps(t, only), 1, "Warm 3 Tbsp oil.")
}

func TestHomeStepsGiveARepeatedLineToTheLaterStep(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("brown sugar", "2", "tbsp"),
		homeLine("pork shoulder", "4", "lb"),
		homeLine("ketchup", "1", "cup"),
		homeLine("brown sugar", "3", "tbsp"),
	},
		"Rub the brown sugar all over the pork.",
		"Whisk the ketchup and brown sugar.")
	in := homeSteps(t, r)
	wantStep(t, in, 1, "Rub 2 Tbsp brown sugar all over 4 lb pork.")
	wantStep(t, in, 2, "Whisk 1 cup ketchup and 3 Tbsp brown sugar.")
}

func TestHomeStepsFindShortNamesHomeCooksUse(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("extra-virgin olive oil", "3", "tbsp"),
		homeLine("bone-in chicken thighs", "2", "lb"),
		homeLine("broccoli florets", "2", "cup"),
		homeLine("beef broth", "2", "cup"),
		homeLine("shredded cheddar", "1", "cup"),
		homeLine("Worcestershire sauce", "1", "tbsp"),
	},
		"Warm the oil. Brown the chicken thighs.",
		"Add the broccoli and pour in the broth.",
		"Top with most of the cheese and a couple shakes of Worcestershire.")
	in := homeSteps(t, r)
	wantStep(t, in, 1, "Warm 3 Tbsp oil. Brown 2 lb chicken thighs.")
	wantStep(t, in, 2, "Add 2 cups broccoli and pour in 2 cups broth.")
	wantStep(t, in, 3, "Top with most of the cheese and a couple shakes of Worcestershire.")
	if segs := ingredientSegments(in.Steps[2]); len(segs) != 2 {
		t.Errorf("segments = %+v, want the cheese and the Worcestershire marked", segs)
	}
}

func TestHomeStepsLeaveCheeseAloneWithTwoCheeses(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("shredded cheddar", "1", "cup"),
		homeLine("mozzarella", "1", "cup"),
	}, "Top with cheddar, mozzarella, and more cheese.")
	in := Annotate(r, 4, nil, true)
	if segs := ingredientSegments(in.Steps[0]); len(segs) != 2 {
		t.Errorf("segments = %+v, want \"cheese\" left plain: it could be either", segs)
	}
}

func TestChecklistEndsAClauseAtAParenthesis(t *testing.T) {
	r := homeRecipe([]RecipeIngredient{
		homeLine("garlic", "4", "clove"),
		homeLine("dried oregano", "1", "tsp"),
	}, "Whisk 4 cloves garlic (minced), 1 tsp dried oregano.")
	in := homeSteps(t, r)
	for _, g := range in.Checklist.ByStep {
		for _, it := range g.Items {
			if it.Name == "dried oregano" && it.Prep != "" {
				t.Errorf("oregano prep = %q, want none: the garlic is minced", it.Prep)
			}
		}
	}
}

func TestEndsWithWord(t *testing.T) {
	for _, c := range []struct {
		s, phrase string
		want      bool
	}{
		{"add more", "more", true}, {"more", "more", true}, {"anymore", "more", false},
		{"wash (except", "except", true}, {"add some", "some", true}, {"handsome", "some", false},
	} {
		if got := endsWithWord(c.s, c.phrase); got != c.want {
			t.Errorf("endsWithWord(%q, %q) = %v, want %v", c.s, c.phrase, got, c.want)
		}
	}
}
