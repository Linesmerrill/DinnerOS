package ingredients

import "testing"

// Hark's build 121 review: the pantry's Yellow Onions read "3" with no word.
// A count reads with the thing counted: "3 onions".
func TestCountNoun(t *testing.T) {
	for _, tc := range []struct{ name, one, other string }{
		{"Yellow Onions", "onion", "onions"},
		{"Large Eggs", "egg", "eggs"},
		{"Eggs", "egg", "eggs"},
		{"Limes", "lime", "limes"},
		{"Roma Tomatoes", "tomato", "tomatoes"},
		{"Bay Leaves", "leaf", "leaves"},
		{"Cherries", "cherry", "cherries"},
		{"Radishes", "radish", "radishes"},
		{"Lemon", "lemon", "lemons"},
		{"Avocado", "avocado", "avocados"},
		{"Potato", "potato", "potatoes"},
		{"Red Bell Pepper", "red bell pepper", "red bell peppers"},
		{"Coconut Milk", "coconut milk", "coconut milks"},
		{"Lettuce", "lettuce", "lettuces"},
		{"Shallot (small)", "shallot", "shallots"},
		{"  Jalapeños ", "jalapeño", "jalapeños"},
	} {
		one, other, ok := CountNoun(tc.name)
		if !ok || one != tc.one || other != tc.other {
			t.Errorf("CountNoun(%q) = %q, %q, %v; want %q, %q", tc.name, one, other, ok, tc.one, tc.other)
		}
	}
	if _, _, ok := CountNoun("  "); ok {
		t.Error("CountNoun(blank) ok; want no word")
	}
}
