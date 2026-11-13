package recipes

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Recipe pages as sites publish them, read by ParsePage.
//
// Every fixture in testdata/pages is synthetic: the markup reproduces how a
// kind of site publishes a recipe (its JSON-LD shape, its recipe card plugin's
// classes, its microdata, its page noise), but the recipe names and lines are
// made up. Nothing in them is copied from a real recipe.

// fixtureLine is one exact check on a read ingredient line, by index.
type fixtureLine struct {
	i                            int
	name, qty, unit, note, group string
}

type pageFixture struct {
	file     string
	ok       bool
	method   string
	name     string
	servings int
	count    int
	// noAmount is whether the "no amount" warning is expected.
	noAmount bool
	lines    []fixtureLine
}

var pageFixtures = []pageFixture{
	// --- JSON-LD: big recipe sites -------------------------------------------------
	{file: "allrecipes.html", ok: true, method: PageJSONLD, name: "Weeknight Lemon Orzo Bake", servings: 6, count: 8,
		lines: []fixtureLine{
			{0, "uncooked orzo pasta", "1 1/2", "cup", "", ""},                  // "1 ½"
			{1, "butter", "2", "tbsp", "divided", ""},                           // comma note
			{2, "petite diced tomatoes", "1", "can", "14.5 ounce, drained", ""}, // package size
			{4, "red pepper flakes", "1/4", "tsp", "or to taste", ""},
			{5, "lemon", "1", "", "zested and juiced", ""},
			{7, "salt and ground black pepper", "", "", "to taste", ""},
		}},
	{file: "nyt-cooking.html", ok: true, method: PageJSONLD, name: "Charred Scallion Fried Rice", servings: 4, count: 8, noAmount: true,
		lines: []fixtureLine{
			{0, "neutral oil", "3", "tbsp", "such as canola", ""},
			{1, "scallions", "1", "bunch", "thinly sliced, whites and greens separated", ""},
			{2, "garlic cloves", "3", "", "finely chopped", ""}, // "2 to 3": the larger
			{3, "piece ginger", "1", "", "1-inch, peeled and minced", ""},
			{6, "soy sauce", "1 1/2", "tbsp", "plus more as needed", ""}, // "1½"
			{7, "Kosher salt", "", "", "", ""},
		}},
	{file: "serious-eats.html", ok: true, method: PageJSONLD, name: "Ginger-Garlic Chicken with Snap Peas", servings: 4, count: 7,
		lines: []fixtureLine{
			{0, "vegetable oil", "1", "tbsp", "15ml", ""},
			{1, "boneless chicken thighs", "1", "lb", "450g, cut into 1-inch pieces", ""},
			{2, "medium cloves garlic", "2", "", "about 2 teaspoons, minced", ""},
			{3, "homemade or store-bought chicken stock", "3/4", "cup", "180ml", ""},
			{4, "cornstarch", "1 1/2", "tsp", "4g", ""},
			{6, "snap peas", "8", "oz", "225g, trimmed", ""},
		}},
	{file: "budget-bytes.html", ok: true, method: PageJSONLD, name: "Turkey Black Bean Skillet", servings: 4, count: 8,
		lines: []fixtureLine{
			{0, "olive oil", "1", "tbsp", "$0.16", ""},
			{2, "garlic", "2", "clove", "$0.16", ""},
			{3, "ground turkey", "1", "lb", "$4.29", ""},      // "lb."
			{4, "black beans", "1", "can", "15oz, $0.79", ""}, // "1 15oz. can"
			{6, "chili powder", "1/2", "tbsp", "$0.15", ""},
		}},
	{file: "food-network.html", ok: true, method: PageJSONLD, name: "Skillet Shells with Smoky Cheddar", servings: 4, count: 9, noAmount: true,
		lines: []fixtureLine{
			{1, "medium onion", "1", "", "finely chopped", ""},
			{4, "sharp white cheddar", "8", "oz", "about 2 cups, grated", ""},
			{5, "elbow macaroni", "1", "lb", "", ""},
			{6, "Kosher salt and freshly ground black pepper", "", "", "", ""},
			{8, "Vegetable oil", "", "", "as needed", ""},
		}},
	{file: "tasty.html", ok: true, method: PageJSONLD, name: "Honey Sesame Chicken Bowls", servings: 4, count: 7,
		lines: []fixtureLine{
			{0, "boneless skinless chicken breast", "1", "lb", "cubed", ""},
			{4, "salt", "1/2", "tsp", "", ""},
			{6, "cooked rice", "1", "cup", "for serving", ""},
		}},
	{file: "delish.html", ok: true, method: PageJSONLD, name: "Paprika Cream Chicken", servings: 4, count: 7, noAmount: true,
		lines: []fixtureLine{
			{0, "extra-virgin olive oil", "1", "tbsp", "", ""}, // "Tbsp."
			{1, "boneless chicken thighs", "1", "lb", `cut into 1" pieces`, ""},
			{2, "smoked paprika", "2", "tsp", "", ""},      // "tsp."
			{3, "sour cream", "1/2", "cup", "", ""},        // "c."
			{4, "cream cheese", "2", "oz", "softened", ""}, // "oz."
		}},
	{file: "hellofresh.html", ok: true, method: PageJSONLD, name: "Sweet Soy Beef Bowls with Ginger Rice", servings: 2, count: 10, noAmount: true,
		lines: []fixtureLine{
			{0, "Yellow Onion", "1", "", "", ""}, // "1 unit"
			{1, "Garlic", "2", "clove", "", ""},
			{3, "Ground Beef", "10", "oz", "", ""},
			{4, "Sour Cream", "2", "tbsp", "", ""}, // "2 tablespoon"
			{7, "Scallions", "2", "", "", ""},
			{8, "Salt", "", "", "", ""},
		}},
	{file: "bon-appetit.html", ok: true, method: PageJSONLD, name: "Coconut-Ginger Braised Chicken", servings: 4, count: 7, noAmount: true,
		lines: []fixtureLine{
			{1, "skin-on chicken thighs", "1 1/2", "lb", "", ""},
			{2, "garlic cloves", "6", "", "smashed", ""},
			{4, "unsweetened coconut milk", "3/4", "cup", "", ""},
			{5, "kosher salt", "2", "tsp", "plus more", ""},
			{6, "Steamed rice", "", "", "for serving", ""},
		}},
	{file: "epicurious.html", ok: true, method: PageJSONLD, name: "Brown Sugar Snack Cake", servings: 8, count: 7,
		lines: []fixtureLine{
			{0, "unsalted butter", "1", "cup", "2 sticks, room temperature", ""},
			{1, "sugar", "1 1/8", "cup", "", ""}, // "1 cup plus 2 tablespoons"
			{3, "all-purpose flour", "2", "cup", "250 g", ""},
			{5, "kosher salt", "1/2", "tsp", "", ""}, // "½ tsp."
			{6, "whole milk", "1/3", "cup", "", ""},  // "⅓"
		}},
	{file: "simply-recipes.html", ok: true, method: PageJSONLD, name: "Chickpea Tomato Stew", servings: 4, count: 7,
		lines: []fixtureLine{
			{1, "large yellow onion", "1", "", "about 1 1/2 cups, chopped", ""},
			{2, "vegetable broth", "2 1/2", "cup", "", ""},
			{3, "chickpeas", "1", "can", "15-ounce, rinsed and drained", ""},
			{5, "chili powder", "1", "tsp", "", ""}, // "1/2 to 1"
			{6, "Salt", "", "", "to taste", ""},
		}},
	{file: "the-kitchn.html", ok: true, method: PageJSONLD, name: "Crunchy Lime Noodle Salad", servings: 4, count: 7,
		lines: []fixtureLine{
			{0, "fresh lime juice", "3", "tbsp", "", "Dressing"}, // "For the dressing:" line in recipeIngredient
			{2, "packed light brown sugar", "1", "tsp", "", "Dressing"},
			{3, "rice noodles", "8", "oz", "", "Salad"},
			{5, "medium cucumber", "1", "", "halved and thinly sliced", "Salad"},
		}},
	{file: "bbc-good-food.html", ok: true, method: PageJSONLD, name: "Speedy Tomato Basil Spaghetti", servings: 4, count: 8,
		lines: []fixtureLine{
			{0, "spaghetti", "300", "g", "", ""},
			{3, "garlic cloves", "2", "", "crushed", ""},
			{6, "small bunch basil", "1/2", "", "leaves picked", ""},
			{7, "parmesan", "50", "g", "finely grated", ""},
		}},
	{file: "king-arthur.html", ok: true, method: PageJSONLD, name: "Buttermilk Dinner Rolls", servings: 8, count: 7,
		lines: []fixtureLine{
			{0, "unbleached all-purpose flour", "2", "cup", "240g", ""},
			{1, "table salt", "1 1/4", "tsp", "", ""},
			{3, "unsalted butter", "1/2", "cup", "113g, cold, cut into pats", ""},
			{5, "instant yeast", "2 1/4", "tsp", "", ""},
		}},
	{file: "taste-of-home.html", ok: true, method: PageJSONLD, name: "Cheesy Beef Noodle Bake", servings: 8, count: 8, noAmount: true,
		lines: []fixtureLine{
			{2, "diced tomatoes", "1", "can", "14-1/2 ounces, undrained", ""},
			{3, "cream cheese", "1", "package", "8 ounces, softened", ""},
			{4, "shredded cheddar cheese", "1 1/2", "cup", "", ""}, // "1-1/2" is a mixed number
			{6, "uncooked egg noodles", "2 1/4", "cup", "", ""},    // "2-1/4"
			{7, "Minced fresh parsley", "", "", "optional", ""},
		}},

	// --- JSON-LD: blogs with recipe card plugins -----------------------------------
	{file: "minimalist-baker.html", ok: true, method: PageJSONLD, name: "Tahini Chickpea Quinoa Bowls", servings: 2, count: 8,
		lines: []fixtureLine{
			{0, "dry quinoa", "1", "cup", "well rinsed", ""},
			{1, "water", "1 1/2", "cup", "", ""},
			{3, "chickpeas", "1", "can", "15-ounce, rinsed + drained", ""}, // "1 15-ounce can"
			{6, "maple syrup", "2", "tbsp", "", ""},                        // "1-2"
			{7, "lemon", "1/2", "", "juiced", ""},
		}},
	{file: "pinch-of-yum.html", ok: true, method: PageJSONLD, name: "Weeknight Peanut Noodle Bowls", servings: 4, count: 7, noAmount: true,
		lines: []fixtureLine{
			{0, "peanut butter", "1/2", "cup", "", ""},
			{4, "garlic", "2", "clove", "", ""},
			{6, "a handful of fresh cilantro", "", "", "", ""},
		}},
	{file: "cookie-and-kate.html", ok: true, method: PageJSONLD, name: "Smoky Black Bean Chili", servings: 4, count: 8,
		lines: []fixtureLine{
			{3, "ground cumin", "1 1/2", "tsp", "", ""},
			{4, "diced tomatoes", "1", "can", "28 ounces, with their juices", ""},
			{5, "black beans", "2", "can", "15 ounces each, rinsed and drained", ""},
			{7, "fine sea salt", "1/4", "tsp", "more to taste", ""},
		}},
	{file: "recipetin-eats.html", ok: true, method: PageJSONLD, name: "Garlic Soy Chicken Noodles", servings: 4, count: 8,
		lines: []fixtureLine{
			{0, "chicken thigh fillets", "500", "g", "boneless and skinless", ""}, // "500g / 1 lb"
			{1, "chicken stock", "1", "cup", "low sodium", ""},                    // "1 cup / 250 ml"
			{3, "Chinese cooking wine", "1", "tbsp", "Shaoxing wine", ""},
			{6, "green onions", "2", "", "garnish, sliced", ""},
			{7, "dried egg noodles", "250", "g", "", ""}, // "250g/8oz"
		}},
	{file: "sallys-baking.html", ok: true, method: PageJSONLD, name: "Buttermilk Blueberry Muffins", servings: 12, count: 8,
		lines: []fixtureLine{
			{0, "all-purpose flour", "2 1/2", "cup", "313g, spooned & leveled", ""}, // "2 and 1/2", "&amp;"
			{3, "unsalted butter", "1/2", "cup", "115g, melted and slightly cooled", ""},
			{5, "large eggs", "2", "", "at room temperature", ""},
			{7, "fresh blueberries", "1 1/2", "cup", "210g", ""}, // "1 and 1/2"
		}},
	{file: "damn-delicious.html", ok: true, method: PageJSONLD, name: "Garlic Butter Soy Noodles", servings: 4, count: 8,
		lines: []fixtureLine{
			{0, "spaghetti", "8", "oz", "", ""},
			{2, "garlic", "3", "clove", "minced", ""},
			{4, "brown sugar", "1", "tbsp", "packed", ""},
			{5, "crushed red pepper flakes", "1/2", "tsp", "optional", ""},
		}},
	{file: "love-and-lemons.html", ok: true, method: PageJSONLD, name: "Lemony White Bean Farro Salad", servings: 4, count: 8, noAmount: true,
		lines: []fixtureLine{
			{0, "extra-virgin olive oil", "1/4", "cup", "plus more for drizzling", ""},
			{3, "sea salt", "3/4", "tsp", "", ""},
			{4, "cannellini beans", "1", "can", "15-ounce, drained and rinsed", ""},
			{7, "Freshly ground black pepper", "", "", "", ""},
		}},

	// --- JSON-LD edge cases --------------------------------------------------------
	{file: "double-escaped-entities.html", ok: true, method: PageJSONLD, name: "Grandma’s Mac & Cheese", servings: 6, count: 6,
		lines: []fixtureLine{
			{0, "elbow macaroni", "1 1/2", "cup", "", ""}, // "&amp;frac12;"
			{1, "butter", "1/2", "cup", "", ""},           // "&#189;"
			{2, "milk", "2 1/4", "cup", "", ""},           // "&amp;#188;"
			{3, "sharp cheddar & jack", "8", "oz", "shredded", ""},
			{4, "mustard powder", "3/4", "tsp", "", ""},
			{5, "Salt & pepper", "", "", "to taste", ""},
		}},
	{file: "legacy-ingredients-key.html", ok: true, method: PageJSONLD, name: "One-Bowl Banana Bread", servings: 4, count: 7,
		lines: []fixtureLine{
			{0, "all-purpose flour", "2", "cup", "", ""}, // "2 cups of"
			{1, "baking soda", "1", "tsp", "", ""},
			{3, "mashed ripe banana", "1", "cup", "about 3 bananas", ""},
			{6, "egg", "1", "", "beaten", ""},
		}},
	{file: "broken-json-ld-card-fallback.html", ok: true, method: PageCard, name: "Honey Garlic Salmon Bites", servings: 3, count: 4,
		lines: []fixtureLine{
			{0, "salmon fillet", "1", "lb", "skin removed, cubed", ""},
			{3, "garlic", "4", "clove", "minced", ""},
		}},

	// --- Microdata -----------------------------------------------------------------
	{file: "smitten-kitchen.html", ok: true, method: PageMicrodata, name: "Slow Onion Soup with Toasts", servings: 6, count: 8, noAmount: true,
		lines: []fixtureLine{
			{1, "yellow onions", "1 1/2", "lb", "thinly sliced", ""},
			{4, "beef or vegetable stock", "4", "cup", "", ""},
			{5, "Freshly ground black pepper", "", "", "", ""},
			{6, "thick slices baguette", "6", "", "", ""}, // "4 to 6"
		}},
	{file: "microdata-only-blog.html", ok: true, method: PageMicrodata, name: "Brown Butter Sage Squash Pasta", servings: 4, count: 6,
		lines: []fixtureLine{
			{0, "small butternut squash", "1", "", "about 2 lb, peeled and cubed", ""},
			{1, "butter", "4", "tbsp", "", ""},
			{4, "grated pecorino", "1/3", "cup", "", ""},
			{5, "nutmeg", "1/8", "tsp", "", ""},
		}},

	// --- Recipe cards with no JSON-LD ----------------------------------------------
	{file: "wprm-card-only.html", ok: true, method: PageCard, name: "Sheet Pan Gnocchi with Sausage", servings: 4, count: 7,
		lines: []fixtureLine{
			{0, "shelf-stable potato gnocchi", "1", "lb", "", "Gnocchi"},
			{1, "cherry tomatoes", "1 1/2", "cup", "halved", "Gnocchi"}, // "1&#189;", notes span
			{2, "olive oil", "2", "tbsp", "", "Gnocchi"},
			{3, "smoked sausage", "12", "oz", "sliced into coins", "Sausage"},
			{4, "red bell pepper", "1", "", "cut into strips", "Sausage"},
			{5, "salt and pepper", "", "", "to taste", "To finish"},
			{6, "fresh basil", "1/4", "cup", "torn", "To finish"},
		}},
	{file: "tasty-recipes-card-only.html", ok: true, method: PageCard, name: "Lemony Ricotta Tart", servings: 8, count: 7, noAmount: true,
		lines: []fixtureLine{
			{0, "graham cracker crumbs", "1 1/4", "cup", "", "Crust"},
			{1, "butter", "5", "tbsp", "melted", "Crust"},
			{3, "whole-milk ricotta", "16", "oz", "", "Filling"},
			{4, "honey", "1/3", "cup", "", "Filling"},
			{6, "zest of 1 lemon", "", "", "", "Filling"},
		}},
	{file: "mediavine-create-card-only.html", ok: true, method: PageCard, name: "Salsa Verde Chicken Enchiladas", servings: 6, count: 6,
		lines: []fixtureLine{
			{0, "shredded cooked chicken", "2", "cup", "", ""}, // the card's "Ingredients" title is not a group
			{2, "small corn tortillas", "8", "", "", ""},
			{3, "shredded Monterey Jack cheese", "1 1/2", "cup", "", ""},
			{4, "sour cream", "1/2", "cup", "for serving", ""},
		}},

	// --- Plain page text -----------------------------------------------------------
	{file: "plain-heading-blog.html", ok: true, method: PageText, name: "Sesame Peanut Noodles", servings: 0, count: 8,
		lines: []fixtureLine{
			{0, "creamy peanut butter", "3", "tbsp", "", "Sauce"},
			{3, "chili crisp", "1", "tsp", "or to taste", "Sauce"},
			{4, "warm water", "3", "tbsp", "", "Sauce"}, // "2-3"
			{5, "dried noodles", "8", "oz", "", "Noodles"},
			{7, "scallions", "2", "", "sliced", "Noodles"},
		}},

	// --- Not a recipe --------------------------------------------------------------
	{file: "no-recipe.html", ok: false},
}

func TestPageFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "pages", "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	// Every fixture on disk has a row, and every row a fixture.
	covered := map[string]bool{}
	for _, f := range pageFixtures {
		covered[f.file] = true
	}
	for _, path := range files {
		if !covered[filepath.Base(path)] {
			t.Errorf("%s has no row in pageFixtures", filepath.Base(path))
		}
	}
	if len(pageFixtures) < 25 {
		t.Errorf("only %d page fixtures", len(pageFixtures))
	}

	for _, f := range pageFixtures {
		t.Run(f.file, func(t *testing.T) {
			page, err := os.ReadFile(filepath.Join("testdata", "pages", f.file))
			if err != nil {
				t.Fatal(err)
			}
			d, method, ok := ParsePage(string(page))
			if ok != f.ok {
				t.Fatalf("ok = %v, want %v (method %q, name %q)", ok, f.ok, method, d.Name)
			}
			if !ok {
				if method != "" || d.Name != "" || len(d.Ingredients) != 0 {
					t.Errorf("a page with no recipe read as %q / %q with %d lines", method, d.Name, len(d.Ingredients))
				}
				return
			}
			if method != f.method {
				t.Errorf("method = %q, want %q", method, f.method)
			}
			if d.Name != f.name {
				t.Errorf("name = %q, want %q", d.Name, f.name)
			}
			if d.Servings != f.servings {
				t.Errorf("servings = %d, want %d", d.Servings, f.servings)
			}
			if len(d.Ingredients) != f.count {
				t.Errorf("%d ingredients, want %d", len(d.Ingredients), f.count)
			}
			for _, want := range f.lines {
				if want.i >= len(d.Ingredients) {
					t.Errorf("line %d: missing", want.i)
					continue
				}
				got := d.Ingredients[want.i]
				if got.Name != want.name || got.Quantity != want.qty || got.Unit != want.unit ||
					got.Note != want.note || got.Group != want.group {
					t.Errorf("line %d %q:\n got name %q, qty %q, unit %q, note %q, group %q\nwant name %q, qty %q, unit %q, note %q, group %q",
						want.i, got.RawText, got.Name, got.Quantity, got.Unit, got.Note, got.Group,
						want.name, want.qty, want.unit, want.note, want.group)
				}
			}
			for i, line := range d.Ingredients {
				if line.RawText == "" || line.Name == "" {
					t.Errorf("line %d has no raw text or name: %+v", i, line)
				}
			}

			// Only the title, servings, and lines are kept (decision 670).
			if d.Description != "" || d.ImageURL != "" || len(d.Steps) != 0 || len(d.Tags) != 0 || len(d.Cuisines) != 0 ||
				d.PrepMinutes != 0 || d.TotalMinutes != 0 {
				t.Errorf("kept more than the title, servings, and lines: %+v", d)
			}
			if !slices.Contains(d.Warnings, ImportedRecipeNote) {
				t.Errorf("warnings %q lack the imported-recipe note", d.Warnings)
			}
			const noAmount = "Some lines have no amount. Add one so the list buys enough."
			if got := slices.Contains(d.Warnings, noAmount); got != f.noAmount {
				t.Errorf("no-amount warning = %v, want %v (warnings %q)", got, f.noAmount, d.Warnings)
			}
		})
	}
}

// Line shapes the page fixtures turned up, checked one by one.
func TestPageIngredientLineShapes(t *testing.T) {
	cases := []struct {
		line                  string
		name, qty, unit, note string
	}{
		// "1-1/2" and "2 and 1/2" are mixed numbers, not a range or a sum.
		{"1-1/2 cups shredded cheddar cheese", "shredded cheddar cheese", "1 1/2", "cup", ""},
		{"2 and 1/2 cups (313g) flour", "flour", "2 1/2", "cup", "313g"},
		{"1 and ½ cups blueberries", "blueberries", "1 1/2", "cup", ""},
		{"2-3 cloves garlic", "garlic", "3", "clove", ""},
		// A package size before its container, without parentheses.
		{"1 15oz. can black beans", "black beans", "1", "can", "15oz"},
		{"2 15-ounce cans chickpeas, drained", "chickpeas", "2", "can", "15-ounce, drained"},
		{"1 cup frozen corn", "frozen corn", "1", "cup", ""},
		// A unit run into its other-units twin.
		{"250g/8oz dried egg noodles", "dried egg noodles", "250", "g", ""},
		{"1 cup/250ml stock", "stock", "1", "cup", ""},
		// A meal kit's "unit".
		{"1 unit Yellow Onion", "Yellow Onion", "1", "", ""},
		{"2 units Limes", "Limes", "2", "", ""},
		{"1 unit", "unit", "1", "", ""},
		// "boneless, skinless" stays in the name.
		{"1 lb boneless, skinless chicken thighs, trimmed", "boneless skinless chicken thighs", "1", "lb", "trimmed"},
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
