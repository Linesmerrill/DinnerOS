package recipes

import "testing"

func TestCleanStepTextStripsHTML(t *testing.T) {
	in := `<ul> <li> <p>Add a <strong>drizzle of oil </strong>to a large pot of <strong>water</strong> and bring to a boil.</p> </li> <li><p>Stir in <strong>¼</strong> cup water (<span style="color: rgb(0, 84, 44)">½ cup for 4 servings</span>) &amp; stir.</p></li> </ul>`
	want := "Add a drizzle of oil to a large pot of water and bring to a boil.\nStir in ¼ cup water (½ cup for 4 servings) & stir."
	if got := CleanStepText(in); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	plain := "Dice the onion. Heat 1 < 2 things."
	if got := CleanStepText(plain); got != plain {
		t.Errorf("plain text changed: %q", got)
	}
}

func TestCleanStepTextUsesFractionGlyphs(t *testing.T) {
	in := "Stir in 1⁄2 cup reserved pasta water (for 4 servings, use 2⁄3 cup)."
	if got, want := CleanStepText(in), "Stir in ½ cup reserved pasta water (for 4 servings, use ⅔ cup)."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCleanStepTextDropsAPastedStepNumber(t *testing.T) {
	for in, want := range map[string]string{
		"1. Whisk the eggs.":      "Whisk the eggs.",
		"2) Bake until golden.":   "Bake until golden.",
		"Step 3: Let it rest.":    "Let it rest.",
		"2 eggs, beaten.":         "2 eggs, beaten.",
		"1.5 cups flour, sifted.": "1.5 cups flour, sifted.",
		"4. cups of flour":        "4. cups of flour",
	} {
		if got := CleanStepText(in); got != want {
			t.Errorf("CleanStepText(%q) = %q, want %q", in, got, want)
		}
	}
}
