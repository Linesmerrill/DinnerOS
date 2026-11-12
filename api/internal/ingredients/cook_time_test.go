package ingredients

import "testing"

func TestCookTimeFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		ok   bool
	}{
		{"Lo Mein Noodles", "4 to 6 minutes", true},
		{"Fresh Lo Mein Noodles", "2 to 3 minutes", true},
		{"Ramen Noodles", "3 minutes", true},
		{"Spaghetti", "9 to 11 minutes", true},
		{"Whole Wheat Spaghetti", "9 to 11 minutes", true},
		{"Brown Rice", "40 to 45 minutes", true},
		{"Jasmine Rice", "15 to 18 minutes", true},
		{"Chicken Breasts", "", false},
		{"Rice Vinegar", "", false},
		{"", "", false},
	} {
		got, ok := CookTimeFor(tc.name)
		if ok != tc.ok || (ok && got.Text() != tc.text) {
			t.Errorf("CookTimeFor(%q) = %q, %v; want %q, %v", tc.name, got.Text(), ok, tc.text, tc.ok)
		}
	}
	if got, _ := CookTimeFor("Lo Mein Noodles"); got.Suggested() != 6 {
		t.Errorf("suggested = %d, want 6", got.Suggested())
	}
}
