package catalog

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Titles here are made up; none come from a real recipe.

func TestNearTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Brown Sugar Bourbon Chicken":           "brown sugar bourbon chicken",
		"  brown sugar  bourbon chicken!  ":     "brown sugar bourbon chicken",
		"Brown Sugar-Bourbon Chicken Recipe":    "brown sugar bourbon chicken",
		"The Best Easy Homemade Brownies":       "brownies",
		"Mac & Cheese":                          "mac and cheese",
		"Mac and Cheese":                        "mac and cheese",
		"Shepherd's Pie":                        "shepherds pie",
		"Recipe":                                "",
		"Brown Sugar Bourbon Pork Chops":        "brown sugar bourbon pork chops",
		"Brown Sugar Bourbon Chicken with Rice": "brown sugar bourbon chicken with rice",
	} {
		if got := nearTitle(in); got != want {
			t.Errorf("nearTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNearCopies(t *testing.T) {
	kit := func(name, link string, lines ...string) recipes.Recipe {
		r := dish(recipes.SourceImport, "kit-"+name+link, link, name, lines...)
		return r
	}
	typedDish := func(name string, lines ...string) recipes.Recipe {
		return typed(name, []string{"Cook it."}, lines...)
	}
	for _, tc := range []struct {
		name string
		a, b recipes.Recipe
		want bool
	}{
		{
			"a meal kit's re-release: same title, same site, new link",
			kit("Brown Sugar Bourbon Chicken", "https://kit.example.com/recipes/bsb-chicken-1", "chicken", "bourbon"),
			kit("Brown Sugar Bourbon Chicken Recipe", "https://kit.example.com/recipes/bsb-chicken-2", "chicken thighs", "green beans"),
			true,
		},
		{
			"same title from two sites with the same main ingredients",
			kit("Pesto Penne", "https://a.example.com/pesto-penne", "penne", "pesto", "parmesan"),
			kit("Pesto Penne", "https://b.example.com/pesto-penne", "penne", "pesto", "parmesan", "basil"),
			true,
		},
		{
			"same title from two sites with other ingredients",
			kit("Pesto Penne", "https://a.example.com/pesto-penne", "penne", "pesto"),
			kit("Pesto Penne", "https://b.example.com/pesto-penne", "zucchini", "walnuts", "spinach"),
			false,
		},
		{
			"two households type the same dish",
			typedDish("Brownies", "butter", "cocoa", "sugar"), typedDish("brownies!", "butter", "cocoa", "sugar"),
			true,
		},
		{
			"a different dinner with a similar name",
			kit("Brown Sugar Bourbon Chicken", "https://kit.example.com/recipes/a", "chicken"),
			kit("Brown Sugar Bourbon Pork Chops", "https://kit.example.com/recipes/b", "pork chops"),
			false,
		},
		{
			"no title says nothing",
			typedDish("Recipe", "butter"), typedDish("Recipe", "butter"),
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nearCopies(nearKeyOf(tc.a), nearKeyOf(tc.b)); got != tc.want {
				t.Errorf("nearCopies = %v, want %v (%+v, %+v)", got, tc.want, nearKeyOf(tc.a), nearKeyOf(tc.b))
			}
			if got := nearCopies(nearKeyOf(tc.b), nearKeyOf(tc.a)); got != tc.want {
				t.Errorf("nearCopies reversed = %v, want %v", got, tc.want)
			}
		})
	}
}

// A row shows one of near copies, the best ranked; search and the full
// list still show both (decision 733).
func TestRowsDropNearCopies(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	for i, link := range []string{"https://kit.example.com/recipes/bsb-1", "https://kit.example.com/recipes/bsb-2"} {
		publishWith(t, h, fmt.Sprintf("bsb-%d", i), "Brown Sugar Bourbon Chicken", func(r *recipes.Recipe) {
			r.SourceURL, r.TotalMinutes = link, 25
		})
	}
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range home.Sections {
		n := 0
		for _, it := range s.Items {
			if it.Content.Name == "Brown Sugar Bourbon Chicken" {
				n++
			}
		}
		if n > 1 {
			t.Errorf("row %s shows Brown Sugar Bourbon Chicken %d times: %v", s.ID, n, itemNames(s.Items))
		}
	}
	_, all, err := h.service.Section(t.Context(), hhAda, "chefs-picks", Query{})
	if err != nil {
		t.Fatal(err)
	}
	if got := itemNames(all.Items); len(got) != 4 || !slices.Contains(got, "Brown Sugar Bourbon Chicken") {
		t.Errorf("See All = %v; want each dinner once", got)
	}
	d, err := h.service.Discover(t.Context(), hhAda, Query{Text: "bourbon"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 2 {
		t.Errorf("discover = %v; the full list keeps both", resultNames(d))
	}
}

// A row leaves out what an earlier row showed when it has enough new
// recipes, and puts its new ones first when it doesn't (decision 733).
func TestRowsPreferRecipesNotShownAbove(t *testing.T) {
	for _, tc := range []struct {
		dishes    int
		wantQuick []string
		wantMore  bool
	}{
		// Chef’s Picks shows 1 to 12; Quick has 13 to 20 new, enough alone.
		{20, quickDishes(13, 20), true},
		// Quick has only 13 and 14 new: they lead, then the repeats.
		{14, append(quickDishes(13, 14), quickDishes(1, 10)...), true},
	} {
		t.Run(fmt.Sprint(tc.dishes), func(t *testing.T) {
			h := newHarness(t, nil)
			for i := 1; i <= tc.dishes; i++ {
				name := fmt.Sprintf("Quick Dish %02d", i)
				publishWith(t, h, fmt.Sprintf("quick-%02d", i), name, func(r *recipes.Recipe) {
					r.TotalMinutes = 20
					r.Ingredients[0].Name = fmt.Sprintf("ingredient %02d", i)
				})
			}
			home, err := h.service.Home(t.Context(), hhAda)
			if err != nil {
				t.Fatal(err)
			}
			if got := itemNames(section(t, home, "chefs-picks").Items); !slices.Equal(got, quickDishes(1, 12)) {
				t.Fatalf("chef's picks = %v", got)
			}
			quick := section(t, home, "quick")
			if got := itemNames(quick.Items); !slices.Equal(got, tc.wantQuick) || quick.More != tc.wantMore {
				t.Errorf("quick = %v more=%v; want %v more=%v", got, quick.More, tc.wantQuick, tc.wantMore)
			}
		})
	}
}

func quickDishes(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("Quick Dish %02d", i))
	}
	return out
}
