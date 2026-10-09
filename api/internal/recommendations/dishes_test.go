package recommendations

import (
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// A dish is what a meal is called, across cuisines: Korean BBQ tacos are
// tacos, a rigatoni is pasta, and a typed dish ("gnocchi") matches the name
// (decision 637).
func TestRecipeDishes(t *testing.T) {
	cases := []struct {
		name  string
		want  []string
		types []string
	}{
		{"Korean BBQ Beef Tacos with Kimchi Slaw", []string{"taco", "korean"}, []string{"taco"}},
		{"Tex-Mex Beef & Pepper Enchiladas", []string{"enchilada"}, []string{"enchilada"}},
		{"Chicken Sausage Cavatappi Bolognese", []string{"cavatappi", "pasta"}, []string{"pasta"}},
		{"Sweet Soy Pork Stir Fry", []string{"stir-fry"}, []string{"stir-fry"}},
		{"Brown Butter Gnocchi", []string{"gnocchi"}, nil},
		{"Spicy Chicken Curries", []string{"curry"}, []string{"curry"}},
	}
	for _, c := range cases {
		r := recipes.Recipe{Name: c.name}
		got := RecipeDishes(r)
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("%q dishes = %v, want %q", c.name, got, w)
			}
		}
		types := dishTypes(got)
		for _, w := range c.types {
			if !slices.Contains(types, w) {
				t.Errorf("%q dish types = %v, want %q", c.name, types, w)
			}
		}
		for _, ty := range types {
			if ty == "with" || ty == "chicken" || ty == "beef" {
				t.Errorf("%q dish types include %q", c.name, ty)
			}
		}
	}
	for in, want := range map[string]string{"Tacos": "taco", " stir fries ": "stir-fry", "Curries": "curry", "Sandwiches": "sandwich", "Couscous": "couscous", "Pasta": "pasta"} {
		if got := canonicalDish(in); got != want {
			t.Errorf("canonicalDish(%q) = %q, want %q", in, got, want)
		}
	}
	if DishLabel("taco") != "Tacos" || DishLabel("gnocchi") != "Gnocchi" {
		t.Errorf("labels = %q %q", DishLabel("taco"), DishLabel("gnocchi"))
	}
}

// A weekday rule can name only dishes, stored singular, and goes to the
// provider with them.
func TestWeekdayRuleWithDishes(t *testing.T) {
	profile := DefaultProfile(hhA)
	profile.WeekdayRules = []WeekdayRule{{Day: "tue", Label: "Taco Tuesday", Dishes: []string{"Tacos", "Enchiladas"}}}
	got, err := normalizeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if r := got.WeekdayRules[0]; !slices.Equal(r.Dishes, []string{"enchilada", "taco"}) && !slices.Equal(r.Dishes, []string{"taco", "enchilada"}) {
		t.Errorf("dishes = %v", r.Dishes)
	}
	if in := got.preferences(2); len(in.Rules) != 1 || len(in.Rules[0].Dishes) != 2 {
		t.Errorf("provider rules = %+v", in.Rules)
	}
}

// A dish word that describes another dish isn't the meal: taco soup is a
// soup, a burrito bowl is a bowl, and sweet chili pork is no chili. The
// dish that ends a part of the name before "with" is (decision 638).
func TestDishIsWhatTheNameEndsWith(t *testing.T) {
	cases := []struct {
		name    string
		types   []string
		notDish []string
	}{
		{"One-Pot Beefy Taco Soup", []string{"soup"}, []string{"taco"}},
		{"Tex-Mex Taco Soup with Sour Cream", []string{"soup"}, []string{"taco"}},
		{"Chipotle Chicken Burrito Bowls", []string{"bowl"}, []string{"burrito"}},
		{"Creamy Pasta Salad", []string{"salad"}, []string{"pasta"}},
		{"Sweet Chili Pork & Rice", nil, []string{"chili"}},
		{"Chili Lime Chicken Tacos", []string{"taco"}, []string{"chili"}},
		{"Pork Carnitas Tacos with Side Salad", []string{"taco"}, []string{"salad"}},
		{"Beef Tacos & Cheesy Quesadillas", []string{"taco", "quesadilla"}, nil},
		{"Turkey Chili and Cornbread", []string{"chili"}, nil},
		{"Chicken Pad Thai", []string{"noodle"}, nil},
		{"Beef Ramen", []string{"noodle"}, nil},
		{"Hearty Beef Stew", []string{"soup"}, nil},
	}
	for _, c := range cases {
		got := RecipeDishes(recipes.Recipe{Name: c.name})
		types := dishTypes(got)
		if !slices.Equal(types, c.types) {
			t.Errorf("%q dish types = %v, want %v", c.name, types, c.types)
		}
		for _, d := range c.notDish {
			if slices.Contains(got, d) {
				t.Errorf("%q dishes = %v, must not include %q", c.name, got, d)
			}
		}
	}
}

// Words that follow a dish without being the meal, and phrases that start
// what comes with it, leave the dish as the meal (decision 638).
func TestDishTrailersAndPhraseBreaks(t *testing.T) {
	cases := map[string][]string{
		"Mexican-Style Chicken Tacos De Canasta":  {"taco"},
		"Southwest Chicken Taco Bar":              {"taco"},
		"One-Pot Chicken Sausage Chili Verde":     {"chili"},
		"Beef Chili Con Carne":                    {"chili"},
		"Chicken & Mushroom Ramen in Dashi Broth": {"noodle"},
		"Meatloaves with a Sweet Chili Glaze":     {"meatloaf"},
	}
	for name, want := range cases {
		if got := dishTypes(RecipeDishes(recipes.Recipe{Name: name})); !slices.Equal(got, want) {
			t.Errorf("%q dish types = %v, want %v", name, got, want)
		}
	}
}
