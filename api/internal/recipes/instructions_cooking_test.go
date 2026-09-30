package recipes

import (
	"math/big"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// These cover how a step reads at the stove: short names found, amounts for
// the size being cooked, citrus in wedges, and the protein actually bought.
// The step text is written for the tests, in the style of a meal-kit card.

// tacoLine is a line authored for 2 and 3 servings with 4 and 6 left blank,
// the way meal-kit cards often leave their bigger boxes.
func tacoLine(id, name, two, three, unit string) RecipeIngredient {
	return RecipeIngredient{
		IngredientID: id, Name: name,
		Amounts: []Amount{
			{Servings: 2, Quantity: two, Unit: unit},
			{Servings: 3, Quantity: three, Unit: unit},
			{Servings: 4, Unit: unit},
			{Servings: 6, Unit: unit},
		},
	}
}

func tacoRecipe(lines []RecipeIngredient, steps ...string) Recipe {
	r := instructionRecipe(lines, steps...)
	r.Servings = []int{2, 3, 4, 6}
	return r
}

func TestAnnotateFindsTheShortNamesAStepUses(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{
			tacoLine("ing-onion", "Red Onion", "1", "3/2", "count"),
			tacoLine("ing-pepper", "Long Green Pepper", "1", "3/2", "count"),
			tacoLine("ing-pork", "Ground Pork", "10", "15", "oz"),
			tacoLine("ing-oil", "Cooking Oil", "2", "3", "tsp"),
			tacoLine("ing-black", "Black Pepper", "", "", ""),
			tacoLine("ing-salt", "Salt", "", "", ""),
		},
		"Thinly slice onion. Slice green pepper into strips.",
		"Heat a drizzle of oil. Add pork and cook. Season with salt and pepper.")
	in := Annotate(r, 2, nil, true)
	if got, want := joined(in.Steps[0]), "Thinly slice 1 onion. Slice 1 green pepper into strips."; got != want {
		t.Errorf("step 1 = %q, want %q", got, want)
	}
	if got, want := joined(in.Steps[1]), "Heat a drizzle of oil. Add 10 oz pork and cook. Season with salt and pepper."; got != want {
		t.Errorf("step 2 = %q, want %q", got, want)
	}
	var names []string
	for _, s := range ingredientSegments(in.Steps[1]) {
		names = append(names, s.Name)
	}
	if got, want := strings.Join(names, ","), "Cooking Oil,Ground Pork,Salt,Black Pepper"; got != want {
		t.Errorf("step 2 names = %s, want %s (plain pepper is the black pepper)", got, want)
	}
}

func TestAnnotateFindsABlendByItsShortName(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-sw", "Southwest Spice Blend", "1", "3/2", "tbsp")},
		"Add pork and remaining Southwest Spice.")
	segs := ingredientSegments(Annotate(r, 2, nil, true).Steps[0])
	if len(segs) != 1 || segs[0].Name != "Southwest Spice Blend" || segs[0].Amount != nil {
		t.Errorf("segments = %+v, want the blend marked, without an amount after \"remaining\"", segs)
	}
}

func TestAnnotateScalesASizeTheCardLeftBlank(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-onion", "Red Onion", "1", "3/2", "count")},
		"Thinly slice onion.")
	in := Annotate(r, 6, nil, true)
	if got, want := joined(in.Steps[0]), "Thinly slice 3 onions."; got != want {
		t.Errorf("text = %q, want %q (scaled from 3 servings)", got, want)
	}
	if a := in.Ingredients[0].Amount; a == nil || a.Text() != "3" {
		t.Errorf("list amount = %+v, want 3", a)
	}
}

func TestAnnotateReadsCitrusInWedges(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-lime", "Lime", "1", "3/2", "count")},
		"Quarter lime.",
		"Combine juice from half the lime and a pinch of sugar.",
		"Stir in a squeeze of lime juice.",
		"Serve with remaining lime wedges.")
	for _, tc := range []struct {
		servings int
		want     []string
	}{
		{2, []string{"Quarter 1 lime.", "Combine juice from 2 lime wedges and a pinch of sugar.",
			"Stir in a squeeze of 1 lime wedge.", "Serve with remaining lime wedges."}},
		{6, []string{"Quarter 3 limes.", "Combine juice from 6 lime wedges and a pinch of sugar.",
			"Stir in a squeeze of 3 lime wedges.", "Serve with remaining lime wedges."}},
	} {
		in := Annotate(r, tc.servings, nil, true)
		for i, want := range tc.want {
			if got := joined(in.Steps[i]); got != want {
				t.Errorf("%d servings, step %d = %q, want %q", tc.servings, i+1, got, want)
			}
		}
		if seg := ingredientSegments(in.Steps[1])[0]; seg.Amount == nil || seg.Amount.Unit != "wedge" || !seg.Part {
			t.Errorf("%d servings, half the lime = %+v, want a share in wedges", tc.servings, seg)
		}
	}
}

func TestAnnotateKeepsAFractionsWordsButListsTheShare(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-onion", "Red Onion", "1", "3/2", "count")},
		"Thinly slice onion.",
		"Combine ¼ of the onion with sugar.",
		"Add remaining onion.")
	in := Annotate(r, 2, nil, true)
	if got, want := joined(in.Steps[1]), "Combine ¼ of the onion with sugar."; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if seg := ingredientSegments(in.Steps[1])[0]; seg.Amount == nil || seg.Amount.Text() != "¼" || !seg.Part {
		t.Errorf("segment = %+v, want ¼ as this step's share", seg)
	}
	if seg := ingredientSegments(in.Steps[2])[0]; seg.Amount != nil {
		t.Errorf("remaining = %+v, want no amount", seg)
	}
}

func TestAnnotateDropsTheOtherBoxNote(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-sw", "Southwest Spice Blend", "1", "3/2", "tbsp")},
		"Combine sour cream with ¼ tsp Southwest Spice (½ tsp for 4).")
	if got, want := joined(Annotate(r, 6, nil, true).Steps[0]), "Combine sour cream with ¾ tsp Southwest Spice."; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestAnnotateNamesTheSwappedProtein(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-pork", "Ground Pork", "10", "15", "oz")},
		"Add pork* and spice. Cook until pork is cooked through.",
		"While pork cooks, warm tortillas.")
	in := AnnotateMeal(r, 2, nil, true, nil, false, Swaps{"ing-pork": {Name: "Ground Beef", Factor: big.NewRat(1, 1)}})
	if got, want := joined(in.Steps[0]), "Add 10 oz ground beef and spice. Cook until ground beef is cooked through."; got != want {
		t.Errorf("step 1 = %q, want %q", got, want)
	}
	if got, want := joined(in.Steps[1]), "While ground beef cooks, warm tortillas."; got != want {
		t.Errorf("step 2 = %q, want %q", got, want)
	}
	if in.Ingredients[0].SwapName != "Ground Beef" {
		t.Errorf("swap name = %q, want Ground Beef", in.Ingredients[0].SwapName)
	}
	// A double keeps the name and doubles the amount; the footnote mark goes either way.
	in = AnnotateMeal(r, 2, nil, true, nil, false, Swaps{"ing-pork": {Factor: big.NewRat(2, 1)}})
	if got, want := joined(in.Steps[0]), "Add 20 oz pork and spice. Cook until pork is cooked through."; got != want {
		t.Errorf("double = %q, want %q", got, want)
	}
}

func TestAnnotateMixesABlendOnceAcrossSteps(t *testing.T) {
	spec := &grocery.Specialty{
		ID: "southwest-spice-blend", Key: "southwest spice blend", Name: "Southwest Spice Blend",
		Choice: &grocery.Choice{
			Type: grocery.ChoiceStoreAlternative, OptionID: "o", OptionName: "From the spice rack",
			Per: grocery.Measure{Quantity: *quantityPtr(t, "1"), Unit: "tbsp"},
			Components: []grocery.Component{
				{Name: "Chili Powder", Quantity: quantityPtr(t, "2"), Unit: "tsp"},
				{Name: "Ground Cumin", Quantity: quantityPtr(t, "1"), Unit: "tsp"},
			},
		},
	}
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-sw", "Southwest Spice Blend", "1", "3/2", "tbsp")},
		"Combine sour cream with ¼ tsp Southwest Spice (½ tsp for 4).",
		"Add pork and remaining Southwest Spice.")
	in := Annotate(r, 2, grocery.Specialties{"ing-sw": spec}, true)
	if got, want := joined(in.Steps[0]), "Combine sour cream with ¼ tsp Southwest Spice Blend."; got != want {
		t.Errorf("step 1 = %q, want %q", got, want)
	}
	if n := in.Steps[0].Notes; len(n) != 1 ||
		n[0].Text != "Instead of 1 Tbsp Southwest Spice Blend, mix 2 tsp Chili Powder and 1 tsp Ground Cumin. Use ¼ tsp here and save the rest." {
		t.Errorf("step 1 notes = %+v", n)
	}
	if n := in.Steps[1].Notes; len(n) != 1 || n[0].Text != "Use the rest of the Southwest Spice Blend you mixed." {
		t.Errorf("step 2 notes = %+v", n)
	}
}

func TestMeasureTextTidiesBigSpoonCounts(t *testing.T) {
	for _, tc := range []struct{ q, unit, want string }{
		{"12", "tsp", "4 Tbsp"},
		{"9/2", "tsp", "1 ½ Tbsp"},
		{"2", "tsp", "2 tsp"},
		{"4", "tsp", "4 tsp"},
		{"12", "tbsp", "¾ cup"},
		{"10", "tsp", "3 Tbsp + 1 tsp"},
		{"15/2", "tsp", "2 ½ Tbsp"},
		{"13/2", "tsp", "2 Tbsp + ½ tsp"},
		{"4", "tbsp", "4 Tbsp"},
	} {
		if got := (Measure{Quantity: *quantityPtr(t, tc.q), Unit: tc.unit}).Text(); got != tc.want {
			t.Errorf("%s %s = %q, want %q", tc.q, tc.unit, got, tc.want)
		}
	}
}

func TestBoxNotesSettleForTheSizeBeingCooked(t *testing.T) {
	for _, tc := range []struct {
		text           string
		servings, base int
		want           string
	}{
		{"Cook, 5-7 minutes (7-10 minutes for 4 servings).", 2, 2, "Cook, 5-7 minutes."},
		{"Cook, 5-7 minutes (7-10 minutes for 4 servings).", 4, 2, "Cook, 7-10 minutes."},
		{"Reserve 2 cups pasta water (4 cups for 4 servings), then drain.", 6, 2, "Reserve 4 cups pasta water, then drain."},
		{"Adjust rack to middle position (middle and top positions for 4 servings).", 2, 2, "Adjust rack to middle position."},
		{"Adjust rack to middle position (middle and top positions for 4 servings).", 4, 2, "Adjust rack to middle position (middle and top positions for 4 servings)."},
		{"Stir in salt (2 tsp water and 1½ tsp salt for 4 servings).", 2, 2, "Stir in salt."},
		{"Stir in salt (2 tsp water and 1½ tsp salt for 4 servings).", 4, 2, "Stir in salt (2 tsp water and 1½ tsp salt for 4 servings)."},
		{"Stir (you'll use the rest later).", 4, 2, "Stir (you'll use the rest later)."},
	} {
		if got := boxNotes(tc.text, tc.servings, tc.base); got != tc.want {
			t.Errorf("boxNotes(%q, %d) = %q, want %q", tc.text, tc.servings, got, tc.want)
		}
	}
}

func TestAnnotateCleansTheCardsMarks(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{
			tacoLine("ing-chicken", "Chicken Breasts", "10", "15", "oz"),
			tacoLine("ing-beans", "Black Beans", "1", "3/2", "count"),
			tacoLine("ing-lemon", "Lemon", "1", "3/2", "count"),
		},
		"Pat chicken*  dry. Drain and rinse beans. Melt 1 TBSP butter. Open package of chicken.",
		"Quarter lemon. Add half the lemon zest.")
	in := Annotate(r, 2, nil, true)
	if got, want := joined(in.Steps[0]), "Pat 10 oz chicken dry. Drain and rinse beans. Melt 1 Tbsp butter. Open package of chicken."; got != want {
		t.Errorf("step 1 = %q, want %q", got, want)
	}
	if got, want := joined(in.Steps[1]), "Quarter 1 lemon. Add half the lemon zest."; got != want {
		t.Errorf("step 2 = %q, want %q", got, want)
	}
}

func TestAnnotateGivesNoAmountForAWholePack(t *testing.T) {
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-chicken", "Chicken Breasts", "10", "15", "oz")},
		"Open package of chicken and drain.")
	if got, want := joined(Annotate(r, 2, nil, true).Steps[0]), "Open package of chicken and drain."; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestCountPlural(t *testing.T) {
	for in, want := range map[string]string{
		"lime": "limes", "jalapeño": "jalapeños", "tomato": "tomatoes", "sweet potato": "sweet potatoes",
		"green pepper": "green peppers", "radish": "radishes",
	} {
		if got := countPlural(in); got != want {
			t.Errorf("countPlural(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBouillonBaseStaysInTeaspoons(t *testing.T) {
	c := grocery.Component{Name: "Smoky Chipotle Bouillon Base", Quantity: quantityPtr(t, "1"), Unit: "tsp"}
	if got := scaledComponent(c, big.NewRat(3, 1)).Text(); got != "3 tsp" {
		t.Errorf("3 packets = %q, want 3 tsp", got)
	}
	paste := grocery.Component{Name: "Tomato Paste", Quantity: quantityPtr(t, "5"), Unit: "tsp"}
	if got := scaledComponent(paste, big.NewRat(3, 1)).Text(); got != "5 Tbsp" {
		t.Errorf("tomato paste = %q, want 5 Tbsp", got)
	}
}

// Two packets of Tex-Mex Paste: 4 Tbsp of paste, made from 2 tsp of the
// chipotle base (1 a packet) and 10 tsp of tomato paste, which reads as
// something you can measure.
func TestTwoPacketsOfTexMexPaste(t *testing.T) {
	spec := &grocery.Specialty{
		ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste",
		UnitSizes: []grocery.UnitSize{{Unit: "count", Quantity: *quantityPtr(t, "2"), SizeUnit: "tbsp"}},
		Choice: &grocery.Choice{
			Type: grocery.ChoiceStoreAlternative, OptionID: "tex-mex-paste.store", OptionName: "Chipotle base",
			Per: grocery.Measure{Quantity: *quantityPtr(t, "1"), Unit: "count"},
			Components: []grocery.Component{
				{Name: "Smoky Chipotle Bouillon Base", Quantity: quantityPtr(t, "1"), Unit: "tsp"},
				{Name: "Tomato Paste", Quantity: quantityPtr(t, "5"), Unit: "tsp"},
			},
		},
	}
	r := tacoRecipe(
		[]RecipeIngredient{tacoLine("ing-texmex", "Tex-Mex Paste", "2", "3", "count")},
		"Stir in Tex-Mex paste.")
	in := Annotate(r, 2, grocery.Specialties{"ing-texmex": spec}, true)
	want := "Instead of 4 Tbsp Tex-Mex Paste, mix 2 tsp Smoky Chipotle Bouillon Base and 3 Tbsp + 1 tsp Tomato Paste."
	if n := in.Steps[0].Notes; len(n) != 1 || n[0].Text != want {
		t.Errorf("note = %+v, want %q", n, want)
	}
	if c := in.Ingredients[0].Component; c == nil || strings.Join(c.Parts, " | ") != "2 tsp Smoky Chipotle Bouillon Base | 3 Tbsp + 1 tsp Tomato Paste" {
		t.Errorf("parts = %+v", c)
	}
}
