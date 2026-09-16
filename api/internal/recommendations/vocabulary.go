package recommendations

import (
	"cmp"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Option is a choice the app can offer.
type Option struct {
	Value string
	Label string
	// Description explains the option, when useful.
	Description string
	// RecipeCount is how many of the household's main-meal recipes carry
	// the value; 0 for starter values the catalog doesn't use yet.
	RecipeCount int
}

// Vocabulary is everything the onboarding and preference screens offer.
type Vocabulary struct {
	Cuisines    []Option
	Tags        []Option
	Proteins    []Option
	Diets       []Option
	Allergens   []Option
	Equipment   []Option
	Novelty     []Option
	TimeBands   []Option
	Frequencies []Option
	Days        []Option
	// MealCategories and PairingFrequencies are for pairing rules.
	MealCategories     []Option
	PairingFrequencies []Option
	// CatalogRecipes is how many main-meal recipes the counts cover.
	CatalogRecipes int
}

// MaxVocabularyOptions bounds the cuisine and tag lists.
const MaxVocabularyOptions = 60

// Fixed choice lists. Diets, allergens, proteins, and equipment are closed:
// the attribute heuristics recognize exactly these values.
var (
	DietOptions = []Option{
		{Value: "vegetarian", Label: "Vegetarian", Description: "No meat or fish"},
		{Value: "pescatarian", Label: "Pescatarian", Description: "Fish, but no other meat"},
		{Value: "vegan", Label: "Vegan", Description: "No animal products"},
		{Value: "gluten-free", Label: "Gluten-free"},
		{Value: "dairy-free", Label: "Dairy-free"},
	}
	AllergenOptions = []Option{
		{Value: "milk", Label: "Milk"}, {Value: "eggs", Label: "Eggs"}, {Value: "fish", Label: "Fish"},
		{Value: "shellfish", Label: "Shellfish"}, {Value: "tree-nuts", Label: "Tree nuts"}, {Value: "peanuts", Label: "Peanuts"},
		{Value: "wheat", Label: "Wheat"}, {Value: "soy", Label: "Soy"}, {Value: "sesame", Label: "Sesame"},
	}
	ProteinOptions = []Option{
		{Value: "chicken", Label: "Chicken"}, {Value: "beef", Label: "Beef"}, {Value: "pork", Label: "Pork"},
		{Value: "turkey", Label: "Turkey"}, {Value: "lamb", Label: "Lamb"}, {Value: "duck", Label: "Duck"},
		{Value: "fish", Label: "Fish"}, {Value: "shellfish", Label: "Shellfish"}, {Value: "tofu", Label: "Tofu & tempeh"},
		{Value: "legumes", Label: "Beans & lentils"}, {Value: "eggs", Label: "Eggs"},
	}
	EquipmentOptions = []Option{
		{Value: "smoker", Label: "Smoker", Description: "Whole or large cuts of chicken, pork, beef, or turkey"},
		{Value: "grill", Label: "Grill"},
		{Value: "air-fryer", Label: "Air fryer"},
		{Value: "slow-cooker", Label: "Slow cooker"},
		{Value: "pressure-cooker", Label: "Instant Pot or pressure cooker"},
	}
	NoveltyOptions = []Option{
		{Value: "favorites", Label: "Mostly favorites", Description: "Stick to meals we know we like"},
		{Value: "balanced", Label: "A mix", Description: "Favorites with something new now and then"},
		{Value: "adventurous", Label: "Try new things", Description: "Lean toward meals we haven't had"},
	}
	TimeBandOptions = []Option{
		{Value: "quick", Label: "Quick", Description: "Up to the quick limit (20 min by default)"},
		{Value: "medium", Label: "Medium", Description: "Up to the medium limit (35 min by default)"},
		{Value: "long", Label: "Long cook OK", Description: "Longer than the medium limit"},
	}
	FrequencyOptions = []Option{
		{Value: "every_week", Label: "Every week"},
		{Value: "at_most_once", Label: "At most once a week"},
	}
	DayOptions = []Option{
		{Value: "mon", Label: "Monday"}, {Value: "tue", Label: "Tuesday"}, {Value: "wed", Label: "Wednesday"},
		{Value: "thu", Label: "Thursday"}, {Value: "fri", Label: "Friday"}, {Value: "sat", Label: "Saturday"},
		{Value: "sun", Label: "Sunday"},
	}

	// Starter cuisines and tags are offered before the catalog has any.
	starterCuisines = []string{
		"Chinese", "French", "Greek", "Indian", "Italian", "Japanese", "Korean",
		"Mediterranean", "Mexican", "Middle Eastern", "North American", "Spanish", "Thai", "Vietnamese",
	}
	starterTags = []string{
		"Comfort Food", "Curry", "Family Friendly", "Healthy", "High Protein", "Low Carb", "One Pot",
		"Pasta", "Pizza", "Quick", "Rice Bowl", "Salad", "Sandwich", "Sheet Pan", "Soup", "Stir Fry", "Tacos",
	}
)

func optionValues(options []Option) []string {
	out := make([]string, 0, len(options))
	for _, o := range options {
		out = append(out, o.Value)
	}
	return out
}

func optionLabel(options []Option, value string) string {
	for _, o := range options {
		if o.Value == value {
			return o.Label
		}
	}
	return value
}

// buildVocabulary counts cuisines, tags, and proteins across the household's
// main meals and merges in the starter lists.
func buildVocabulary(catalog []recipes.Recipe) Vocabulary {
	v := Vocabulary{
		Diets: DietOptions, Allergens: AllergenOptions, Equipment: EquipmentOptions, Novelty: NoveltyOptions,
		TimeBands: TimeBandOptions, Frequencies: FrequencyOptions, Days: DayOptions,
	}
	cuisines, tags := newCounter(cuisineValues), newCounter(tagValues)
	proteinCounts := map[string]int{}
	for _, r := range catalog {
		if r.IsAddon {
			continue
		}
		v.CatalogRecipes++
		cuisines.addAll(r.Cuisines)
		tags.addAll(r.Tags)
		for _, p := range classifyProteins(ingredientTokens(r)) {
			proteinCounts[p]++
		}
	}
	v.Cuisines = cuisines.options(starterCuisines, v.CatalogRecipes)
	v.Tags = tags.options(starterTags, v.CatalogRecipes)
	for _, o := range ProteinOptions {
		o.RecipeCount = proteinCounts[o.Value]
		v.Proteins = append(v.Proteins, o)
	}
	return v
}

// valueKind says how a vocabulary list reads catalog values.
type valueKind struct {
	// canonical maps a source label to its stored value.
	canonical func(string) string
	// expand returns every value a recipe with the canonical value carries.
	expand func(string) []string
	// label returns a known value's display label, or "".
	label func(string) string
	// hidden reports whether a value is bookkeeping rather than a real
	// choice, so it is never offered. count is how many of the catalog's
	// total main meals carry it. nil means nothing is hidden.
	hidden func(value string, count, total int) bool
}

var (
	// A cuisine counts for its region too, so each count is how many recipes
	// a like of that value matches.
	cuisineValues = valueKind{canonical: canonicalCuisine, expand: withAncestors, label: cuisineLabel}
	tagValues     = valueKind{
		canonical: canonicalTag,
		expand:    func(v string) []string { return []string{v} },
		label:     func(string) string { return "" },
		hidden:    hiddenTag,
	}
)

// counter counts canonical values per recipe and remembers the most common
// source spelling of each.
type counter struct {
	kind      valueKind
	counts    map[string]int
	spellings map[string]map[string]int
}

func newCounter(kind valueKind) *counter {
	return &counter{kind: kind, counts: map[string]int{}, spellings: map[string]map[string]int{}}
}

// addAll counts one recipe's values.
func (c *counter) addAll(values []string) {
	seen := map[string]bool{}
	for _, raw := range values {
		spelling := strings.Join(strings.Fields(raw), " ")
		value := c.kind.canonical(spelling)
		if value == "" || seen[value] || len(value) > MaxValueLength {
			continue
		}
		if c.spellings[value] == nil {
			c.spellings[value] = map[string]int{}
		}
		c.spellings[value][spelling]++
		for _, v := range c.kind.expand(value) {
			if !seen[v] {
				seen[v] = true
				c.counts[v]++
			}
		}
	}
}

// label is the known label, or the most common source spelling.
func (c *counter) label(value string) string {
	if label := c.kind.label(value); label != "" {
		return label
	}
	best, bestN := "", 0
	for label, n := range c.spellings[value] {
		if n > bestN || (n == bestN && label < best) {
			best, bestN = label, n
		}
	}
	return best
}

// options returns counted values (most used first), then starters the
// catalog doesn't use (alphabetically), capped at MaxVocabularyOptions.
// Hidden values are dropped before the cap, so hiding one frees a slot for a
// real choice. total is how many main meals the counts cover.
func (c *counter) options(starters []string, total int) []Option {
	hide := func(value string, n int) bool { return c.kind.hidden != nil && c.kind.hidden(value, n, total) }
	var counted []Option
	for value, n := range c.counts {
		if hide(value, n) {
			continue
		}
		counted = append(counted, Option{Value: value, Label: c.label(value), RecipeCount: n})
	}
	slices.SortFunc(counted, func(a, b Option) int {
		if a.RecipeCount != b.RecipeCount {
			return cmp.Compare(b.RecipeCount, a.RecipeCount)
		}
		return cmp.Compare(a.Value, b.Value)
	})
	for _, s := range starters {
		value := c.kind.canonical(s)
		if c.counts[value] > 0 || hide(value, 0) || slices.ContainsFunc(counted, func(o Option) bool { return o.Value == value }) {
			continue
		}
		label := c.kind.label(value)
		if label == "" {
			label = s
		}
		counted = append(counted, Option{Value: value, Label: label})
	}
	return counted[:min(len(counted), MaxVocabularyOptions)]
}

// normalizeValue is the stored form of a cuisine, tag, or ingredient choice:
// trimmed, lowercase, single-spaced.
func normalizeValue(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
