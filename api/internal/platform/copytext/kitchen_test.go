package copytext

import "testing"

func TestIngredientName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"bow tie pasta", "Bow Tie Pasta"},
		{"Bow tie pasta", "Bow Tie Pasta"},
		{"Jasmine Rice", "Jasmine Rice"},
		{"baby spinach", "Baby Spinach"},
		{"sun-dried tomatoes", "Sun-Dried Tomatoes"},
		{"cream of mushroom soup", "Cream of Mushroom Soup"},
		{"salt and pepper", "Salt and Pepper"},
		{"and more", "And More"},
		{"2% milk", "2% Milk"},
		{"BBQ sauce", "BBQ Sauce"},
		{"iPhone case", "iPhone Case"},
		{"chef's blend", "Chef’s Blend"},
		{"lemon (zested)", "Lemon (Zested)"},
		{"élan vital", "Élan Vital"},
		{"double  space", "Double  Space"},
	} {
		if got := IngredientName(tc.in); got != tc.want {
			t.Errorf("IngredientName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRanges(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"until fragrant, 30-60 seconds.", "until fragrant, 30 to 60 seconds."},
		{"cook until tender, 15-18 minutes. Keep covered", "cook until tender, 15 to 18 minutes. Keep covered"},
		{"5–7 min", "5 to 7 min"},
		{"1 - 2 hours", "1 to 2 hours"},
		{"Bake at 400-425°F.", "Bake at 400 to 425°F."},
		{"Use 1-2 Tbsp oil", "Use 1 to 2 Tbsp oil"},
		{"(2-3 cloves)", "(2 to 3 cloves)"},
		{"½-1 tsp chili flakes", "½ to 1 tsp chili flakes"},
		{"3-4 minutes, then 1-2 minutes more", "3 to 4 minutes, then 1 to 2 minutes more"},
		// Left alone: hyphenated words, fractions, sizes and bare numbers.
		{"a 9-by-13-inch baking dish", "a 9-by-13-inch baking dish"},
		{"sun-dried tomatoes and bite-size pieces", "sun-dried tomatoes and bite-size pieces"},
		{"1-1/2 cups rice", "1-1/2 cups rice"},
		{"steps 1-2", "steps 1-2"},
		{"COVID-19 rules", "COVID-19 rules"},
		{"2-3 garlic cloves", "2-3 garlic cloves"},
		{"a 12-inch pan", "a 12-inch pan"},
		{"no ranges here", "no ranges here"},
	} {
		if got := Ranges(tc.in); got != tc.want {
			t.Errorf("Ranges(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDescription(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Web Description: Fun fact: “Rosa” means “pink” in Italian.", "Fun fact: “Rosa” means “pink” in Italian."},
		{"Description: A quick weeknight pasta.", "A quick weeknight pasta."},
		{"  recipe summary :  Crispy and bright.", "Crispy and bright."},
		{"Web Description: Description: Twice labeled.", "Twice labeled."},
		{"About this recipe: Weeknight easy.", "Weeknight easy."},
		{"Fun fact: this stays.", "Fun fact: this stays."},
		{"Describe the sauce: it's bright.", "Describe the sauce: it's bright."},
		{"A description: that isn't a label.", "A description: that isn't a label."},
		{"", ""},
	} {
		if got := Description(tc.in); got != tc.want {
			t.Errorf("Description(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
