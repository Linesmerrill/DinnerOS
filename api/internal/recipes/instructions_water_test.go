package recipes

import (
	"strings"
	"testing"
)

// "Reserve 1½ cups pasta cooking water, then drain" is easy to miss once the
// pot is in the sink: the water is bold in the step and on the step's
// checklist, set aside, and the later step's reserved water is too
// (decision 632).
func TestReservedPastaWaterIsMarkedAndListed(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-pasta", "Cavatappi Pasta", "6", "9", "oz"),
		tacoLine("ing-paste", "Tomato Paste", "1.5", "3", "oz"),
	},
		"Add cavatappi to pot. Cook until al dente, 9-11 minutes. Reserve 1½ cups pasta cooking water, then drain.",
		"Stir in tomato paste and 1 cup reserved pasta cooking water. Bring to a simmer.",
		"Stir in ¾ cup water.")
	in := Annotate(r, 2, nil, false)

	water := func(step int) []Segment {
		var out []Segment
		for _, seg := range in.Steps[step-1].Segments {
			if seg.Kind == SegmentIngredient && seg.Ingredient == NoIngredient {
				out = append(out, seg)
			}
		}
		return out
	}
	if w := water(1); len(w) != 1 || w[0].Name != "Pasta cooking water" || w[0].Text != "1½ cups pasta cooking water" {
		t.Errorf("step 1 water = %+v, want the reserved pasta water marked", w)
	}
	if w := water(2); len(w) != 1 || w[0].Name != "Pasta cooking water" || !strings.Contains(w[0].Text, "1 cup reserved pasta cooking water") {
		t.Errorf("step 2 water = %+v, want the reserved pasta water marked", w)
	}
	if w := water(3); len(w) != 1 || w[0].Name != "Water" {
		t.Errorf("step 3 water = %+v, want plain water marked", w)
	}

	c := in.Checklist
	if it := item(t, c, 1, "Pasta cooking water"); it.AmountText != "1 ½ cups" || it.Prep != "set aside before draining" {
		t.Errorf("step 1 row = %+v", it)
	}
	if it := item(t, c, 2, "Pasta cooking water"); it.AmountText != "1 cup" || it.Prep != "reserved" {
		t.Errorf("step 2 row = %+v", it)
	}
	if it := item(t, c, 3, "Water"); it.AmountText != "¾ cup" || it.Prep != "" {
		t.Errorf("step 3 row = %+v", it)
	}
	// The step's own words still read the same.
	if got := joined(in.Steps[0]); !strings.Contains(got, "Reserve 1½ cups pasta cooking water, then drain.") {
		t.Errorf("step 1 = %q", got)
	}
	if f := CheckSteps(in); len(f) != 0 {
		t.Errorf("findings = %+v", f)
	}
}

// A pot's cooking liquid kept for later is marked and listed the same way;
// "liquid" alone isn't water.
func TestReservedCookingLiquidIsListed(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{tacoLine("ing-potato", "Yukon Gold Potatoes", "12", "18", "oz")},
		"Boil potatoes until tender. Reserve ½ cup potato cooking liquid, then drain.",
		"Mash potatoes with splashes of reserved potato cooking liquid. Add 1 cup liquid smoke marinade.")
	in := Annotate(r, 2, nil, false)
	if it := item(t, in.Checklist, 1, "Potato cooking liquid"); it.AmountText != "½ cup" || it.Prep != "set aside before draining" {
		t.Errorf("row = %+v", it)
	}
	for _, seg := range in.Steps[1].Segments {
		if seg.Ingredient == NoIngredient && seg.Kind == SegmentIngredient && strings.Contains(seg.Text, "smoke") {
			t.Errorf("marked %q as water", seg.Text)
		}
	}
}
