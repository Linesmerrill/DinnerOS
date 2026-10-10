package ingredients

import (
	"slices"
	"testing"
)

// "Onion" and "Yellow Onion" are one ingredient; "Red Onion" is another
// (decision 640).
func TestSameIngredient(t *testing.T) {
	for in, want := range map[string]string{
		"yellow onion": "onion", "onion": "onion", "red onion": "red onion",
		"roma tomato": "tomato", "grape tomatoes": "grape tomatoes",
		"persian cucumber": "mini cucumber", "sweet potatoes": "sweet potatoes",
	} {
		if got := SameIngredientKey(in); got != want {
			t.Errorf("SameIngredientKey(%q) = %q, want %q", in, got, want)
		}
	}
	if got := SameIngredientKeys("yellow onion"); !slices.Equal(got, []string{"onion", "yellow onion"}) {
		t.Errorf("SameIngredientKeys(yellow onion) = %q", got)
	}
	if got := SameIngredientKeys("garlic"); !slices.Equal(got, []string{"garlic"}) {
		t.Errorf("SameIngredientKeys(garlic) = %q", got)
	}
	// Every name is already normalized, and none is in two groups.
	seen := map[string]bool{}
	for _, group := range sameIngredients {
		for _, name := range group {
			if NormalizeName(name) != name || seen[name] {
				t.Errorf("%q is unnormalized or listed twice", name)
			}
			seen[name] = true
		}
	}
	if v := SameIngredientVariants(); v["yellow onion"] != "onion" || len(v) != len(sameAs) {
		t.Errorf("variants = %v", v)
	}
}
