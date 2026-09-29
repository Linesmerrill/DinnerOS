package recipes

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// A left-out ingredient stays in the step, in the step's own words, marked and
// without an amount, and the step says so. Nothing is silently deleted.
func TestAnnotateMarksALeftOutIngredientInsteadOfDeletingIt(t *testing.T) {
	r := instructionRecipe(
		[]RecipeIngredient{
			instructionLine("ing-rice", "Rice", "1", "cup"),
			instructionLine("", "Cilantro", "1/4", "oz"),
		},
		"Fluff the rice and top with cilantro.",
		"Garnish with the cilantro.")
	leftOut := grocery.LeftOutSet{"name:cilantro": {SkipID: "skip-1", Scope: grocery.SkipRecipe}}
	in := AnnotateWith(r, 2, nil, true, leftOut, true)

	first := in.Steps[0]
	if got, want := joined(first), "Fluff 1 cup rice and top with cilantro."; got != want {
		t.Errorf("text = %q, want %q (no amount put in front of what's left out)", got, want)
	}
	segs := ingredientSegments(first)
	if len(segs) != 2 || segs[0].LeftOut || !segs[1].LeftOut || segs[1].Amount != nil || segs[1].Text != "cilantro" {
		t.Errorf("segments = %+v, want rice as usual and cilantro marked left out without an amount", segs)
	}
	if len(first.Notes) != 1 || first.Notes[0].Kind != NoteLeftOut || first.Notes[0].Text != "You leave out the Cilantro." {
		t.Errorf("notes = %+v, want one left-out note", first.Notes)
	}
	if first.LeftOut {
		t.Error("a step that still adds rice is not a left-out step")
	}
	// A step whose only ingredient is left out can be skipped.
	if !in.Steps[1].LeftOut {
		t.Errorf("step 2 = %+v, want it marked left out: it only adds cilantro", in.Steps[1])
	}
	if !in.LeftOutApplied || len(in.Ingredients) != 2 {
		t.Fatalf("ingredients = %+v, want both, with left-out applied", in.Ingredients)
	}
	if st := in.Ingredients[1]; st.IngredientKey != "name:cilantro" || st.LeftOut == nil || st.LeftOut.SkipID != "skip-1" || st.Index != 1 {
		t.Errorf("cilantro state = %+v, want its key and the skip leaving it out", st)
	}
	if st := in.Ingredients[0]; st.IngredientKey != "ing-rice" || st.LeftOut != nil {
		t.Errorf("rice state = %+v, want its catalog key and nothing left out", st)
	}
}

// A catalog ingredient is found under its name spelling too, the way a skip
// made from a free-text line in another recipe spells it.
func TestAnnotateFindsALeftOutCatalogIngredientByName(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{instructionLine("ing-cilantro", "Cilantro", "1/4", "oz")}, "Top with cilantro.")
	in := AnnotateWith(r, 2, nil, true, grocery.LeftOutSet{"name:cilantro": {SkipID: "s", Scope: grocery.SkipAlways}}, true)
	if st := in.Ingredients[0]; st.LeftOut == nil || st.LeftOut.Scope != grocery.SkipAlways {
		t.Errorf("state = %+v, want it left out by the always skip", st)
	}
}

// The crema is a component: a specialty ingredient made from sour cream,
// roasted red peppers, and paprika. Its state says what it's made of, and
// leaving it out keeps its own name in the step instead of the substitute's.
func TestAnnotateDescribesAndLeavesOutAComponent(t *testing.T) {
	spec := &grocery.Specialty{
		ID: "smoky-red-pepper-crema", Key: "smoky red pepper crema", Name: "Smoky Red Pepper Crema",
		Choice: &grocery.Choice{
			Type: grocery.ChoiceStoreAlternative, OptionID: "crema.store", OptionName: "Sour cream with roasted peppers",
			Per: grocery.Measure{Quantity: *quantityPtr(t, "1"), Unit: "tbsp"},
			Components: []grocery.Component{
				{Name: "Sour Cream", Quantity: quantityPtr(t, "2"), Unit: "tsp"},
				{Name: "Roasted Red Peppers", Quantity: quantityPtr(t, "1"), Unit: "tsp"},
				{Name: "Smoked Paprika", Quantity: quantityPtr(t, "1/8"), Unit: "tsp"},
			},
		},
	}
	r := instructionRecipe(
		[]RecipeIngredient{instructionLine("ing-crema", "Smoky Red Pepper Crema", "2", "tbsp")},
		"Drizzle the smoky red pepper crema over the tacos.")
	specs := grocery.Specialties{"ing-crema": spec}

	kept := AnnotateWith(r, 2, specs, true, nil, true)
	c := kept.Ingredients[0].Component
	if c == nil || c.SpecialtyName != "Smoky Red Pepper Crema" || len(c.Parts) != 3 || c.Parts[0] != "4 tsp Sour Cream" {
		t.Fatalf("component = %+v, want the crema and its three parts for 2 Tbsp", c)
	}
	if len(kept.Substitutions) != 1 {
		t.Errorf("substitutions = %+v, want the crema's", kept.Substitutions)
	}

	left := AnnotateWith(r, 2, specs, true, grocery.LeftOutSet{"ing-crema": {SkipID: "skip-crema", Scope: grocery.SkipRecipe}}, true)
	step := left.Steps[0]
	if got, want := joined(step), "Drizzle the smoky red pepper crema over the tacos."; got != want {
		t.Errorf("text = %q, want the step as written: %q", got, want)
	}
	if !step.LeftOut || len(left.Substitutions) != 0 {
		t.Errorf("step = %+v, substitutions = %+v; want the step left out and nothing substituted", step, left.Substitutions)
	}
	if seg := ingredientSegments(step)[0]; !seg.LeftOut || seg.SpecialtyName != "Smoky Red Pepper Crema" || seg.Substituted {
		t.Errorf("segment = %+v, want the crema left out, not substituted", seg)
	}
	if left.Ingredients[0].Component == nil {
		t.Error("a left-out component still says what it is made of, so it can be put back knowingly")
	}
}

func TestAnnotateWithoutLeftOutIsAnnotate(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{instructionLine("", "Cilantro", "1/4", "oz")}, "Top with cilantro.")
	a, b := Annotate(r, 2, nil, true), AnnotateWith(r, 2, nil, true, nil, false)
	if joined(a.Steps[0]) != joined(b.Steps[0]) || b.LeftOutApplied || b.Steps[0].LeftOut {
		t.Errorf("AnnotateWith(nil) differs from Annotate: %+v vs %+v", a.Steps[0], b.Steps[0])
	}
}
