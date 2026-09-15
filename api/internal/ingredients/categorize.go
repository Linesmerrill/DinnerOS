package ingredients

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Grocery categories in aisle order. They match grocery.CategoryOrder.
const (
	CategoryProduce     = "produce"
	CategoryMeatSeafood = "meat-seafood"
	CategoryDairyEggs   = "dairy-eggs"
	CategoryBakery      = "bakery"
	CategoryDeli        = "deli"
	CategoryPantry      = "pantry"
	CategorySpices      = "spices"
	CategoryCondiments  = "condiments"
	CategoryFrozen      = "frozen"
	CategoryBeverages   = "beverages"
	CategoryOther       = "other"
)

// categoryRule maps name phrases to a category. Rules are evaluated in order,
// so specific phrases ("garlic powder", "coconut milk", "stock concentrate")
// come before general words ("garlic", "milk", "chicken"). Exact rules match
// only the whole normalized name, so "Pepper" is a spice but "Bell Pepper" is
// produce.
type categoryRule struct {
	category string
	exact    bool
	phrases  []string
}

var categoryRules = []categoryRule{
	{CategorySpices, true, []string{"salt", "pepper", "black pepper", "white pepper", "kosher salt", "sea salt", "salt and pepper"}},
	// Non-food supplies occasionally listed as ingredients.
	{CategoryOther, false, []string{"skewers", "wooden skewers", "parchment", "foil", "toothpicks", "oven ready tray"}},
	{CategorySpices, false, []string{"harissa powder", "dashi powder", "truffle zest"}},
	{CategoryProduce, false, []string{"salad kit", "mixed greens"}},

	{CategoryPantry, false, []string{"stock concentrate", "stock", "broth", "bouillon", "cream sauce base", "cheese roux concentrate", "roux concentrate", "coconut milk", "coconut cream"}},
	{CategorySpices, false, []string{"garlic powder", "onion powder", "chili powder", "chipotle powder", "curry powder", "chili flakes", "red pepper flakes", "ground ginger", "spice blend", "seasoning", "spice", "cumin", "paprika", "turmeric", "cinnamon", "oregano", "dried thyme", "dried rosemary", "dried oregano", "nutmeg", "coriander", "garam masala", "za'atar", "sumac", "cayenne", "bay leaf"}},
	{CategoryPantry, false, []string{"crispy fried onions", "fried onions", "tortilla chips", "chips", "breadcrumbs", "panko", "croutons", "egg noodles", "shredded coconut", "wonton strips", "batter mix", "cooking spray", "dried apricots", "bean blend"}},
	{CategoryCondiments, false, []string{"chimichurri", "sofrito", "marinara", "hot sauce", "sriracha", "sauce", "glaze", "paste", "dressing", "vinaigrette", "vinegar", "mayonnaise", "mayo", "ketchup", "mustard", "salsa", "pico de gallo", "guacamole", "crema", "jam", "preserves", "compote", "honey", "syrup", "aioli", "chutney", "relish", "pesto", "tahini", "hummus", "gochujang", "hoisin", "ponzu", "soy", "teriyaki", "bbq", "barbecue", "worcestershire"}},
	{CategoryDairyEggs, false, []string{"garlic herb butter", "butter", "sour cream", "cream cheese", "heavy cream", "whipping cream", "creme fraiche", "cheese", "parmesan", "mozzarella", "cheddar", "feta", "ricotta", "monterey jack", "milk", "yogurt", "egg", "eggs", "half and half", "cream"}},
	{CategoryMeatSeafood, false, []string{"chicken", "pork", "beef", "turkey", "sausage", "steak", "bacon", "prosciutto", "ham", "chorizo", "meatball", "meatballs", "lamb", "shrimp", "lobster", "salmon", "tilapia", "barramundi", "cod", "tuna", "fish", "scallop", "scallops", "crab", "sirloin", "tenderloin", "cutlets", "thighs", "breast", "ground"}},
	{CategoryBakery, false, []string{"tortillas", "tortilla", "baguette", "ciabatta", "bread", "buns", "bun", "rolls", "naan", "pita", "pitas", "flatbread", "focaccia", "biscuit", "biscuits", "pretzel", "brioche", "english muffin", "waffle", "waffles", "muffin", "muffins", "pie crust", "pie crusts"}},
	{CategoryProduce, false, []string{"green beans", "snap peas", "sweet potato", "sweet potatoes", "grape tomatoes", "cherry tomatoes"}},
	{CategoryPantry, false, []string{"rice", "pasta", "spaghetti", "penne", "rigatoni", "cavatappi", "linguine", "fettuccine", "couscous", "farro", "barley", "bulgur", "noodles", "ramen", "lo mein", "orzo", "quinoa", "flour", "cornstarch", "sugar", "oil", "beans", "chickpeas", "lentils", "crushed tomatoes", "diced tomatoes", "peanuts", "cashews", "almonds", "walnuts", "pecans", "pistachios", "pine nuts", "olives", "sesame seeds", "seeds", "nuts", "raisins", "dried cranberries", "oats", "cocoa", "chocolate", "baking powder", "baking soda", "yeast", "cornmeal", "vanilla"}},
	{CategoryProduce, false, []string{"scallions", "green onion", "green onions", "onion", "onions", "shallot", "shallots", "garlic", "ginger", "lime", "limes", "lemon", "lemons", "orange", "oranges", "apple", "apples", "mango", "pineapple", "peach", "pear", "berries", "kiwi", "avocado", "cilantro", "parsley", "basil", "mint", "dill", "thyme", "rosemary", "chives", "sage", "tomato", "tomatoes", "potato", "potatoes", "zucchini", "squash", "carrot", "carrots", "cabbage", "coleslaw mix", "lettuce", "kale", "spinach", "arugula", "broccoli", "cauliflower", "brussels sprouts", "peas", "asparagus", "mushroom", "mushrooms", "bell pepper", "green pepper", "long green pepper", "jalapeno", "poblano", "chili pepper", "cucumber", "celery", "corn", "bok choy", "radish", "radishes", "beet", "beets", "eggplant", "fennel", "leek", "edamame", "tomatillo", "tomatillos", "parsnip", "parsnips"}},
	{CategoryFrozen, false, []string{"frozen", "ice cream"}},
	{CategoryBeverages, false, []string{"juice", "wine", "beer", "soda", "coffee", "tea"}},
}

var (
	nonWordRe     = regexp.MustCompile(`[^a-z0-9']+`)
	compiledRules = compileCategoryRules()
)

type compiledRule struct {
	category string
	exact    map[string]bool
	phrases  []*regexp.Regexp
}

func compileCategoryRules() []compiledRule {
	out := make([]compiledRule, 0, len(categoryRules))
	for _, r := range categoryRules {
		cr := compiledRule{category: r.category}
		if r.exact {
			cr.exact = map[string]bool{}
			for _, p := range r.phrases {
				cr.exact[normalizeName(p)] = true
			}
		} else {
			for _, p := range r.phrases {
				cr.phrases = append(cr.phrases, regexp.MustCompile(`(^| )`+regexp.QuoteMeta(normalizeName(p))+`( |$)`))
			}
		}
		out = append(out, cr)
	}
	return out
}

// normalizeName lowercases, strips accents, and collapses punctuation to spaces:
// "Crème Fraîche" → "creme fraiche", "Jalapeño" → "jalapeno".
func normalizeName(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	stripped, _, err := transform.String(t, s)
	if err != nil {
		stripped = s
	}
	stripped = strings.ToLower(strings.ReplaceAll(stripped, "&", " and "))
	return strings.TrimSpace(nonWordRe.ReplaceAllString(stripped, " "))
}

// Categorize assigns a grocery category to an ingredient name using ordered
// rules. It returns CategoryOther with confident=false when no rule matches,
// so unknown ingredients are flagged for review instead of guessed.
func Categorize(name string) (category string, confident bool) {
	n := normalizeName(name)
	if n == "" {
		return CategoryOther, false
	}
	for _, r := range compiledRules {
		if r.exact[n] {
			return r.category, true
		}
		for _, p := range r.phrases {
			if p.MatchString(n) {
				return r.category, true
			}
		}
	}
	return CategoryOther, false
}

// NormalizeName returns the canonical comparison form of an ingredient name,
// used as a stable key for ingredients not yet in the catalog.
func NormalizeName(name string) string { return normalizeName(name) }
