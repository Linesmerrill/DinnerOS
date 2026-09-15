package recommendations

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Meal categories are the kinds of dinner that pairings match on ("pasta",
// "soup"). Recipes don't carry them, so they are derived at read time from a
// recipe's name, then its tags, then its cuisines, with a small keyword
// heuristic (docs/autopilot.md#meal-categories). Households override them per
// recipe, as with cooking methods, and overrides always win.
//
// The heuristic gives a recipe at most one category: the one whose keyword
// comes last in the name, because dish names end with the dish ("Spicy Beef
// Taco Rigatoni" is pasta, "Chicken Noodle Soup" is soup, "Greek Salad Pita
// Pockets" are sandwiches). Overrides can add more.

// MealCategoryOptions is the closed list of meal categories, in display order.
var MealCategoryOptions = []Option{
	{Value: "pasta", Label: "Pasta"},
	{Value: "soup", Label: "Soup, stew & chili"},
	{Value: "salad", Label: "Salad"},
	{Value: "tacos", Label: "Tacos & Mexican"},
	{Value: "curry", Label: "Curry"},
	{Value: "bowl", Label: "Rice & grain bowls"},
	{Value: "stir-fry", Label: "Stir-fry & noodles"},
	{Value: "sandwich", Label: "Burgers & sandwiches"},
	{Value: "pizza", Label: "Pizza & flatbread"},
}

type categoryKeyword struct {
	words []string
	// final: the keyword counts only as the name's last word. "Chili" is the
	// dish in "Turkey & Bean Chili" and a flavor in "Sweet Chili Pork Bowls".
	final bool
}

type mealCategoryRule struct {
	value string
	// short names the category in explanations ("with pasta", "Your rule:
	// pasta → Garlic Bread"); week names its weeks ("pasta weeks").
	short, week string
	keywords    []categoryKeyword
}

// keywords builds keyword rules; a trailing "$" marks a final-word keyword.
func keywords(list ...string) []categoryKeyword {
	out := make([]categoryKeyword, 0, len(list))
	for _, k := range list {
		out = append(out, categoryKeyword{words: tokens(strings.TrimSuffix(k, "$")), final: strings.HasSuffix(k, "$")})
	}
	return out
}

// mealCategoryRules are in MealCategoryOptions order. Words match with plural
// forms ("tacos", "sandwiches", "curries").
var mealCategoryRules = []mealCategoryRule{
	{"pasta", "pasta", "pasta", keywords("pasta", "spaghetti", "penne", "rigatoni", "linguine", "fettuccine", "fettuccini",
		"cavatappi", "macaroni", "mac cheese", "mac and cheese", "lasagna", "lasagne", "ravioli", "tortellini", "gnocchi", "orzo",
		"ziti", "bucatini", "pappardelle", "tagliatelle", "orecchiette", "farfalle", "rotini", "fusilli", "campanelle", "gemelli",
		"manicotti", "carbonara")},
	{"soup", "soup", "soup", keywords("soup", "stew", "chowder", "bisque", "gumbo", "broth", "pho", "minestrone", "pozole",
		"chili con carne", "chili verde", "chili$")},
	{"salad", "salad", "salad", keywords("salad")},
	{"tacos", "tacos", "taco", keywords("taco", "burrito", "enchilada", "quesadilla", "fajita", "tostada", "flauta", "taquito",
		"nacho", "chimichanga", "tamale")},
	{"curry", "curry", "curry", keywords("curry", "curried", "masala", "korma", "vindaloo", "dal", "dahl")},
	{"bowl", "bowls", "bowl", keywords("bowl", "fried rice", "bibimbap", "donburi", "poke")},
	{"stir-fry", "stir-fry", "stir-fry", keywords("stir fry", "lo mein", "chow mein", "ramen", "yakisoba", "udon", "soba",
		"pad thai", "noodle", "vermicelli", "bami")},
	{"sandwich", "burgers & sandwiches", "sandwich", keywords("burger", "cheeseburger", "sandwich", "sando", "wrap", "pita",
		"slider", "panini", "hoagie", "gyro", "banh mi")},
	{"pizza", "pizza", "pizza", keywords("pizza", "flatbread", "calzone")},
}

// mealCategoryCuisines are cuisines that put a recipe without a keyword in a
// category.
var mealCategoryCuisines = map[string]string{"mexican": "tacos", "tex-mex": "tacos"}

func mealCategoryFor(value string) mealCategoryRule {
	for _, r := range mealCategoryRules {
		if r.value == value {
			return r
		}
	}
	return mealCategoryRule{value: value, short: value, week: value}
}

// MealCategoryAttribute says whether a recipe is in a meal category.
type MealCategoryAttribute struct {
	Category string
	Suits    bool
	// Source is heuristic or override.
	Source string
	// HeuristicSuits is the heuristic's answer, even when overridden.
	HeuristicSuits bool
	// Evidence explains the heuristic ("Named rigatoni").
	Evidence string
}

// lastMatchEnd returns the index of the last word of k's last match in name,
// or -1.
func lastMatchEnd(name []string, k categoryKeyword) int {
	n := len(k.words)
	if n == 0 {
		return -1
	}
	for start := len(name) - n; start >= 0; start-- {
		if k.final && start+n != len(name) {
			continue
		}
		matched := true
		for i, w := range k.words {
			if !sameWord(name[start+i], w) {
				matched = false
				break
			}
		}
		if matched {
			return start + n - 1
		}
	}
	return -1
}

// heuristicMealCategory returns a recipe's meal category and the evidence, or
// "" when nothing matches. In order: the keyword that comes last in the name;
// tags, when they point at exactly one category; a cuisine in
// mealCategoryCuisines.
func heuristicMealCategory(r recipes.Recipe) (category, evidence string) {
	name := tokens(r.Name)
	bestEnd := -1
	for _, rule := range mealCategoryRules {
		for _, k := range rule.keywords {
			if end := lastMatchEnd(name, k); end > bestEnd {
				bestEnd, category = end, rule.value
				evidence = "Named " + strings.Join(name[end-len(k.words)+1:end+1], " ")
			}
		}
	}
	if category != "" {
		return category, evidence
	}
	var tagged []string
	for _, t := range r.Tags {
		words := tokens(t)
		for _, rule := range mealCategoryRules {
			if slices.ContainsFunc(rule.keywords, func(k categoryKeyword) bool { return lastMatchEnd(words, k) >= 0 }) {
				if !slices.Contains(tagged, rule.value) {
					tagged = append(tagged, rule.value)
				}
				evidence = "Tagged " + normalizeValue(t)
			}
		}
	}
	if len(tagged) == 1 {
		return tagged[0], evidence
	}
	for _, c := range canonicalCuisines(r.Cuisines) {
		if value, ok := mealCategoryCuisines[c]; ok {
			return value, cuisineLabel(c) + " cuisine"
		}
	}
	return "", ""
}

// mealCategoryAttributes returns one attribute per meal category, with the
// household's overrides applied.
func mealCategoryAttributes(r recipes.Recipe, override *RecipeOverride) []MealCategoryAttribute {
	heuristic, evidence := heuristicMealCategory(r)
	out := make([]MealCategoryAttribute, 0, len(MealCategoryOptions))
	for _, o := range MealCategoryOptions {
		a := MealCategoryAttribute{Category: o.Value, Source: SourceHeuristic, HeuristicSuits: o.Value == heuristic}
		if a.HeuristicSuits {
			a.Evidence = evidence
		}
		a.Suits = a.HeuristicSuits
		if override != nil {
			if v, ok := override.MealCategories[o.Value]; ok {
				a.Suits, a.Source = v, SourceOverride
			}
		}
		out = append(out, a)
	}
	return out
}

// recipeMealCategories returns the categories a main meal is in, overrides
// applied. Add-ons are never in a category.
func recipeMealCategories(r recipes.Recipe, override *RecipeOverride) []string {
	if r.IsAddon {
		return nil
	}
	var out []string
	for _, a := range mealCategoryAttributes(r, override) {
		if a.Suits {
			out = append(out, a.Category)
		}
	}
	return out
}

// mealCategoryVocabulary counts the heuristic's categories across the
// household's main meals.
func mealCategoryVocabulary(catalog []recipes.Recipe) []Option {
	counts := map[string]int{}
	for _, r := range catalog {
		if r.IsAddon {
			continue
		}
		if c, _ := heuristicMealCategory(r); c != "" {
			counts[c]++
		}
	}
	out := make([]Option, 0, len(MealCategoryOptions))
	for _, o := range MealCategoryOptions {
		o.RecipeCount = counts[o.Value]
		out = append(out, o)
	}
	return out
}

// SetRecipeOverrides changes a recipe's cooking-method and meal-category
// overrides and returns its attributes. A nil value returns that method or
// category to the heuristic; values left out don't change.
func (s *Service) SetRecipeOverrides(ctx context.Context, householdID, userID, recipeID string, methods, categories map[string]*bool) (RecipeAttributes, error) {
	if len(methods) == 0 && len(categories) == 0 {
		return RecipeAttributes{}, invalidf("name at least one method or meal category")
	}
	allowed := optionValues(MealCategoryOptions)
	for _, c := range slices.Sorted(maps.Keys(categories)) {
		if !slices.Contains(allowed, c) {
			return RecipeAttributes{}, invalidf("mealCategories: %q must be one of %s", c, strings.Join(allowed, ", "))
		}
	}
	if len(methods) > 0 {
		if _, err := s.SetRecipeOverride(ctx, householdID, userID, recipeID, methods); err != nil {
			return RecipeAttributes{}, err
		}
	}
	if len(categories) > 0 {
		if err := s.setRecipeMealCategories(ctx, householdID, userID, recipeID, categories); err != nil {
			return RecipeAttributes{}, err
		}
	}
	return s.RecipeAttributes(ctx, householdID, recipeID)
}

// setRecipeMealCategories records the household's say on a recipe's meal
// categories. Each changed category records an
// autopilot.recipe_override_updated event with its category.
func (s *Service) setRecipeMealCategories(ctx context.Context, householdID, userID, recipeID string, categories map[string]*bool) error {
	if err := required(householdID, userID); err != nil {
		return err
	}
	if _, err := s.recipe(ctx, householdID, recipeID); err != nil {
		return err
	}
	current, err := s.store.GetOverride(ctx, householdID, recipeID)
	if err != nil && !isNotFound(err) {
		return err
	}
	next := maps.Clone(current.MealCategories)
	if next == nil {
		next = map[string]bool{}
	}
	type change struct{ category, value, previous string }
	var changes []change
	for _, c := range slices.Sorted(maps.Keys(categories)) {
		previous := overrideValue(current.MealCategories, c)
		if v := categories[c]; v == nil {
			delete(next, c)
		} else {
			next[c] = *v
		}
		if value := overrideValue(next, c); value != previous {
			changes = append(changes, change{c, value, previous})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	now := s.now().UTC()
	o := RecipeOverride{
		HouseholdID: householdID, RecipeID: recipeID, Methods: current.Methods, MealCategories: next, UpdatedBy: userID, UpdatedAt: now,
	}
	if err := s.store.SaveOverride(ctx, o); err != nil {
		return err
	}
	for _, c := range changes {
		s.record(ctx, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypeAutopilotRecipeOverrideUpdated, RecipeID: recipeID, OccurredAt: now,
			Payload: events.AutopilotRecipeOverrideUpdated{Category: c.category, Value: c.value, Previous: c.previous},
		})
	}
	return nil
}
