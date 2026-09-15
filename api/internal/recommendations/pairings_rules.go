package recommendations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// PairingRule is a household habit: when a main meal matches When, suggest
// (or always include) Add with it. Rules are the profile's pairings section.
type PairingRule struct {
	// ID is assigned when the rule is created and kept while it exists.
	ID    string
	Label string
	When  PairingWhen
	Add   PairingTarget
	// Frequency is PairingAlways or PairingSuggest.
	Frequency string
}

// PairingWhen is which main meals a rule applies to. A meal matches when it
// matches every group the rule sets; within a group any value matches.
type PairingWhen struct {
	MealCategories []string
	// Cuisines match a meal's cuisines or their regions.
	Cuisines []string
	Tags     []string
	Proteins []string
}

// PairingTarget is what a pairing adds: an add-on recipe or a grocery item.
type PairingTarget struct {
	RecipeID string
	// RecipeName is the recipe's name when the rule was saved, for display
	// and history.
	RecipeName  string
	GroceryItem *GroceryItem
}

// GroceryItem is a grocery line that isn't a recipe ("Club crackers").
type GroceryItem struct {
	Name string
	// Quantity is exact ("1", "1/2"), or "" for none; Unit is an ingredient
	// unit code, set when Quantity is.
	Quantity string
	Unit     string
}

// Pairing kinds, sources, and frequencies.
const (
	PairingKindRecipe      = "recipe"
	PairingKindGroceryItem = "grocery_item"
	PairingSourceRule      = "rule"
	PairingSourceLearned   = "learned"
	// PairingAlways rules are included with a proposed meal unless the member
	// leaves them out.
	PairingAlways = "always"
	// PairingSuggest rules are offered with the meal.
	PairingSuggest = "suggest"
)

// Pairing limits.
const (
	MaxPairingRules          = 20
	MaxGroceryItemNameLength = 60
	MaxGroceryItemQuantity   = 99
)

// PairingFrequencyOptions are the rule frequencies.
var PairingFrequencyOptions = []Option{
	{Value: PairingAlways, Label: "Always", Description: "Included with the meal unless you leave it out"},
	{Value: PairingSuggest, Label: "Suggest", Description: "Offered with the meal"},
}

var hexID = regexp.MustCompile(`^[0-9a-f]{24}$`)

// Kind returns PairingKindRecipe or PairingKindGroceryItem.
func (t PairingTarget) Kind() string {
	if t.GroceryItem != nil {
		return PairingKindGroceryItem
	}
	return PairingKindRecipe
}

// Key identifies the target: "recipe:<id>" or "grocery:<normalized name>".
func (t PairingTarget) Key() string {
	if t.GroceryItem != nil {
		return groceryKey(t.GroceryItem.Name)
	}
	return "recipe:" + t.RecipeID
}

func groceryKey(name string) string { return "grocery:" + normalizeValue(name) }

// Name is the target's display name.
func (t PairingTarget) Name() string {
	switch {
	case t.GroceryItem != nil:
		return t.GroceryItem.Name
	case t.RecipeName != "":
		return t.RecipeName
	}
	return "recipe " + t.RecipeID
}

// normalizePairingRules validates rules, assigns IDs to new ones, and puts
// their values in canonical form. Rules keep their order.
func normalizePairingRules(rules []PairingRule) ([]PairingRule, error) {
	if len(rules) > MaxPairingRules {
		return nil, invalidf("pairings: at most %d rules", MaxPairingRules)
	}
	out := make([]PairingRule, 0, len(rules))
	for i, rule := range rules {
		field := fmt.Sprintf("pairings[%d]", i)
		nr := PairingRule{ID: strings.ToLower(strings.TrimSpace(rule.ID)), Label: strings.Join(strings.Fields(rule.Label), " ")}
		switch {
		case nr.ID == "":
			nr.ID = newID()
		case !hexID.MatchString(nr.ID):
			return nil, invalidf("%s.id must be the id of a saved rule, or null for a new rule", field)
		case slices.ContainsFunc(out, func(r PairingRule) bool { return r.ID == nr.ID }):
			return nil, invalidf("pairings: more than one rule with id %s", nr.ID)
		}
		if utf8.RuneCountInString(nr.Label) > MaxLabelLength {
			return nil, invalidf("%s.label must be at most %d characters", field, MaxLabelLength)
		}
		var err error
		w, in := &nr.When, rule.When
		if w.MealCategories, err = closedValues(field+".when.mealCategories", in.MealCategories, MealCategoryOptions); err != nil {
			return nil, err
		}
		if w.Cuisines, err = freeValues(field+".when.cuisines", in.Cuisines, MaxRuleValues, MaxValueLength, canonicalCuisine); err != nil {
			return nil, err
		}
		if w.Tags, err = freeValues(field+".when.tags", in.Tags, MaxRuleValues, MaxValueLength, canonicalTag); err != nil {
			return nil, err
		}
		if w.Proteins, err = closedValues(field+".when.proteins", in.Proteins, ProteinOptions); err != nil {
			return nil, err
		}
		if len(w.MealCategories)+len(w.Cuisines)+len(w.Tags)+len(w.Proteins) == 0 {
			return nil, invalidf("%s.when needs at least one meal category, cuisine, tag, or protein", field)
		}
		if nr.Add, err = normalizePairingTarget(field+".add", rule.Add); err != nil {
			return nil, err
		}
		nr.Frequency = normalizeValue(rule.Frequency)
		if nr.Frequency == "" {
			nr.Frequency = PairingSuggest
		}
		if !slices.Contains(optionValues(PairingFrequencyOptions), nr.Frequency) {
			return nil, invalidf("%s.frequency must be always or suggest", field)
		}
		if slices.ContainsFunc(out, func(r PairingRule) bool { return r.Add.Key() == nr.Add.Key() && r.When.equal(nr.When) }) {
			return nil, invalidf("%s repeats another rule for the same meals and item", field)
		}
		out = append(out, nr)
	}
	return out, nil
}

func normalizePairingTarget(field string, t PairingTarget) (PairingTarget, error) {
	id := strings.ToLower(strings.TrimSpace(t.RecipeID))
	switch {
	case (id == "") == (t.GroceryItem == nil):
		return PairingTarget{}, invalidf("%s needs exactly one of recipeId or groceryItem", field)
	case t.GroceryItem == nil && !hexID.MatchString(id):
		return PairingTarget{}, invalidf("%s.recipeId must be a recipe id", field)
	case t.GroceryItem == nil:
		return PairingTarget{RecipeID: id, RecipeName: t.RecipeName}, nil
	}
	g := GroceryItem{Name: strings.Join(strings.Fields(t.GroceryItem.Name), " "), Unit: strings.TrimSpace(t.GroceryItem.Unit)}
	if n := utf8.RuneCountInString(g.Name); n == 0 || n > MaxGroceryItemNameLength {
		return PairingTarget{}, invalidf("%s.groceryItem.name must be 1 to %d characters", field, MaxGroceryItemNameLength)
	}
	raw := strings.TrimSpace(t.GroceryItem.Quantity)
	switch {
	case raw == "" && g.Unit != "":
		return PairingTarget{}, invalidf("%s.groceryItem.unit needs a quantity", field)
	case raw != "":
		q, err := ingredients.ParseQuantity(raw)
		if err != nil || q.IsZero() || q.Cmp(ingredients.NewQuantity(MaxGroceryItemQuantity, 1)) > 0 {
			return PairingTarget{}, invalidf("%s.groceryItem.quantity must be more than 0 and at most %d", field, MaxGroceryItemQuantity)
		}
		g.Quantity = q.String()
		if g.Unit == "" {
			g.Unit = "count"
		}
		if _, err := ingredients.LookupUnit(g.Unit); err != nil {
			return PairingTarget{}, invalidf("%s.groceryItem.unit must be one of %s", field, strings.Join(ingredients.UnitCodes(), ", "))
		}
	}
	return PairingTarget{GroceryItem: &g}, nil
}

func (w PairingWhen) equal(o PairingWhen) bool {
	return slices.Equal(w.MealCategories, o.MealCategories) && slices.Equal(w.Cuisines, o.Cuisines) &&
		slices.Equal(w.Tags, o.Tags) && slices.Equal(w.Proteins, o.Proteins)
}

// onlyCategories reports whether the rule matches on meal categories alone.
func (w PairingWhen) onlyCategories() bool {
	return len(w.MealCategories) > 0 && len(w.Cuisines)+len(w.Tags)+len(w.Proteins) == 0
}

// mealFacts are what pairing rules match a main meal on.
type mealFacts struct {
	categories []string
	// cuisines include the cuisines' regions.
	cuisines []string
	tags     []string
	proteins []string
}

// matches reports whether a meal matches the rule, and the rule's first meal
// category the meal is in ("" when the rule sets none).
func (w PairingWhen) matches(f mealFacts) (bool, string) {
	anyOf := func(want, have []string) bool {
		return len(want) == 0 || slices.ContainsFunc(want, func(v string) bool { return slices.Contains(have, v) })
	}
	if !anyOf(w.MealCategories, f.categories) || !anyOf(w.Cuisines, f.cuisines) || !anyOf(w.Tags, f.tags) || !anyOf(w.Proteins, f.proteins) {
		return false, ""
	}
	for _, c := range w.MealCategories {
		if slices.Contains(f.categories, c) {
			return true, c
		}
	}
	return true, ""
}

// summary describes the meals: "pasta, chicken".
func (w PairingWhen) summary() string {
	var parts []string
	for _, c := range w.MealCategories {
		parts = append(parts, mealCategoryFor(c).short)
	}
	for _, c := range w.Cuisines {
		if label := cuisineLabel(c); label != "" {
			c = label
		}
		parts = append(parts, c)
	}
	parts = append(parts, w.Tags...)
	for _, p := range w.Proteins {
		parts = append(parts, strings.ToLower(optionLabel(ProteinOptions, p)))
	}
	return strings.Join(parts, ", ")
}

// summary describes the rule for history: "Pasta night: pasta → Garlic Bread
// · always".
func (r PairingRule) summary() string {
	s := r.When.summary() + " → " + r.Add.Name() + " · " + r.Frequency
	if r.Label != "" {
		s = r.Label + ": " + s
	}
	return s
}

// diffPairingRules summarizes rule changes as added and removed rules.
func diffPairingRules(old, next []PairingRule) []events.FieldChange {
	summaries := func(rules []PairingRule) []string {
		out := make([]string, 0, len(rules))
		for _, r := range rules {
			out = append(out, r.summary())
		}
		return out
	}
	var d differ
	d.list("pairings", summaries(old), summaries(next))
	return d.changes
}

// resolvePairingTargets checks that recipe targets are add-on recipes in the
// household that can be planned, and records their names.
func (s *Service) resolvePairingTargets(ctx context.Context, householdID string, rules []PairingRule) ([]PairingRule, error) {
	out := slices.Clone(rules)
	for i := range out {
		t := &out[i].Add
		if t.GroceryItem != nil {
			continue
		}
		r, err := s.recipes.Get(ctx, householdID, t.RecipeID)
		switch {
		case errors.Is(err, recipes.ErrNotFound):
			return nil, invalidf("pairings[%d].add.recipeId: recipe not found", i)
		case err != nil:
			return nil, fmt.Errorf("load pairing recipe: %w", err)
		case !r.IsAddon:
			return nil, invalidf("pairings[%d].add.recipeId must be an add-on recipe (a side or dessert)", i)
		case len(r.Servings) == 0:
			return nil, invalidf("pairings[%d].add.recipeId: the recipe has no serving sizes, so it can't be planned", i)
		}
		t.RecipeName = r.Name
	}
	return out, nil
}
