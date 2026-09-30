package recipes

import (
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

func TestPrepWordsReadAsDone(t *testing.T) {
	for clause, want := range map[string]string{
		"Trim and slice ":                                "sliced",
		"Quarter ":                                       "quartered",
		"Peel, core, and dice ":                          "peeled, cored, and diced",
		"Halve, core, and thinly slice ":                 "halved, cored, and thinly sliced",
		"In a medium bowl, combine ":                     "",
		"Stir drained rigatoni, half the Parmesan, and ": "",
		"Peel and mince or grate ":                       "peeled, minced, or grated",
		"Add diced ":                                     "diced",
		"Add peeled and diced ":                          "peeled and diced",
		"juice from half ":                               "juiced",
		"Dice the onion, then add the ":                  "",
		"Combine lime ":                                  "",
	} {
		if got := PrepWords(clause); got != want {
			t.Errorf("PrepWords(%q) = %q, want %q", clause, got, want)
		}
	}
}

func TestTrailingPrepOnlyReadsCuts(t *testing.T) {
	for text, want := range map[string]string{
		" into strips. Halve orange.":      "into strips",
		" into ½-inch pieces, then chill.": "into ½-inch pieces",
		" lengthwise.":                     "lengthwise",
		" into pot with couscous.":         "",
		" into a bowl.":                    "",
		" in damp paper towels.":           "",
	} {
		if got := TrailingPrep(text); got != want {
			t.Errorf("TrailingPrep(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestAheadNote(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"Butter", "cut into pieces, for step 4"},
		{"Unsalted Butter", "cut into pieces, for step 4"},
		{"Peanut Butter", ""},
		{"Cream Cheese", "let soften, for step 4"},
		{"Shallot", ""},
	} {
		if got := AheadNote(tc.name, 4); got != tc.want {
			t.Errorf("AheadNote(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func item(t *testing.T, c Checklist, step int, name string) CookStepItem {
	t.Helper()
	for _, g := range c.ByStep {
		if g.Index != step {
			continue
		}
		for _, it := range g.Items {
			if it.Name == name {
				return it
			}
		}
	}
	t.Fatalf("no %q in step %d: %+v", name, step, c.ByStep)
	return CookStepItem{}
}

// The Citrus Pork Tacos case, in words written for the test: short names,
// cuts after the name, wedges, the rest, a blend to mix first, and "while"
// mentions left out.
func TestChecklistReadsTheStepsLikeACook(t *testing.T) {
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
		[]RecipeIngredient{
			tacoLine("ing-onion", "Red Onion", "1", "3/2", "count"),
			tacoLine("ing-pepper", "Long Green Pepper", "1", "3/2", "count"),
			tacoLine("ing-lime", "Lime", "1", "3/2", "count"),
			tacoLine("ing-sw", "Southwest Spice Blend", "1", "3/2", "tbsp"),
			tacoLine("ing-pork", "Ground Pork", "10", "15", "oz"),
			tacoLine("ing-butter", "Butter", "2", "3", "tbsp"),
			tacoLine("ing-salt", "Salt", "", "", ""),
		},
		"Halve, peel, and thinly slice onion. Quarter lime. Halve, core, and thinly slice green pepper into strips.",
		"Combine ¼ of the onion and juice from half the lime. Stir in ¼ tsp Southwest Spice (½ tsp for 4).",
		"Add pork and remaining Southwest Spice. Add remaining onion. Stir in a squeeze of lime juice and butter.",
		"While pork cooks, warm tortillas. Serve with remaining lime wedges.")
	c := Annotate(r, 2, grocery.Specialties{"ing-sw": spec}, true).Checklist

	if it := item(t, c, 1, "Red Onion"); it.Prep != "halved, peeled, and thinly sliced" || it.AmountText != "1" {
		t.Errorf("onion = %+v", it)
	}
	if it := item(t, c, 1, "Long Green Pepper"); it.Prep != "halved, cored, and thinly sliced into strips" {
		t.Errorf("pepper = %+v", it)
	}
	if it := item(t, c, 1, "Lime"); it.Prep != "quartered" {
		t.Errorf("lime = %+v", it)
	}
	if it := item(t, c, 2, "Red Onion"); it.AmountText != "¼" {
		t.Errorf("onion share = %+v", it)
	}
	if it := item(t, c, 2, "Lime wedges"); it.AmountText != "2" || it.Prep != "juiced" {
		t.Errorf("lime wedges = %+v", it)
	}
	if it := item(t, c, 3, "Southwest Spice Blend"); it.Prep != "the rest" {
		t.Errorf("rest of the blend = %+v", it)
	}
	if it := item(t, c, 3, "Lime wedge"); it.AmountText != "1" {
		t.Errorf("squeeze = %+v", it)
	}
	if it := item(t, c, 4, "Lime wedges"); it.Prep != "the rest" {
		t.Errorf("serving wedges = %+v", it)
	}
	for _, g := range c.ByStep {
		if g.Index == 4 && slices.ContainsFunc(g.Items, func(it CookStepItem) bool { return it.Name == "Ground Pork" }) {
			t.Errorf("step 4 lists the pork from \"While pork cooks\": %+v", g.Items)
		}
	}
	if mix := item(t, c, 0, "Southwest Spice Blend"); mix.Prep != "mix together first" ||
		strings.Join(mix.Parts, "|") != "2 tsp Chili Powder|1 tsp Ground Cumin" || mix.AmountText != "1 Tbsp" {
		t.Errorf("blend to mix = %+v", mix)
	}
	if ahead := item(t, c, 0, "Butter"); ahead.Prep != "cut into pieces, for step 3" {
		t.Errorf("butter ahead = %+v", ahead)
	}
	if salt := item(t, c, 0, "Salt"); salt.ID != "6-ing-salt@0" {
		t.Errorf("salt = %+v, want it under Have Ready with a stable ID", salt)
	}
	// All together: the onion's shares aren't whole-step amounts, so only the lime,
	// named in wedges in two steps, opens into parts.
	for _, ci := range c.All {
		if ci.Name == "Lime" && len(ci.Parts) != 2 {
			t.Errorf("lime parts = %+v, want its two wedge shares", ci.Parts)
		}
	}
}

func TestStepTimersNameWhatsCooking(t *testing.T) {
	timers := stepTimers(
		"Add 10 oz ground beef and spice. Cook, breaking up meat, until browned, 3-4 minutes. Stir in paste; simmer until sauce thickens, 2-3 minutes more. Season with salt.",
		[]string{"Ground Beef", "Southwest Spice Blend", "Salt"})
	if len(timers) != 2 {
		t.Fatalf("timers = %+v", timers)
	}
	if got := timers[0]; got.Text != "3-4 minutes" || got.LowSeconds != 180 || got.HighSeconds != 240 || got.StartSeconds != 240 || got.Subject != "Ground Beef" {
		t.Errorf("first = %+v", got)
	}
	if got := timers[1]; got.Subject != "Sauce" || got.StartSeconds != 180 {
		t.Errorf("second = %+v", got)
	}
	if got := stepTimers("Microwave for 30 seconds.", nil); len(got) != 1 || got[0].StartSeconds != 30 {
		t.Errorf("seconds = %+v", got)
	}
	if got := stepTimers("Roast 1½ hours.", nil); len(got) != 1 || got[0].StartSeconds != 5400 {
		t.Errorf("hours = %+v", got)
	}
}
