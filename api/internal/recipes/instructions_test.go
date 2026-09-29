package recipes

import (
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// --- helpers ------------------------------------------------------------------

func instructionLine(id, name, quantity, unit string) RecipeIngredient {
	return RecipeIngredient{
		IngredientID: id, Name: name,
		Amounts: []Amount{
			{Servings: 2, Quantity: quantity, Unit: unit},
			{Servings: 4, Quantity: doubled(quantity), Unit: unit},
		},
	}
}

// doubled returns twice an exact quantity, for the 4-serving amount.
func doubled(q string) string {
	if q == "" {
		return ""
	}
	parsed, err := ingredients.ParseQuantity(q)
	if err != nil {
		return q
	}
	return parsed.Mul(ingredients.NewQuantity(2, 1)).String()
}

func instructionRecipe(lines []RecipeIngredient, steps ...string) Recipe {
	r := Recipe{ID: "r1", Name: "Test Dinner", Servings: []int{2, 4}, Ingredients: lines}
	for i, text := range steps {
		r.Steps = append(r.Steps, Step{Index: i + 1, Text: text})
	}
	return r
}

func quantityPtr(t *testing.T, s string) *ingredients.Quantity {
	t.Helper()
	q, err := ingredients.ParseQuantity(s)
	if err != nil {
		t.Fatalf("ParseQuantity(%q) error = %v", s, err)
	}
	return &q
}

// segmentTexts returns the step's segments as "kind:text" for comparison.
func segmentTexts(step InstructionStep) []string {
	out := make([]string, 0, len(step.Segments))
	for _, s := range step.Segments {
		out = append(out, string(s.Kind)+":"+s.Text)
	}
	return out
}

func ingredientSegments(step InstructionStep) []Segment {
	var out []Segment
	for _, s := range step.Segments {
		if s.Kind == SegmentIngredient {
			out = append(out, s)
		}
	}
	return out
}

func joined(step InstructionStep) string {
	var b strings.Builder
	for _, s := range step.Segments {
		b.WriteString(s.Text)
	}
	return b.String()
}

// --- highlighting -------------------------------------------------------------

func TestAnnotateHighlightsIngredientsWithScaledAmounts(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{
			instructionLine("ing-gochujang", "Gochujang", "1", "tbsp"),
			instructionLine("ing-scallion", "Scallion", "2", "count"),
		},
		"Add the gochujang, then the scallions.")

	for _, tc := range []struct {
		servings int
		want     string
	}{
		{2, "Add 1 Tbsp gochujang, then 2 scallions."},
		{4, "Add 2 Tbsp gochujang, then 4 scallions."},
	} {
		in := Annotate(r, tc.servings, nil, true)
		if got := joined(in.Steps[0]); got != tc.want {
			t.Errorf("servings %d text = %q, want %q", tc.servings, got, tc.want)
		}
		if in.Steps[0].Text != tc.want || in.Steps[0].Original == "" {
			t.Errorf("servings %d step = %+v, want rendered text and the original kept", tc.servings, in.Steps[0])
		}
	}
	segs := ingredientSegments(Annotate(r, 2, nil, true).Steps[0])
	if len(segs) != 2 || segs[0].Name != "Gochujang" || !segs[0].Spicy || segs[1].Spicy {
		t.Fatalf("segments = %+v, want gochujang spicy and scallion not", segs)
	}
	if segs[0].Amount == nil || segs[0].Amount.Text() != "1 Tbsp" {
		t.Errorf("gochujang amount = %+v, want 1 Tbsp", segs[0].Amount)
	}
}

func TestAnnotateMarksSpicyIngredientWithoutAnAmount(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-flakes", "Chili Flakes", "", "")},
		"Sprinkle a pinch of chili flakes over the top.")
	segs := ingredientSegments(Annotate(r, 2, nil, true).Steps[0])
	if len(segs) != 1 || segs[0].Amount != nil || !segs[0].Spicy || segs[0].Text != "chili flakes" {
		t.Fatalf("segments = %+v, want one spicy segment with no amount", segs)
	}
	// Nothing changed, so the original text is not repeated.
	if step := Annotate(r, 2, nil, true).Steps[0]; step.Original != "" || step.Text != r.Steps[0].Text {
		t.Errorf("step = %+v, want the recipe's own text and no original", step)
	}
}

func TestAnnotateSameIngredientTwiceInOneStepShowsTheAmountOnce(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-butter", "Butter", "2", "tbsp")},
		"Melt the butter, then brush with butter.",
		"Finish with butter.")
	in := Annotate(r, 2, nil, true)
	first := ingredientSegments(in.Steps[0])
	if len(first) != 2 {
		t.Fatalf("segments = %+v, want both mentions", first)
	}
	if first[0].Amount == nil || first[1].Amount != nil {
		t.Errorf("amounts = %+v / %+v, want only the first to carry one", first[0].Amount, first[1].Amount)
	}
	if got, want := joined(in.Steps[0]), "Melt 2 Tbsp butter, then brush with butter."; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	// A new step starts over: the amount is shown again.
	if second := ingredientSegments(in.Steps[1]); len(second) != 1 || second[0].Amount == nil {
		t.Errorf("second step = %+v, want the amount again", second)
	}
}

func TestAnnotateIgnoresIngredientsTheRecipeDoesNotList(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-rice", "Rice", "1", "cup")},
		"Stir in the rice, then add saffron and truffle zest.")
	step := Annotate(r, 2, nil, true).Steps[0]
	segs := ingredientSegments(step)
	if len(segs) != 1 || segs[0].Name != "Rice" {
		t.Fatalf("segments = %+v, want only the listed ingredient", segs)
	}
	if !strings.Contains(joined(step), "saffron and truffle zest") {
		t.Errorf("text = %q, want the unlisted names left alone", joined(step))
	}
}

func TestAnnotateReplacesAnAmountTheStepAlreadyWrote(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-gochujang", "Gochujang", "1", "tbsp")},
		"Whisk 1 Tbsp gochujang into the sauce.")
	if got, want := joined(Annotate(r, 4, nil, true).Steps[0]), "Whisk 2 Tbsp gochujang into the sauce."; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestAnnotateKeepsWordsItCannotRecognizeAsAnAmount(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-rice", "Rice", "1", "cup")},
		"Fluff the cooked rice.")
	if got, want := joined(Annotate(r, 2, nil, true).Steps[0]), "Fluff the cooked 1 cup rice."; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestAnnotateMatchesPluralAndSingularForms(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{
			instructionLine("ing-tomato", "Cherry Tomatoes", "4", "oz"),
			instructionLine("ing-potato", "Potato", "1", "count"),
		},
		"Halve the cherry tomato and quarter the potatoes.")
	segs := ingredientSegments(Annotate(r, 2, nil, true).Steps[0])
	if len(segs) != 2 || segs[0].Name != "Cherry Tomatoes" || segs[1].Name != "Potato" {
		t.Fatalf("segments = %+v, want both matched across plural forms", segs)
	}
}

// --- substitutions ------------------------------------------------------------

func storeSpecialty(t *testing.T, components ...grocery.Component) *grocery.Specialty {
	t.Helper()
	return &grocery.Specialty{
		ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste",
		UnitSizes: []grocery.UnitSize{{Unit: "count", Quantity: *quantityPtr(t, "1"), SizeUnit: "tbsp"}},
		Choice: &grocery.Choice{
			Type: grocery.ChoiceStoreAlternative, OptionID: "opt-store", OptionName: "Chipotle tomato base",
			Per: grocery.Measure{Quantity: *quantityPtr(t, "1"), Unit: "tbsp"}, Components: components,
		},
	}
}

func TestAnnotateStoreAlternativeWithOneIngredientSwapsTheNameAndAmount(t *testing.T) {
	spec := storeSpecialty(t, grocery.Component{Name: "Tomato Paste", Quantity: quantityPtr(t, "2/3"), Unit: "tbsp"})
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-texmex", "Tex-Mex Paste", "3", "tbsp")},
		"Stir in the Tex-Mex Paste.")
	in := Annotate(r, 2, grocery.Specialties{"ing-texmex": spec}, true)
	if got, want := joined(in.Steps[0]), "Stir in 2 Tbsp Tomato Paste."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	seg := ingredientSegments(in.Steps[0])[0]
	if !seg.Substituted || seg.Name != "Tomato Paste" || seg.SpecialtyName != "Tex-Mex Paste" {
		t.Errorf("segment = %+v, want the substitute named and the specialty kept", seg)
	}
	if len(in.Steps[0].Notes) != 0 {
		t.Errorf("notes = %+v, want none for a one-ingredient swap", in.Steps[0].Notes)
	}
	if len(in.Substitutions) != 1 || in.Substitutions[0].Source != ChoiceSourceHousehold {
		t.Fatalf("substitutions = %+v, want one from the household", in.Substitutions)
	}
}

func TestAnnotateStoreAlternativeWithSeveralIngredientsExplainsItself(t *testing.T) {
	spec := storeSpecialty(t,
		grocery.Component{Name: "Tomato Paste", Quantity: quantityPtr(t, "2"), Unit: "tsp"},
		grocery.Component{Name: "Chili Powder", Quantity: quantityPtr(t, "1/2"), Unit: "tsp"})
	spec.Choice.Strategy = "similar"
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-texmex", "Tex-Mex Paste", "2", "tbsp")},
		"Stir in the Tex-Mex Paste.")
	in := Annotate(r, 2, grocery.Specialties{"ing-texmex": spec}, true)
	step := in.Steps[0]
	if got, want := joined(step), "Stir in 2 Tbsp Tex-Mex Paste."; got != want {
		t.Fatalf("text = %q, want the original name kept: %q", got, want)
	}
	if len(step.Notes) != 1 || !strings.Contains(step.Notes[0].Text, "4 tsp Tomato Paste") ||
		!strings.Contains(step.Notes[0].Text, "1 tsp Chili Powder") {
		t.Fatalf("notes = %+v, want both scaled ingredients spelled out", step.Notes)
	}
	if seg := ingredientSegments(step)[0]; !seg.Substituted || !seg.Spicy {
		t.Errorf("segment = %+v, want it marked substituted and spicy (chili powder)", seg)
	}
	if in.Substitutions[0].Source != ChoiceSourceStrategy {
		t.Errorf("source = %q, want the strategy", in.Substitutions[0].Source)
	}
}

func TestAnnotateStoreAlternativeWithoutAConvertibleAmount(t *testing.T) {
	spec := storeSpecialty(t, grocery.Component{Name: "Tomato Paste", Quantity: quantityPtr(t, "2"), Unit: "tsp"})
	// The recipe measures the packet in ounces; the specialty only knows
	// "1 count = 1 Tbsp", so no exact conversion exists.
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-texmex", "Tex-Mex Paste", "1", "oz")},
		"Stir in the Tex-Mex Paste.")
	step := Annotate(r, 2, grocery.Specialties{"ing-texmex": spec}, true).Steps[0]
	if len(step.Notes) != 1 || !strings.Contains(step.Notes[0].Text, "Tomato Paste") {
		t.Fatalf("notes = %+v, want the swap spelled out when the amount can't convert", step.Notes)
	}
	if seg := ingredientSegments(step)[0]; seg.Name != "Tex-Mex Paste" {
		t.Errorf("segment = %+v, want the original name kept", seg)
	}
}

func TestAnnotateHouseMadeBatchConvertsThePacketAndSaysSo(t *testing.T) {
	spec := &grocery.Specialty{
		ID: "southwest-spice-blend", Key: "southwest spice blend", Name: "Southwest Spice Blend",
		UnitSizes: []grocery.UnitSize{{Unit: "count", Quantity: *quantityPtr(t, "1"), SizeUnit: "tbsp"}},
		Choice: &grocery.Choice{
			Type: grocery.ChoiceHouseMadeBatch, OptionID: "opt-batch", OptionName: "House blend",
			Yield: grocery.Measure{Quantity: *quantityPtr(t, "12"), Unit: "tbsp"},
		},
	}
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-sw", "Southwest Spice Blend", "1", "count")},
		"Toss with the Southwest Spice Blend.")
	in := Annotate(r, 2, grocery.Specialties{"ing-sw": spec}, true)
	if got, want := joined(in.Steps[0]), "Toss with 1 Tbsp Southwest Spice Blend."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if len(in.Steps[0].Notes) != 1 || !strings.Contains(in.Steps[0].Notes[0].Text, "house-made") {
		t.Fatalf("notes = %+v, want the batch conversion explained", in.Steps[0].Notes)
	}
	if len(in.Substitutions) != 1 || in.Substitutions[0].Type != grocery.ChoiceHouseMadeBatch {
		t.Errorf("substitutions = %+v, want the batch", in.Substitutions)
	}
}

func TestAnnotateMatchesASpecialtyAlias(t *testing.T) {
	spec := &grocery.Specialty{
		ID: "szechuan-paste", Key: "szechuan paste", Name: "Szechuan Paste", Aliases: []string{"Sichuan Paste"},
		Choice: &grocery.Choice{
			Type: grocery.ChoiceStoreAlternative, OptionID: "o", OptionName: "Chili garlic sauce",
			Per:        grocery.Measure{Quantity: *quantityPtr(t, "1"), Unit: "tbsp"},
			Components: []grocery.Component{{Name: "Chili Garlic Sauce", Quantity: quantityPtr(t, "1"), Unit: "tbsp"}},
		},
	}
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-sz", "Szechuan Paste", "1", "tbsp")},
		"Whisk the Sichuan Paste into the sauce.")
	step := Annotate(r, 2, grocery.Specialties{"ing-sz": spec}, true).Steps[0]
	segs := ingredientSegments(step)
	if len(segs) != 1 || segs[0].Name != "Chili Garlic Sauce" || !segs[0].Spicy {
		t.Fatalf("segments = %+v, want the alias matched and swapped", segs)
	}
}

func TestAnnotateWithoutAChoiceLeavesTheStepAloneAndReportsIt(t *testing.T) {
	spec := &grocery.Specialty{ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste"}
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-texmex", "Tex-Mex Paste", "1", "tbsp")},
		"Stir in the Tex-Mex Paste.")
	in := Annotate(r, 2, grocery.Specialties{"ing-texmex": spec}, true)
	if got, want := joined(in.Steps[0]), "Stir in 1 Tbsp Tex-Mex Paste."; got != want {
		t.Errorf("text = %q, want the card's own ingredient: %q", got, want)
	}
	seg := ingredientSegments(in.Steps[0])[0]
	if seg.Substituted || seg.SpecialtyID != "tex-mex-paste" {
		t.Errorf("segment = %+v, want it flagged as an unchosen specialty", seg)
	}
	if len(in.Unchosen) != 1 || len(in.Substitutions) != 0 {
		t.Errorf("unchosen = %+v, substitutions = %+v, want one unchosen and no substitution", in.Unchosen, in.Substitutions)
	}
}

func TestAnnotateAsIsKeepsTheIngredient(t *testing.T) {
	spec := &grocery.Specialty{
		ID: "ponzu", Key: "ponzu sauce", Name: "Ponzu Sauce",
		Choice: &grocery.Choice{Type: grocery.ChoiceAsIs, OptionID: "as_is"},
	}
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-ponzu", "Ponzu Sauce", "1", "tbsp")},
		"Finish with the Ponzu Sauce.")
	in := Annotate(r, 2, grocery.Specialties{"ing-ponzu": spec}, true)
	if seg := ingredientSegments(in.Steps[0])[0]; seg.Substituted || seg.Name != "Ponzu Sauce" {
		t.Errorf("segment = %+v, want it untouched", seg)
	}
	if len(in.Unchosen) != 0 || len(in.Substitutions) != 0 {
		t.Errorf("unchosen = %+v, substitutions = %+v, want neither", in.Unchosen, in.Substitutions)
	}
}

func TestAnnotateAmountsComeFromTheAuthoredServingSizeOnly(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-rice", "Rice", "1", "cup")},
		"Cook the rice.")
	if segs := ingredientSegments(Annotate(r, 3, nil, true).Steps[0]); len(segs) != 1 || segs[0].Amount != nil {
		t.Errorf("segments = %+v, want no amount for a size the recipe wasn't authored at", segs)
	}
}

func TestGroceryLinesUseTheAuthoredAmounts(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{
		instructionLine("ing-rice", "Rice", "1", "cup"),
		instructionLine("", "Flaky Salt", "", ""),
	})
	lines := GroceryLines(r, 4)
	if len(lines) != 2 || lines[0].Quantity == nil || lines[0].Quantity.String() != "2" || lines[0].UnitCode != "cup" {
		t.Fatalf("lines = %+v, want the 4-serving amount", lines)
	}
	if lines[1].IngredientKey != "name:flaky salt" || lines[1].Quantity != nil {
		t.Errorf("line = %+v, want a name key and no amount", lines[1])
	}
}

func TestSegmentTextsJoinToTheStep(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-gochujang", "Gochujang", "1", "tbsp")},
		"Add the gochujang.")
	step := Annotate(r, 2, nil, true).Steps[0]
	if got := segmentTexts(step); len(got) != 3 || got[0] != "text:Add " || got[2] != "text:." {
		t.Fatalf("segments = %v, want text, ingredient, text", got)
	}
	if joined(step) != step.Text {
		t.Errorf("joined = %q, want the step's text %q", joined(step), step.Text)
	}
}

// A card that splits an ingredient across steps writes each step's share,
// with the other box size in parentheses. The step shows its share for the
// size being cooked, not the recipe's total: "Melt 7 Tbsp butter" twice was
// the bug. A later "remaining butter" gets no amount rather than the total.
func TestAnnotateKeepsEachStepsShare(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-butter", "Butter", "3", "tbsp")},
		"Melt 1 TBSP butter (2 TBSP for 4 servings) in a large pan.",
		"Stir in 2 TBSP butter (4 TBSP for 4 servings) until glossy.",
		"Top with the remaining butter.")
	two := Annotate(r, 2, nil, true)
	for i, want := range []string{
		"Melt 1 Tbsp butter in a large pan.",
		"Stir in 2 Tbsp butter until glossy.",
		"Top with the remaining butter.",
	} {
		if got := joined(two.Steps[i]); got != want {
			t.Errorf("2 servings, step %d = %q, want %q", i+1, got, want)
		}
	}
	first := ingredientSegments(two.Steps[0])
	if len(first) != 1 || !first[0].Part || first[0].Amount == nil || first[0].Amount.Text() != "1 Tbsp" {
		t.Errorf("step 1 segment = %+v, want a 1 Tbsp share", first)
	}
	if last := ingredientSegments(two.Steps[2]); len(last) != 1 || last[0].Amount != nil {
		t.Errorf("remaining butter = %+v, want no amount", last)
	}

	four := Annotate(r, 4, nil, true)
	if got, want := joined(four.Steps[0]), "Melt 2 Tbsp butter in a large pan."; got != want {
		t.Errorf("4 servings, step 1 = %q, want %q", got, want)
	}
	if got, want := joined(four.Steps[1]), "Stir in 4 Tbsp butter until glossy."; got != want {
		t.Errorf("4 servings, step 2 = %q, want %q", got, want)
	}
}

// Without the parenthetical, a share for a bigger box grows the way the
// recipe's own amounts do.
func TestAnnotateScalesAShareByTheRecipesOwnAmounts(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-butter", "Butter", "7", "tbsp")},
		"Dice 6 TBSP butter into pieces.",
		"Grease a baking dish with 1 TBSP butter.")
	two := Annotate(r, 2, nil, true)
	if got, want := joined(two.Steps[0]), "Dice 6 Tbsp butter into pieces."; got != want {
		t.Errorf("step 1 = %q, want %q", got, want)
	}
	if got, want := joined(two.Steps[1]), "Grease a baking dish with 1 Tbsp butter."; got != want {
		t.Errorf("step 2 = %q, want %q", got, want)
	}
	if got, want := joined(Annotate(r, 4, nil, true).Steps[1]), "Grease a baking dish with 2 Tbsp butter."; got != want {
		t.Errorf("4 servings, step 2 = %q, want %q", got, want)
	}
}

// Meal-kit cards often write fractions with the fraction slash (U+2044).
func TestAnnotateReadsTheFractionSlash(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-sugar", "Sugar", "1/4", "tsp")},
		"Combine coleslaw, 1⁄4 tsp sugar (1⁄2 tsp for 4 servings), and salt.")
	if got, want := joined(Annotate(r, 2, nil, true).Steps[0]), "Combine coleslaw, ¼ tsp sugar, and salt."; got != want {
		t.Errorf("2 servings = %q, want %q", got, want)
	}
	if got, want := joined(Annotate(r, 4, nil, true).Steps[0]), "Combine coleslaw, ½ tsp sugar, and salt."; got != want {
		t.Errorf("4 servings = %q, want %q", got, want)
	}
}

// Packets read as kitchen measures: tomato paste is 2 Tbsp a packet, stock
// concentrate 1 tsp (or a bouillon cube), in the steps and the ingredient list.
func TestAnnotateReadsPacketsAsKitchenMeasures(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{
			instructionLine("ing-paste", "Tomato Paste", "1", "count"),
			instructionLine("ing-stock", "Beef Stock Concentrate", "1", "count"),
		},
		"Add tomato paste to the pan. Stir in stock concentrate.")
	two := Annotate(r, 2, nil, true)
	if got, want := joined(two.Steps[0]), "Add 2 Tbsp tomato paste to the pan. Stir in 1 tsp (or 1 bouillon cube) stock concentrate."; got != want {
		t.Errorf("2 servings = %q, want %q", got, want)
	}
	if got := two.Ingredients[0].Amount.Text(); got != "2 Tbsp" {
		t.Errorf("tomato paste amount = %q, want 2 Tbsp", got)
	}
	four := Annotate(r, 4, nil, true)
	if got := four.Ingredients[1].Amount.Text(); got != "2 tsp (or 2 bouillon cubes)" {
		t.Errorf("4 servings stock = %q", got)
	}
}

// A mention inside a hyphenated word gets no amount written into it.
func TestAnnotateLeavesCompoundWordsWhole(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-sriracha", "Sriracha", "1", "tsp")},
		"Add drained noodles and soy-sriracha sauce.")
	if got, want := joined(Annotate(r, 2, nil, true).Steps[0]), "Add drained noodles and soy-sriracha sauce."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A household that uses a bouillon base for stock concentrate still scoops
// its own measure: 1 tsp a packet, whatever the option's authored amount.
func TestAnnotateStockConcentrateSubstituteKeepsTheKitchenMeasure(t *testing.T) {
	spec := storeSpecialty(t, grocery.Component{Name: "Beef Bouillon Base", Quantity: quantityPtr(t, "1/2"), Unit: "tsp"})
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-stock", "Beef Stock Concentrate", "1", "count")},
		"Stir in stock concentrate and water.")
	two := Annotate(r, 2, grocery.Specialties{"ing-stock": spec}, true)
	if got, want := joined(two.Steps[0]), "Stir in 1 tsp Beef Bouillon Base and water."; got != want {
		t.Errorf("2 servings = %q, want %q", got, want)
	}
	if got := joined(Annotate(r, 4, grocery.Specialties{"ing-stock": spec}, true).Steps[0]); got != "Stir in 2 tsp Beef Bouillon Base and water." {
		t.Errorf("4 servings = %q", got)
	}
	if got := two.Ingredients[0].Amount.Text(); got != "1 tsp" {
		t.Errorf("ingredient amount = %q, want 1 tsp", got)
	}
}

// "A squeeze of lime juice" is one wedge of a lime the card already quartered.
func TestAnnotateSqueezeIsOneWedge(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-lime", "Lime", "1", "count"), instructionLine("ing-orange", "Orange", "1", "count")},
		"Stir in juice from orange and a squeeze of lime juice. Bring to a simmer.")
	if got, want := joined(Annotate(r, 2, nil, true).Steps[0]), "Stir in juice from 1 orange and a squeeze of 1 lime wedge. Bring to a simmer."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Nothing chosen for a specialty yet: its packet reads as what it holds, and
// the step says how to make that much.
func TestAnnotateUnchosenSpecialtyGivesAmountAndRecipe(t *testing.T) {
	q := func(s string) *ingredients.Quantity { return quantityPtr(t, s) }
	spec := &grocery.Specialty{
		ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste",
		UnitSizes: []grocery.UnitSize{{Unit: "count", Quantity: *q("2"), SizeUnit: "tbsp"}},
		HowToMake: &grocery.HowToMake{
			Yield: grocery.Measure{Quantity: *q("8"), Unit: "tbsp"},
			Components: []grocery.Component{
				{Name: "Tomato Paste", Quantity: q("8"), Unit: "tbsp"},
				{Name: "Adobo Sauce", Quantity: q("1"), Unit: "tbsp"},
				{Name: "Ground Cumin", Quantity: q("2"), Unit: "tsp"},
			},
		},
	}
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-texmex", "Tex-Mex Paste", "1", "count")},
		"Stir in Tex-Mex paste.")
	in := Annotate(r, 2, grocery.Specialties{"ing-texmex": spec}, true)
	if got, want := joined(in.Steps[0]), "Stir in 2 Tbsp Tex-Mex paste."; got != want {
		t.Errorf("step = %q, want %q", got, want)
	}
	want := "To make 2 Tbsp Tex-Mex Paste, stir together 2 Tbsp tomato paste, ¾ tsp adobo sauce, and ½ tsp ground cumin."
	if len(in.Steps[0].Notes) != 1 || in.Steps[0].Notes[0].Text != want {
		t.Errorf("notes = %+v, want %q", in.Steps[0].Notes, want)
	}
}

func TestAnnotateSqueezeWithNonBreakingSpace(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-lime", "Lime", "1", "count")},
		"Add a squeeze of lime juice.")
	if got := joined(Annotate(r, 2, nil, true).Steps[0]); got != "Add a squeeze of 1 lime wedge." {
		t.Errorf("got %q", got)
	}
}
