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
