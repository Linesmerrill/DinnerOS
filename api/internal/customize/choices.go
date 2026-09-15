package customize

import (
	"math/big"
	"slices"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// ChoiceKind says what a choice does to a protein line.
type ChoiceKind string

// Choice kinds.
const (
	KindOriginal   ChoiceKind = "original"
	KindDouble     ChoiceKind = "double"
	KindSwap       ChoiceKind = "swap"
	KindSwapDouble ChoiceKind = "swap_double"
)

// Choice IDs are "original", "double", "swap:<proteinId>", and
// "swap:<proteinId>:double".
const (
	ChoiceOriginal = "original"
	ChoiceDouble   = "double"
	swapPrefix     = "swap:"
	doubleSuffix   = ":double"
	// BadgeDouble is the badge on doubled choices.
	BadgeDouble = "Double portion"
)

// unnamedKeyPrefix keys lines without a catalog ID, as the planner does.
const unnamedKeyPrefix = "name:"

// Line is a protein ingredient line of a recipe at one serving size.
type Line struct {
	// Key is the planner's line key: the catalog ID or "name:<normalized>".
	Key          string
	IngredientID string
	Name         string
	Protein      Protein
	// Quantity is the authored amount in Unit, a mass unit.
	Quantity ingredients.Quantity
	Unit     string
}

// Choice is one way to cook a protein line.
type Choice struct {
	ID   string
	Kind ChoiceKind
	// Label is "Ground Pork", "2x Ground Pork", "Ground Beef", or
	// "2x Ground Beef".
	Label string
	// Protein is what's cooked: the line's own protein for original and
	// double, the swapped-in protein otherwise.
	Protein Protein
	// Factor is the amount used per original amount.
	Factor *big.Rat
	Badge  string
}

// IsSwap reports whether the choice replaces the ingredient.
func (c Choice) IsSwap() bool { return c.Kind == KindSwap || c.Kind == KindSwapDouble }

// LineKey returns the key the planner gives a recipe ingredient line, or ""
// for a line without a usable name.
func LineKey(ing recipes.RecipeIngredient) string {
	if ing.IngredientID != "" {
		return ing.IngredientID
	}
	if n := ingredients.NormalizeName(ing.Name); n != "" {
		return unnamedKeyPrefix + n
	}
	return ""
}

// ProteinLines returns r's protein lines for servings, in recipe order. Only
// lines with an exact, non-zero weight for that serving size qualify, so a
// swap is always a like-for-like weight. A repeated key keeps its first line.
func ProteinLines(t *Table, r recipes.Recipe, servings int) []Line {
	var out []Line
	for _, ing := range r.Ingredients {
		key := LineKey(ing)
		p, ok := t.Match(ing.Name)
		if key == "" || !ok || slices.ContainsFunc(out, func(l Line) bool { return l.Key == key }) {
			continue
		}
		for _, a := range ing.Amounts {
			if a.Servings != servings {
				continue
			}
			q, ok := a.ExactQuantity()
			unit, err := ingredients.LookupUnit(a.Unit)
			if ok && !q.IsZero() && err == nil && unit.Kind == ingredients.KindMass {
				out = append(out, Line{Key: key, IngredientID: ing.IngredientID, Name: ing.Name, Protein: p, Quantity: q, Unit: a.Unit})
			}
			break
		}
	}
	return out
}

// Choices returns every choice for a line of protein p named name, before
// restrictions: original, double, then each swap followed by its double.
func (t *Table) Choices(p Protein, name string) []Choice {
	out := []Choice{
		{ID: ChoiceOriginal, Kind: KindOriginal, Label: name, Protein: p, Factor: big.NewRat(1, 1)},
		{ID: ChoiceDouble, Kind: KindDouble, Label: "2x " + name, Protein: p, Factor: big.NewRat(2, 1), Badge: BadgeDouble},
	}
	for _, q := range t.Swaps(p) {
		ratio := t.Ratio(p.ID, q.ID)
		out = append(out,
			Choice{ID: swapPrefix + q.ID, Kind: KindSwap, Label: q.Name, Protein: q, Factor: ratio},
			Choice{ID: swapPrefix + q.ID + doubleSuffix, Kind: KindSwapDouble, Label: "2x " + q.Name, Protein: q,
				Factor: new(big.Rat).Mul(ratio, big.NewRat(2, 1)), Badge: BadgeDouble})
	}
	return out
}

// Resolve returns the choice with id for a line of protein p named name.
// ok is false when p offers no such choice.
func (t *Table) Resolve(p Protein, name, id string) (Choice, bool) {
	switch {
	case id == ChoiceOriginal, id == ChoiceDouble:
	case strings.HasPrefix(id, swapPrefix):
		target := strings.TrimSuffix(strings.TrimPrefix(id, swapPrefix), doubleSuffix)
		if !slices.ContainsFunc(t.Swaps(p), func(q Protein) bool { return q.ID == target }) {
			return Choice{}, false
		}
	default:
		return Choice{}, false
	}
	for _, c := range t.Choices(p, name) {
		if c.ID == id {
			return c, true
		}
	}
	return Choice{}, false
}

// Restrictions are the household's hard constraints that rule out choices.
// Values are Autopilot's (recommendations.Restrictions).
type Restrictions struct {
	Diets               []string
	Allergens           []string
	ExcludedIngredients []string
	ExcludedProteins    []string
}

// Allows reports whether a choice cooking protein p, as an ingredient named
// name, passes the restrictions. Like Autopilot, an excluded ingredient
// matches names containing its words ("turkey" matches "Ground Turkey").
func (r Restrictions) Allows(p Protein, name string) bool {
	for _, v := range p.Proteins {
		if slices.Contains(r.ExcludedProteins, v) {
			return false
		}
	}
	for _, v := range p.Allergens {
		if slices.Contains(r.Allergens, v) {
			return false
		}
	}
	for _, diet := range r.Diets {
		switch diet {
		case "vegetarian", "vegan":
			if p.Meat || p.Seafood {
				return false
			}
		case "pescatarian":
			if p.Meat {
				return false
			}
		}
	}
	names := [][]string{words(name), words(p.Name)}
	for _, phrase := range r.ExcludedIngredients {
		w := words(phrase)
		if len(w) > 0 && slices.ContainsFunc(names, func(n []string) bool { return containsWords(n, w) }) {
			return false
		}
	}
	return true
}

// Filter returns the choices the restrictions allow, always keeping the
// original and the choice with id keep (a member's existing choice).
func (r Restrictions) Filter(choices []Choice, line Line, keep string) []Choice {
	var out []Choice
	for _, c := range choices {
		name := c.Protein.Name
		if !c.IsSwap() {
			name = line.Name
		}
		if c.Kind == KindOriginal || c.ID == keep || r.Allows(c.Protein, name) {
			out = append(out, c)
		}
	}
	return out
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// containsWords reports whether want appears consecutively in name, allowing
// plurals ("thighs" contains "thigh").
func containsWords(name, want []string) bool {
	for start := 0; start+len(want) <= len(name); start++ {
		ok := true
		for i, w := range want {
			got := name[start+i]
			if got != w && got != w+"s" && got != w+"es" {
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
