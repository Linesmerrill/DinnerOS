package recipes

import "testing"

// Ingredient lines as recipe sites write them.
func TestParseIngredientLine(t *testing.T) {
	cases := []struct {
		line                  string
		name, qty, unit, note string
	}{
		// Plain amounts and units.
		{"2 cups all-purpose flour", "all-purpose flour", "2", "cup", ""},
		{"1 egg", "egg", "1", "", ""},
		{"3 large eggs", "large eggs", "3", "", ""},
		{"0.5 lb ground beef", "ground beef", "0.5", "lb", ""},
		{".5 cup milk", "milk", ".5", "cup", ""},
		// Fractions, slash and unicode.
		{"1/2 cup sugar", "sugar", "1/2", "cup", ""},
		{"1 1/2 cups broth", "broth", "1 1/2", "cup", ""},
		{"½ tsp salt", "salt", "1/2", "tsp", ""},
		{"1½ cups rice", "rice", "1 1/2", "cup", ""},
		{"1 ½ cups rice", "rice", "1 1/2", "cup", ""},
		{"¾ cup water", "water", "3/4", "cup", ""},
		{"⅓ cup honey", "honey", "1/3", "cup", ""},
		{"1⁄2 teaspoon cumin", "cumin", "1/2", "tsp", ""},
		// Ranges: the larger amount.
		{"2-3 cloves garlic, minced", "garlic", "3", "clove", "minced"},
		{"2 to 3 tablespoons oil", "oil", "3", "tbsp", ""},
		{"1–2 jalapeños", "jalapeños", "2", "", ""},
		{"½-1 tsp chili flakes", "chili flakes", "1", "tsp", ""},
		// Mixed and dual units.
		{"1 cup plus 2 tablespoons milk", "milk", "1 1/8", "cup", ""},
		{"1 cup + 2 Tbsp milk", "milk", "1 1/8", "cup", ""},
		{"1 lb / 450 g ground beef", "ground beef", "1", "lb", ""},
		{"8 oz (225g) cream cheese", "cream cheese", "8", "oz", "225g"},
		{"1 (14.5 oz) can diced tomatoes", "diced tomatoes", "1", "can", "14.5 oz"},
		{"2 (8-ounce) packages cream cheese, softened", "cream cheese", "2", "package", "8-ounce, softened"},
		{"12 fl oz beer", "beer", "12", "floz", ""},
		// Abbreviations.
		{"2 Tbsp. butter", "butter", "2", "tbsp", ""},
		{"1 tsp. vanilla", "vanilla", "1", "tsp", ""},
		{"4 oz. feta", "feta", "4", "oz", ""},
		{"1 T olive oil", "olive oil", "1", "tbsp", ""},
		{"1 t baking soda", "baking soda", "1", "tsp", ""},
		{"2 c. flour", "flour", "2", "cup", ""},
		{"2 cups of flour", "flour", "2", "cup", ""},
		// To taste, notes, and no amount.
		{"Salt to taste", "Salt", "", "", "to taste"},
		{"Salt and pepper, to taste", "Salt and pepper", "", "", "to taste"},
		{"Kosher salt, as needed", "Kosher salt", "", "", "as needed"},
		{"Fresh parsley, for garnish", "Fresh parsley", "", "", "for garnish"},
		{"1 cup flour (sifted)", "flour", "1", "cup", "sifted"},
		{"2 cups spinach (about 2 ounces), chopped", "spinach", "2", "cup", "about 2 ounces, chopped"},
		{"1 onion, diced", "onion", "1", "", "diced"},
		{"• 1 cup rice", "rice", "1", "cup", ""},
		// A unit word with nothing after it is a name.
		{"2 cans", "cans", "2", "", ""},
	}
	for _, c := range cases {
		got := parseIngredientLine(c.line)
		if got.Name != c.name || got.Quantity != c.qty || got.Unit != c.unit || got.Note != c.note {
			t.Errorf("%q = name %q, qty %q, unit %q, note %q; want %q, %q, %q, %q",
				c.line, got.Name, got.Quantity, got.Unit, got.Note, c.name, c.qty, c.unit, c.note)
		}
		if got.RawText != c.line {
			t.Errorf("%q: raw text %q", c.line, got.RawText)
		}
	}
}

// Group headings head the lines under them, in pasted text too.
func TestGroupedSections(t *testing.T) {
	d := ParseText("Tacos\nIngredients\nFor the filling:\n1 lb beef\nFor the salsa:\n2 tomatoes\nSteps\nCook.")
	if len(d.Ingredients) != 2 || d.Ingredients[0].Group != "Filling" || d.Ingredients[1].Group != "Salsa" {
		t.Fatalf("ingredients = %+v", d.Ingredients)
	}
	for heading, want := range map[string]string{"For the sauce:": "Sauce", "Topping:": "Topping", "for dressing:": "Dressing"} {
		if got := groupName(heading); got != want {
			t.Errorf("groupName(%q) = %q, want %q", heading, got, want)
		}
	}
}
