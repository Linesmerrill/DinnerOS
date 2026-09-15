package customize

import (
	"math/big"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

const (
	ingPork  = "66e5a1f2c3b4a5d6e7f8a001"
	ingBeef  = "66e5a1f2c3b4a5d6e7f8a002"
	ingOnion = "66e5a1f2c3b4a5d6e7f8a003"
)

func amounts(unit string, bySize ...string) []recipes.Amount {
	var out []recipes.Amount
	for i, q := range bySize {
		out = append(out, recipes.Amount{Servings: 2 * (i + 1), Quantity: q, Unit: unit})
	}
	return out
}

// tacosRecipe is a made-up recipe with one customizable protein and lines
// that don't qualify.
func tacosRecipe() recipes.Recipe {
	return recipes.Recipe{
		ID: "66e5a1f2c3b4a5d6e7f8b001", Name: "One-Pan Pork Tacos", Servings: []int{2, 4},
		Ingredients: []recipes.RecipeIngredient{
			{IngredientID: ingPork, Name: "Ground Pork", Category: "meat-seafood", Amounts: amounts("oz", "10", "20")},
			{IngredientID: ingOnion, Name: "Yellow Onion", Category: "produce", Amounts: amounts("count", "1", "2")},
			{Name: "Chicken Stock Concentrate", Amounts: amounts("count", "1", "2")},
			// No amount, or a count, gives no group.
			{Name: "Shrimp", Amounts: []recipes.Amount{{Servings: 2}, {Servings: 4}}},
			{Name: "Pork Chops", Amounts: amounts("count", "2", "4")},
			// A weight in pounds for one size only.
			{Name: "Chicken Cutlets", Amounts: amounts("lb", "1/2")},
			// A repeated key keeps its first line.
			{IngredientID: ingPork, Name: "Ground Pork", Amounts: amounts("oz", "4", "8")},
		},
	}
}

func choiceIDs(cs []Choice) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestProteinLines(t *testing.T) {
	table := DefaultTable()
	lines := ProteinLines(table, tacosRecipe(), 2)
	if len(lines) != 2 {
		t.Fatalf("ProteinLines(2) = %+v", lines)
	}
	if l := lines[0]; l.Key != ingPork || l.Protein.ID != "ground-pork" || l.Quantity.String() != "10" || l.Unit != "oz" {
		t.Errorf("pork line = %+v", l)
	}
	if l := lines[1]; l.Key != "name:chicken cutlets" || l.Protein.ID != "chicken-cutlets" || l.Quantity.String() != "1/2" || l.Unit != "lb" {
		t.Errorf("cutlets line = %+v", l)
	}
	if lines := ProteinLines(table, tacosRecipe(), 4); len(lines) != 1 || lines[0].Quantity.String() != "20" {
		t.Errorf("ProteinLines(4) = %+v", lines)
	}
	if lines := ProteinLines(table, tacosRecipe(), 3); len(lines) != 0 {
		t.Errorf("ProteinLines(3) = %+v", lines)
	}
	if lines := ProteinLines(table, recipes.Recipe{Servings: []int{2}}, 2); len(lines) != 0 {
		t.Errorf("empty recipe lines = %+v", lines)
	}
}

func TestChoices(t *testing.T) {
	table := DefaultTable()
	pork, _ := table.Match("Ground Pork")
	choices := table.Choices(pork, "Ground Pork")
	wantIDs := []string{
		"original", "double",
		"swap:ground-beef", "swap:ground-beef:double",
		"swap:ground-turkey", "swap:ground-turkey:double",
		"swap:ground-chicken", "swap:ground-chicken:double",
		"swap:chopped-chicken-breast", "swap:chopped-chicken-breast:double",
	}
	if got := choiceIDs(choices); !slices.Equal(got, wantIDs) {
		t.Fatalf("Choices() = %v", got)
	}
	for i, want := range []struct {
		kind         ChoiceKind
		label, badge string
		factor       int64
		protein      string
	}{
		{KindOriginal, "Ground Pork", "", 1, "ground-pork"},
		{KindDouble, "2x Ground Pork", "Double portion", 2, "ground-pork"},
		{KindSwap, "Ground Beef", "", 1, "ground-beef"},
		{KindSwapDouble, "2x Ground Beef", "Double portion", 2, "ground-beef"},
	} {
		c := choices[i]
		if c.Kind != want.kind || c.Label != want.label || c.Badge != want.badge || c.Factor.Cmp(big.NewRat(want.factor, 1)) != 0 || c.Protein.ID != want.protein {
			t.Errorf("choice %d = %+v, want %+v", i, c, want)
		}
	}
	if choices[0].IsSwap() || choices[1].IsSwap() || !choices[2].IsSwap() || !choices[3].IsSwap() {
		t.Error("IsSwap is wrong")
	}
}

func TestResolve(t *testing.T) {
	table := DefaultTable()
	pork, _ := table.Match("Ground Pork")
	for id, want := range map[string]bool{
		"original": true, "double": true, "swap:ground-beef": true, "swap:ground-beef:double": true,
		"swap:chopped-chicken-breast:double": true,
		"swap:shrimp":                        false, // not in the ground family's swaps
		"swap:ground-pork":                   false, // itself
		"swap:ground-beef:triple":            false,
		"swap:":                              false,
		"bogus":                              false,
		"":                                   false,
	} {
		c, ok := table.Resolve(pork, "Ground Pork", id)
		if ok != want || (ok && c.ID != id) {
			t.Errorf("Resolve(%q) = %+v, %v; want %v", id, c, ok, want)
		}
	}
}

func TestRestrictionsFilter(t *testing.T) {
	table := DefaultTable()
	porkLine := ProteinLines(table, tacosRecipe(), 2)[0]
	porkChoices := table.Choices(porkLine.Protein, porkLine.Name)
	shrimp, _ := table.Match("Shrimp")
	shrimpLine := Line{Key: "name:shrimp", Name: "Shrimp", Protein: shrimp}
	shrimpChoices := table.Choices(shrimp, "Shrimp")
	cutlets, _ := table.Match("Chicken Cutlets")
	cutletLine := Line{Key: "name:chicken cutlets", Name: "Chicken Cutlets", Protein: cutlets}
	cutletChoices := table.Choices(cutlets, "Chicken Cutlets")

	for _, tc := range []struct {
		name    string
		r       Restrictions
		line    Line
		choices []Choice
		keep    string
		want    []string
	}{
		{"no restrictions", Restrictions{}, porkLine, porkChoices, "", choiceIDs(porkChoices)},
		{"excluded protein", Restrictions{ExcludedProteins: []string{"beef", "chicken"}}, porkLine, porkChoices, "",
			[]string{"original", "double", "swap:ground-turkey", "swap:ground-turkey:double"}},
		// The original stays even when it hits a restriction; its double doesn't.
		{"excluded original", Restrictions{ExcludedProteins: []string{"pork"}}, porkLine, porkChoices, "",
			[]string{"original", "swap:ground-beef", "swap:ground-beef:double", "swap:ground-turkey", "swap:ground-turkey:double",
				"swap:ground-chicken", "swap:ground-chicken:double", "swap:chopped-chicken-breast", "swap:chopped-chicken-breast:double"}},
		{"excluded ingredient words", Restrictions{ExcludedIngredients: []string{"turkey", "chicken breast"}}, porkLine, porkChoices, "",
			[]string{"original", "double", "swap:ground-beef", "swap:ground-beef:double", "swap:ground-chicken", "swap:ground-chicken:double"}},
		{"allergen", Restrictions{Allergens: []string{"fish"}}, shrimpLine, shrimpChoices, "",
			[]string{"original", "double", "swap:chopped-chicken-breast", "swap:chopped-chicken-breast:double"}},
		{"shellfish allergen keeps the original only", Restrictions{Allergens: []string{"shellfish"}}, shrimpLine, shrimpChoices, "",
			[]string{"original", "swap:salmon", "swap:salmon:double", "swap:tilapia", "swap:tilapia:double", "swap:barramundi",
				"swap:barramundi:double", "swap:cod", "swap:cod:double", "swap:chopped-chicken-breast", "swap:chopped-chicken-breast:double"}},
		{"pescatarian", Restrictions{Diets: []string{"pescatarian"}}, shrimpLine, shrimpChoices, "",
			[]string{"original", "double", "swap:salmon", "swap:salmon:double", "swap:tilapia", "swap:tilapia:double",
				"swap:barramundi", "swap:barramundi:double", "swap:cod", "swap:cod:double"}},
		{"vegetarian", Restrictions{Diets: []string{"vegetarian"}}, cutletLine, cutletChoices, "",
			[]string{"original", "swap:tofu", "swap:tofu:double"}},
		{"soy allergen", Restrictions{Allergens: []string{"soy"}, ExcludedProteins: []string{"pork"}, ExcludedIngredients: []string{"thigh"}}, cutletLine, cutletChoices, "",
			[]string{"original", "double", "swap:chopped-chicken-breast", "swap:chopped-chicken-breast:double",
				"swap:chicken-breast-strips", "swap:chicken-breast-strips:double", "swap:chicken-breasts", "swap:chicken-breasts:double"}},
		{"keeps the current choice", Restrictions{Diets: []string{"vegan"}}, porkLine, porkChoices, "swap:ground-beef",
			[]string{"original", "swap:ground-beef"}},
	} {
		if got := choiceIDs(tc.r.Filter(tc.choices, tc.line, tc.keep)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: Filter() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLineKey(t *testing.T) {
	for _, tc := range []struct {
		ing  recipes.RecipeIngredient
		want string
	}{
		{recipes.RecipeIngredient{IngredientID: ingPork, Name: "Ground Pork"}, ingPork},
		{recipes.RecipeIngredient{Name: "Ground  Pork!"}, "name:ground pork"},
		{recipes.RecipeIngredient{Name: "!!"}, ""},
	} {
		if got := LineKey(tc.ing); got != tc.want {
			t.Errorf("LineKey(%+v) = %q, want %q", tc.ing, got, tc.want)
		}
	}
}
