package ingredients

import "testing"

func TestCategorize(t *testing.T) {
	tests := map[string]string{
		// Staples that must not be mistaken for produce.
		"Salt":         CategorySpices,
		"Pepper":       CategorySpices,
		"Black Pepper": CategorySpices,

		// Specific phrases beat general words.
		"Garlic Powder":             CategorySpices,
		"Garlic":                    CategoryProduce,
		"Coconut Milk":              CategoryPantry,
		"Chicken Stock Concentrate": CategoryPantry,
		"Crispy Fried Onions":       CategoryPantry,
		"Yellow Onion":              CategoryProduce,
		"Sour Cream":                CategoryDairyEggs,
		"Cream Sauce Base":          CategoryPantry,
		"Garlic Herb Butter":        CategoryDairyEggs,
		"Chili Flakes":              CategorySpices,
		"Korean Chili Flakes":       CategorySpices,
		"Chili Powder":              CategorySpices,
		"Chili Pepper":              CategoryProduce,
		"Blue Corn Tortilla Chips":  CategoryPantry,
		"Flour Tortillas":           CategoryBakery,
		"Corn":                      CategoryProduce,
		"Bell Pepper":               CategoryProduce,
		"Long Green Pepper":         CategoryProduce,
		"Jalapeño":                  CategoryProduce,
		"Crème Fraîche":             CategoryDairyEggs,
		"Dried Thyme":               CategorySpices,
		"Thyme":                     CategoryProduce,
		"Crushed Tomatoes":          CategoryPantry,
		"Grape Tomatoes":            CategoryProduce,
		"Tomato Paste":              CategoryCondiments,

		// Common HelloFresh ingredients.
		"Cooking Oil":                 CategoryPantry,
		"Olive Oil":                   CategoryPantry,
		"Butter":                      CategoryDairyEggs,
		"Scallions":                   CategoryProduce,
		"Lime":                        CategoryProduce,
		"Jasmine Rice":                CategoryPantry,
		"Cilantro":                    CategoryProduce,
		"Sugar":                       CategoryPantry,
		"Ground Pork":                 CategoryMeatSeafood,
		"Ground Beef":                 CategoryMeatSeafood,
		"Southwest Spice Blend":       CategorySpices,
		"Parmesan Cheese":             CategoryDairyEggs,
		"Sweet Soy Glaze":             CategoryCondiments,
		"Tex-Mex Paste":               CategoryCondiments,
		"Green Beans":                 CategoryProduce,
		"Mayonnaise":                  CategoryCondiments,
		"Sweet Thai Chili Sauce":      CategoryCondiments,
		"Panko Breadcrumbs":           CategoryPantry,
		"Italian Seasoning":           CategorySpices,
		"Spaghetti":                   CategoryPantry,
		"Mexican Cheese Blend":        CategoryDairyEggs,
		"Smoky Red Pepper Crema":      CategoryCondiments,
		"Italian Chicken Sausage Mix": CategoryMeatSeafood,
		"Pork Chops":                  CategoryMeatSeafood,
		"Shrimp":                      CategoryMeatSeafood,
		"Demi-Baguette":               CategoryBakery,
		"Pretzel Bites":               CategoryBakery,
		"Peanuts":                     CategoryPantry,
		"Sesame Seeds":                CategoryPantry,
		"Shredded Red Cabbage":        CategoryProduce,
		"Red Cabbage and Carrot Mix":  CategoryProduce,
		"Button Mushrooms":            CategoryProduce,
		"Brussels Sprouts":            CategoryProduce,
		"Mini Cucumber":               CategoryProduce,
		"Monterey Jack Cheese":        CategoryDairyEggs,
		"Rice Wine Vinegar":           CategoryCondiments,
		"Honey":                       CategoryCondiments,
		"Berry Compote":               CategoryCondiments,
		"Farro":                       CategoryPantry,
		"Guacamole":                   CategoryCondiments,
		"Kiwi":                        CategoryProduce,
		"Pie Crusts":                  CategoryBakery,
		"Pistachios":                  CategoryPantry,
		"Radishes":                    CategoryProduce,
		"Shredded Coconut":            CategoryPantry,
		"Wooden Skewers":              CategoryOther,
	}
	for name, want := range tests {
		got, confident := Categorize(name)
		if got != want || !confident {
			t.Errorf("Categorize(%q) = %s (confident=%v), want %s", name, got, confident, want)
		}
	}
}

func TestCategorizeUnknownIsFlagged(t *testing.T) {
	for _, name := range []string{"", "   ", "Mystery Ingredient XYZ"} {
		if got, confident := Categorize(name); got != CategoryOther || confident {
			t.Errorf("Categorize(%q) = %s (confident=%v), want other/false", name, got, confident)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	tests := map[string]string{
		"Crème Fraîche":            "creme fraiche",
		"  Pork & Green  Peppers ": "pork and green peppers",
		"Tex-Mex Paste":            "tex mex paste",
		"Colman's English Mustard": "colman's english mustard",
	}
	for in, want := range tests {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
