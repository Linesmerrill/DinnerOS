package recipes

import (
	"strings"
	"testing"
)

// sweetChiliPork is Sweet Chili Pork Bowls as the card writes it: butter and
// sugar are measured in the steps and missing from the ingredient list.
func sweetChiliPork() Recipe {
	return instructionRecipe([]RecipeIngredient{
		instructionLine("ing-pork", "Ground Pork", "10", "oz"),
		instructionLine("ing-rice", "Jasmine Rice", "3/4", "cup"),
		instructionLine("ing-peanuts", "Peanuts", "1/2", "oz"),
		instructionLine("ing-lime", "Lime", "1", "count"),
	},
		"While rice cooks, heat a large pan. Add peanuts, 1 tsp sugar (2 tsp for 4 servings), and 2 TBSP water.",
		"Add pork; season with salt and pepper. Stir in cooked veggies, then add 1 TBSP butter (2 TBSP for 4 servings). Cook 1-2 minutes more.",
		"Fluff rice with a fork; stir in 1 TBSP butter (2 TBSP for 4 servings) and lime zest to taste.",
	)
}

// stepIngredient returns the step's bolded mention of name, or nil.
func stepIngredient(st InstructionStep, name string) *Segment {
	for i, seg := range st.Segments {
		if seg.Kind == SegmentIngredient && seg.Name == name {
			return &st.Segments[i]
		}
	}
	return nil
}

func TestStepOnlyButterIsBoldedAndTotaled(t *testing.T) {
	for _, tc := range []struct {
		servings     int
		share, total string
		sugarShare   string
	}{
		{2, "1 Tbsp", "2 Tbsp", "1 tsp"},
		{4, "2 Tbsp", "4 Tbsp", "2 tsp"},
	} {
		in := Annotate(sweetChiliPork(), tc.servings, nil, false)
		for _, step := range []int{1, 2} {
			seg := stepIngredient(in.Steps[step], "Butter")
			if seg == nil {
				t.Fatalf("%d servings: step %d doesn't bold butter: %v", tc.servings, step+1, segmentTexts(in.Steps[step]))
			}
			if seg.Amount == nil || seg.Amount.Text() != tc.share || !seg.Part {
				t.Errorf("%d servings: step %d butter = %+v, want its share %s", tc.servings, step+1, seg.Amount, tc.share)
			}
			if strings.Contains(in.Steps[step].Text, "for 4 servings") {
				t.Errorf("%d servings: step %d still has the card's note: %q", tc.servings, step+1, in.Steps[step].Text)
			}
		}
		if seg := stepIngredient(in.Steps[0], "Sugar"); seg == nil || seg.Amount == nil || seg.Amount.Text() != tc.sugarShare {
			t.Errorf("%d servings: sugar = %+v, want %s", tc.servings, seg, tc.sugarShare)
		}
		var ready *CookStepItem
		for i, it := range in.Checklist.ByStep[0].Items {
			if it.Name == "Butter" {
				ready = &in.Checklist.ByStep[0].Items[i]
			}
		}
		if ready == nil || ready.AmountText != tc.total {
			t.Errorf("%d servings: Have Ready butter = %+v, want %s", tc.servings, ready, tc.total)
		}
	}
}

func TestStepStaplesLeaveTheRecipesOwnLinesAlone(t *testing.T) {
	r := sweetChiliPork()
	got := withStepStaples(r)
	if len(r.Ingredients) != 4 {
		t.Fatalf("the recipe itself changed: %d lines", len(r.Ingredients))
	}
	if len(got.Ingredients) != 6 || got.Ingredients[4].Name != "Butter" || got.Ingredients[5].Name != "Sugar" {
		t.Fatalf("lines = %v, want the recipe's four, then Butter and Sugar", got.Ingredients)
	}
	if b := got.Ingredients[4]; !b.PantryStaple || b.Amounts[0].Quantity != "2" || b.Amounts[1].Quantity != "4" || b.Amounts[0].Unit != "tbsp" {
		t.Errorf("Butter = %+v, want 2 Tbsp for 2 and 4 Tbsp for 4", b)
	}
	// Positions the client matches by don't move.
	in := Annotate(r, 2, nil, false)
	for i, st := range in.Ingredients[:4] {
		if st.Index != i || st.Name != r.Ingredients[i].Name {
			t.Errorf("ingredient %d = %+v", i, st)
		}
	}
	// The grocery list never buys them.
	if lines := GroceryLines(r, 2); len(lines) != 4 {
		t.Errorf("grocery lines = %d, want 4", len(lines))
	}
}

func TestStepStaplesOnlyWhenMeasuredAndUnlisted(t *testing.T) {
	cases := map[string]Recipe{
		"listed": instructionRecipe([]RecipeIngredient{instructionLine("ing-butter", "Butter", "2", "tbsp")},
			"Stir in 1 TBSP butter (2 TBSP for 4 servings)."),
		"peanut butter listed": instructionRecipe([]RecipeIngredient{instructionLine("ing-pb", "Peanut Butter", "2", "tbsp")},
			"Whisk 2 TBSP peanut butter with water."),
		"no amount":   instructionRecipe(nil, "Melt butter in a pan."),
		"brown sugar": instructionRecipe(nil, "Add 1 tsp brown sugar."),
		"a pat":       instructionRecipe(nil, "Top with a pat of butter."),
	}
	for name, r := range cases {
		if got := withStepStaples(r); len(got.Ingredients) != len(r.Ingredients) {
			t.Errorf("%s: added %v", name, got.Ingredients[len(r.Ingredients):])
		}
	}
}

func TestStepStapleScalesASizeWithoutANote(t *testing.T) {
	r := instructionRecipe(nil, "Stir in 1 TBSP butter.", "Add 1 TBSP butter (2 TBSP for 4 servings).")
	r.Servings = []int{2, 3, 4}
	got := withStepStaples(r)
	if len(got.Ingredients) != 1 {
		t.Fatalf("lines = %v", got.Ingredients)
	}
	want := map[int]string{2: "2", 3: "3", 4: "4"}
	for _, a := range got.Ingredients[0].Amounts {
		if a.Quantity != want[a.Servings] {
			t.Errorf("%d servings = %s, want %s", a.Servings, a.Quantity, want[a.Servings])
		}
	}
}

// "• 4 SERVINGS: Use 4 TBSP butter" is the bigger box's amount, not more
// butter for the smallest.
func TestStepStapleReadsSizeBullets(t *testing.T) {
	r := instructionRecipe(nil,
		"Add 2 TBSP butter to pan with veggies. • 4 SERVINGS: Use 4 TBSP butter and 1¾ cups water.",
		"Place 1 TBSP butter in a small bowl; microwave until melted. Brush biscuits with melted butter.",
		"Stir in 2 TBSP butter.\n4 SERVINGS: Use 3 TBSP butter.",
	)
	r.Servings = []int{2, 3, 4}
	got := withStepStaples(r)
	if len(got.Ingredients) != 1 {
		t.Fatalf("lines = %v", got.Ingredients)
	}
	want := map[int]string{2: "5", 3: "15/2", 4: "9"}
	for _, a := range got.Ingredients[0].Amounts {
		if a.Quantity != want[a.Servings] {
			t.Errorf("%d servings = %s, want %s", a.Servings, a.Quantity, want[a.Servings])
		}
	}
}
