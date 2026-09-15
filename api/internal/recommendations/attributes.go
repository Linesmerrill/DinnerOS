package recommendations

import (
	"slices"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Recipe attributes are derived with small, documented keyword heuristics
// (docs/autopilot.md#recipe-attributes). Hard constraints must never be
// weaker than the data: when a heuristic is unsure, a recipe does not satisfy
// a diet and does contain an allergen. Cooking methods are soft preferences,
// so households can override them per recipe.

// RecipeAttributes is what Autopilot knows about a recipe, with the evidence
// behind the derived parts.
type RecipeAttributes struct {
	RecipeID string
	// CookMinutes is recipes.CookMinutes; 0 means unknown.
	CookMinutes int
	TimeBand    string
	// Cuisines are canonical (canonicalCuisine).
	Cuisines []string
	// CuisineRegions are the broader regions of Cuisines, which Autopilot
	// matches too ("italian" → "southern european", "european").
	CuisineRegions []string
	// Tags are canonical (canonicalTag).
	Tags      []string
	Proteins  []string
	Allergens []string
	Diets     []string
	Spicy     bool
	// SpicyEvidence is the tag or ingredient that made it spicy.
	SpicyEvidence string
	Methods       []MethodAttribute
	// MealCategories are set by Service.RecipeAttributes (pairings_categories.go).
	MealCategories []MealCategoryAttribute
	Override       *RecipeOverride
}

// MethodAttribute says whether a recipe suits a cooking method.
type MethodAttribute struct {
	Method string
	Suits  bool
	// Source is heuristic or override.
	Source string
	// HeuristicSuits is the heuristic's answer, even when overridden.
	HeuristicSuits bool
	// Evidence explains the heuristic ("pork tenderloin").
	Evidence string
}

// Method sources.
const (
	SourceHeuristic = "heuristic"
	SourceOverride  = "override"
)

// phrase is a keyword rule. Words match consecutively, allowing plurals.
// A match is ignored when the ingredient also contains any word in unless.
type phrase struct {
	words  []string
	unless []string
}

func p(text string, unless ...string) phrase {
	return phrase{words: tokens(text), unless: unless}
}

// Non-protein uses of protein words ("chicken stock").
var notProtein = []string{"stock", "broth", "bouillon", "base", "concentrate", "seasoning", "fat", "demi", "glace", "flavor", "flavour"}

var proteinRules = map[string][]phrase{
	"chicken": {p("chicken", notProtein...)},
	"beef":    {p("beef", notProtein...), p("steak"), p("sirloin"), p("brisket"), p("ribeye"), p("short rib"), p("tri tip"), p("chuck roast")},
	"pork": {p("pork", notProtein...), p("bacon"), p("ham", "hamburger"), p("prosciutto"), p("pancetta"), p("chorizo"), p("salami"),
		p("pepperoni"), p("sausage", "chicken", "turkey", "vegan", "plant", "veggie", "beef")},
	"turkey":    {p("turkey", notProtein...)},
	"lamb":      {p("lamb", notProtein...)},
	"duck":      {p("duck", notProtein...)},
	"fish":      fishPhrases(true),
	"shellfish": {p("shrimp"), p("prawn"), p("scallop"), p("crab"), p("lobster"), p("mussel"), p("clam"), p("calamari"), p("squid")},
	"tofu":      {p("tofu"), p("tempeh"), p("seitan")},
	"legumes": {p("chickpea"), p("lentil"), p("garbanzo"), p("black bean"), p("pinto bean"), p("kidney bean"), p("cannellini"),
		p("white bean"), p("navy bean"), p("refried bean"), p("edamame")},
	"eggs": {p("egg", "noodle", "roll", "wash")},
}

func fishPhrases(asProtein bool) []phrase {
	var sauce []string
	if asProtein {
		sauce = []string{"sauce"}
	}
	return []phrase{p("fish", sauce...), p("salmon"), p("tuna"), p("cod"), p("tilapia"), p("trout"), p("halibut"), p("haddock"),
		p("pollock"), p("anchovy"), p("anchovies"), p("sardine"), p("mahi"), p("barramundi"), p("snapper"), p("catfish"), p("swordfish")}
}

// allergenRules find allergens among ingredient names. Recipe allergen labels
// are also mapped through allergenLabels.
var allergenRules = map[string][]phrase{
	"milk": {p("milk", "coconut", "almond", "oat", "soy", "rice", "cashew"), p("cream", "tartar", "coconut"), p("butter", "peanut", "almond", "apple", "cashew"),
		p("cheese"), p("parmesan"), p("mozzarella"), p("cheddar"), p("feta"), p("ricotta"), p("mascarpone"), p("yogurt"), p("yoghurt"),
		p("ghee"), p("creme fraiche"), p("crème fraîche"), p("buttermilk"), p("half and half"), p("whey"), p("paneer"), p("queso"),
		p("gruyere"), p("burrata"), p("cotija")},
	"eggs":      {p("egg"), p("mayonnaise"), p("mayo"), p("aioli")},
	"fish":      append(fishPhrases(false), p("worcestershire")),
	"shellfish": {p("shrimp"), p("prawn"), p("scallop"), p("crab"), p("lobster"), p("mussel"), p("clam"), p("oyster", "mushroom"), p("calamari"), p("squid"), p("crawfish"), p("octopus")},
	"tree-nuts": {p("almond"), p("walnut"), p("pecan"), p("cashew"), p("pistachio"), p("hazelnut"), p("macadamia"), p("pine nut"), p("brazil nut")},
	"peanuts":   {p("peanut")},
	"wheat": {p("flour", "rice", "almond", "coconut", "corn", "chickpea", "cassava", "tapioca"), p("bread"), p("breadcrumb"), p("panko"),
		p("pasta"), p("spaghetti"), p("penne"), p("rigatoni"), p("linguine"), p("fettuccine"), p("macaroni"), p("orzo"), p("couscous"),
		p("tortilla", "corn"), p("bun"), p("pita"), p("naan"), p("noodle", "rice", "glass", "shirataki"), p("wheat", "buckwheat"),
		p("soy sauce"), p("crouton"), p("gnocchi"), p("ravioli"), p("tortellini"), p("lasagna"), p("dumpling"), p("wonton"),
		p("dough"), p("baguette"), p("ciabatta"), p("brioche"), p("farro"), p("bulgur"), p("seitan"), p("flatbread"), p("roll", "spring"), p("biscuit")},
	"soy":    {p("soy"), p("soya"), p("tofu"), p("edamame"), p("tempeh"), p("miso"), p("tamari")},
	"sesame": {p("sesame"), p("tahini"), p("furikake")},
}

var allergenLabels = map[string]string{
	"milk": "milk", "dairy": "milk", "lactose": "milk",
	"egg": "eggs", "eggs": "eggs",
	"fish":      "fish",
	"shellfish": "shellfish", "crustacean": "shellfish", "crustaceans": "shellfish", "crustacean shellfish": "shellfish",
	"mollusc": "shellfish", "molluscs": "shellfish", "mollusk": "shellfish", "mollusks": "shellfish",
	"tree nut": "tree-nuts", "tree nuts": "tree-nuts", "nuts": "tree-nuts",
	"peanut": "peanuts", "peanuts": "peanuts",
	"wheat": "wheat", "gluten": "wheat",
	"soy": "soy", "soya": "soy", "soybean": "soy", "soybeans": "soy",
	"sesame": "sesame", "sesame seeds": "sesame",
}

// Ingredients that rule out gluten-free besides wheat.
var glutenRules = []phrase{p("barley"), p("rye"), p("gluten"), p("malt")}

// Land-meat ingredients, including stocks, for vegetarian and pescatarian.
var landMeatRules = []phrase{
	p("chicken"), p("beef"), p("steak"), p("pork"), p("bacon"), p("ham", "hamburger"), p("prosciutto"), p("pancetta"),
	p("chorizo"), p("sausage", "vegan", "plant", "veggie"), p("salami"), p("pepperoni"), p("turkey"), p("lamb"), p("veal"),
	p("duck"), p("venison"), p("bison"), p("brisket"), p("meatball"), p("gelatin"), p("lard"), p("bone broth"), p("ribs"),
}

var seafoodRules = append(fishPhrases(false), p("worcestershire"), p("shrimp"), p("prawn"), p("scallop"), p("crab"),
	p("lobster"), p("mussel"), p("clam"), p("oyster", "mushroom"), p("calamari"), p("squid"))

var honeyRules = []phrase{p("honey")}

// Heat ingredients. Mild spices such as chili powder or paprika don't count.
var spicyRules = []phrase{
	p("habanero"), p("scotch bonnet"), p("ghost pepper"), p("sriracha"), p("gochujang"), p("sambal"), p("hot sauce"),
	p("jalapeño"), p("jalapeno"), p("serrano"), p("cayenne"), p("chili flakes"), p("chile flakes"), p("red pepper flakes"),
	p("chili oil"), p("chili crisp"), p("thai chili"), p("bird's eye"), p("chipotle"),
}

// Smoker-friendly cuts: whole or large cuts of chicken, pork, beef, or turkey.
// Meat that is cut small, pre-cooked, or processed doesn't count: ground,
// sliced, diced, chopped, strips, cutlets, pulled, sausage, or patties.
var cutUnless = []string{
	"ground", "sliced", "diced", "cubed", "chopped", "strip", "cutlet", "shredded", "pulled", "minced", "cooked", "deli",
	"sausage", "mix", "patty", "crumble", "meatball",
}

var smokerCuts = []phrase{
	p("whole chicken", cutUnless...), p("bone in chicken", cutUnless...), p("chicken leg", cutUnless...), p("chicken quarter", cutUnless...),
	p("drumstick", cutUnless...), p("spatchcock", cutUnless...), p("chicken wing", cutUnless...),
	p("pork shoulder", cutUnless...), p("pork butt", cutUnless...), p("boston butt", cutUnless...), p("pork tenderloin", cutUnless...),
	p("pork filet", cutUnless...), p("pork loin", cutUnless...), p("pork chop", cutUnless...), p("pork steak", cutUnless...),
	p("pork belly", cutUnless...), p("baby back rib", cutUnless...), p("spare rib", cutUnless...), p("spareribs", cutUnless...),
	p("pork rib", cutUnless...), p("country style rib", cutUnless...),
	p("brisket", cutUnless...), p("beef rib", cutUnless...), p("short rib", cutUnless...), p("tri tip", cutUnless...), p("chuck roast", cutUnless...),
	p("turkey breast", cutUnless...), p("whole turkey", cutUnless...), p("turkey leg", cutUnless...),
}

// notSmokerDishes are dish shapes that aren't smoker meals even when they use
// a smokable cut: pasta, noodles, soups and stews, pies and casseroles,
// stir-fries, tacos and other wrapped or bowl meals, and ground-meat dishes.
// They're matched in the name's main part, before "with", "over", or "in",
// so a side ("Pork Chops with Garlic Noodles") doesn't count.
var notSmokerDishes = []phrase{
	p("pasta"), p("spaghetti"), p("penne"), p("rigatoni"), p("linguine"), p("fettuccine"), p("cavatappi"), p("macaroni"),
	p("lasagna"), p("noodle"), p("ramen"), p("lo mein"), p("yakisoba"), p("udon"),
	p("soup"), p("stew"), p("pot pie"), p("casserole"), p("bake"), p("fricassee"),
	p("stir fry"), p("fried rice"), p("skillet"),
	p("taco"), p("taquito"), p("burrito"), p("enchilada"), p("quesadilla"), p("nacho"), p("bowl"), p("wrap"), p("pita"),
	p("sandwich"), p("sando"), p("slider"), p("burger"), p("pizza"), p("flatbread"),
	p("meatball"), p("meatloaf"), p("meatloaves"), p("patty"), p("sausage"), p("gyoza"), p("dumpling"), p("wonton"),
	p("bibimbap"), p("donburi"), p("katsu"), p("schnitzel"),
}

// dishHead is the name's main part: the words before "with", "over", or "in".
func dishHead(name []string) []string {
	if i := slices.IndexFunc(name, func(w string) bool { return w == "with" || w == "over" || w == "in" }); i > 0 {
		return name[:i]
	}
	return name
}

// Method keywords found in recipe names, tags, or utensils.
var methodKeywords = map[string][]phrase{
	"smoker":          {p("smoker"), p("smoked"), p("smoking")},
	"grill":           {p("grill"), p("grilled"), p("bbq"), p("barbecue"), p("kebab"), p("skewer")},
	"air-fryer":       {p("air fryer"), p("air fried"), p("airfryer")},
	"slow-cooker":     {p("slow cooker"), p("crock pot"), p("crockpot")},
	"pressure-cooker": {p("instant pot"), p("pressure cooker")},
}

// attributes derives a recipe's attributes. bands decide the time band.
func attributes(r recipes.Recipe, override *RecipeOverride, bands autopilot.TimeBands) RecipeAttributes {
	names := ingredientTokens(r)
	a := RecipeAttributes{
		RecipeID: r.ID, CookMinutes: r.CookMinutes(), Cuisines: canonicalCuisines(r.Cuisines), Tags: canonicalTags(r.Tags),
		Proteins: classifyProteins(names), Override: override,
	}
	a.CuisineRegions = cuisineRegions(a.Cuisines)
	a.TimeBand = string(bands.Of(a.CookMinutes))
	a.Allergens = classifyAllergens(r.Allergens, names)
	a.Diets = classifyDiets(a.Tags, a.Allergens, names)
	a.Spicy, a.SpicyEvidence = classifySpicy(r.Name, a.Tags, r.Ingredients, names)

	labelTokens := [][]string{tokens(r.Name)}
	for _, t := range append(slices.Clone(r.Tags), r.Utensils...) {
		labelTokens = append(labelTokens, tokens(t))
	}
	for _, method := range optionValues(EquipmentOptions) {
		m := MethodAttribute{Method: method, Source: SourceHeuristic}
		m.HeuristicSuits, m.Evidence = heuristicMethod(method, r, names, labelTokens, a.Proteins)
		m.Suits = m.HeuristicSuits
		if override != nil {
			if v, ok := override.Methods[method]; ok {
				m.Suits, m.Source = v, SourceOverride
			}
		}
		a.Methods = append(a.Methods, m)
	}
	return a
}

// Item turns attributes into the provider's catalog item. Its cuisine regions
// let preferences for a region match the region's cuisines.
func (a RecipeAttributes) item(r recipes.Recipe) autopilot.Item {
	it := autopilot.Item{
		ID: r.ID, Cuisines: a.Cuisines, CuisineRegions: a.CuisineRegions, Tags: a.Tags, Proteins: a.Proteins, CookMinutes: a.CookMinutes,
		Servings: slices.Clone(r.Servings), Allergens: a.Allergens, Diets: a.Diets, Spicy: a.Spicy,
	}
	for _, m := range a.Methods {
		if m.Suits {
			it.Methods = append(it.Methods, m.Method)
		}
	}
	for _, line := range r.Ingredients {
		if name := normalizeValue(line.Name); name != "" {
			it.Ingredients = append(it.Ingredients, name)
		}
	}
	return it
}

func heuristicMethod(method string, r recipes.Recipe, names, labels [][]string, proteins []string) (bool, string) {
	if method == "smoker" {
		// An explicit smoker tag or utensil always counts.
		for _, label := range labels[1:] {
			if match(label, []phrase{p("smoker")}) {
				return true, "Tagged or named for smoking"
			}
		}
		// Pasta, tacos, soups, and similar dishes aren't smoker meals, even
		// with a smokable cut or "smoked" in the name.
		head := dishHead(labels[0])
		for _, dish := range notSmokerDishes {
			if match(head, []phrase{dish}) {
				return false, "Not a smoker dish: " + strings.Join(dish.words, " ")
			}
		}
		for i, name := range names {
			if match(name, smokerCuts) {
				return true, r.Ingredients[i].Name
			}
		}
		// A "smoked" name or tag counts with a smokable protein, so "smoked
		// paprika" or smoked salmon from a package don't.
		smokable := slices.ContainsFunc(proteins, func(p string) bool { return slices.Contains([]string{"chicken", "pork", "beef", "turkey"}, p) })
		for _, label := range labels {
			if match(label, []phrase{p("smoker")}) || (smokable && match(label, methodKeywords["smoker"])) {
				return true, "Tagged or named for smoking"
			}
		}
		return false, ""
	}
	for _, label := range labels {
		if match(label, methodKeywords[method]) {
			return true, "Named or tagged " + strings.Join(label, " ")
		}
	}
	return false, ""
}

func classifyProteins(names [][]string) []string {
	var out []string
	for _, o := range ProteinOptions {
		for _, name := range names {
			if match(name, proteinRules[o.Value]) {
				out = append(out, o.Value)
				break
			}
		}
	}
	return out
}

func classifyAllergens(labels []string, names [][]string) []string {
	found := map[string]bool{}
	for _, label := range labels {
		key := normalizeValue(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(label)), "contains "))
		if code, ok := allergenLabels[key]; ok {
			found[code] = true
		}
	}
	for code, rules := range allergenRules {
		for _, name := range names {
			if match(name, rules) {
				found[code] = true
				break
			}
		}
	}
	var out []string
	for _, o := range AllergenOptions {
		if found[o.Value] {
			out = append(out, o.Value)
		}
	}
	return out
}

// classifyDiets returns the diets a recipe satisfies. Evidence of a violation
// always wins; without evidence, a recipe needs ingredient data or a tag that
// asserts the diet.
func classifyDiets(tags, allergens []string, names [][]string) []string {
	any := func(rules []phrase) bool {
		return slices.ContainsFunc(names, func(name []string) bool { return match(name, rules) })
	}
	landMeat, seafood := any(landMeatRules), any(seafoodRules)
	hasData := len(names) > 0
	tagged := func(words ...string) bool {
		return slices.ContainsFunc(tags, func(t string) bool { return slices.Contains(words, t) })
	}
	var out []string
	vegetarian := !landMeat && !seafood && (hasData || tagged("vegetarian", "veggie", "vegan"))
	if vegetarian {
		out = append(out, "vegetarian")
	}
	if !landMeat && (hasData || tagged("pescatarian", "vegetarian", "veggie", "vegan")) {
		out = append(out, "pescatarian")
	}
	if vegetarian && !slices.Contains(allergens, "milk") && !slices.Contains(allergens, "eggs") && !any(honeyRules) &&
		(hasData || tagged("vegan")) {
		out = append(out, "vegan")
	}
	if !slices.Contains(allergens, "wheat") && !any(glutenRules) && (hasData || tagged("gluten-free", "gluten free")) {
		out = append(out, "gluten-free")
	}
	if !slices.Contains(allergens, "milk") && (hasData || tagged("dairy-free", "dairy free")) {
		out = append(out, "dairy-free")
	}
	return out
}

func classifySpicy(name string, tags []string, lines []recipes.RecipeIngredient, names [][]string) (bool, string) {
	for _, t := range tags {
		if slices.Contains(tokens(t), "spicy") {
			return true, "Tagged " + t
		}
	}
	if slices.Contains(tokens(name), "spicy") {
		return true, "Named spicy"
	}
	for i, n := range names {
		if match(n, spicyRules) {
			return true, lines[i].Name
		}
	}
	return false, ""
}

// --- matching -----------------------------------------------------------------

func ingredientTokens(r recipes.Recipe) [][]string {
	out := make([][]string, 0, len(r.Ingredients))
	for _, line := range r.Ingredients {
		out = append(out, tokens(line.Name))
	}
	return out
}

// tokens splits text into lowercase words; "Bone-In" becomes "bone", "in".
func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
}

func match(name []string, rules []phrase) bool {
	for _, rule := range rules {
		if containsWords(name, rule.words) && !slices.ContainsFunc(rule.unless, func(u string) bool { return containsWords(name, tokens(u)) }) {
			return true
		}
	}
	return false
}

// containsWords reports whether words appear consecutively in name, allowing
// plural forms ("thighs", "anchovies").
func containsWords(name, words []string) bool {
	if len(words) == 0 {
		return false
	}
	for start := 0; start+len(words) <= len(name); start++ {
		ok := true
		for i, w := range words {
			if !sameWord(name[start+i], w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func sameWord(got, want string) bool {
	if got == want || got == want+"s" || got == want+"es" {
		return true
	}
	return strings.HasSuffix(want, "y") && got == strings.TrimSuffix(want, "y")+"ies"
}
