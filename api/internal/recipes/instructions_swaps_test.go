package recipes

import (
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

func oneForOne(t *testing.T, id, name string, unitSize grocery.UnitSize, per grocery.Measure, store grocery.Component) *grocery.Specialty {
	t.Helper()
	sizes := []grocery.UnitSize{}
	if unitSize.Unit != "" {
		sizes = append(sizes, unitSize)
	}
	return &grocery.Specialty{
		ID: id, Key: strings.ToLower(name), Name: name, UnitSizes: sizes,
		Choice: &grocery.Choice{Type: grocery.ChoiceStoreAlternative, OptionID: id + ".store", OptionName: store.Name, Per: per, Components: []grocery.Component{store}},
	}
}

// A packet bought as one store ingredient reads as that ingredient, in
// spoons, in the step and on the checklist: "1 oz Sweet Thai Chili Sauce"
// with "mix Bottled Sweet Chili Sauce", and "mix together first" for a single
// jar, both read wrong at the stove (decision 633).
func TestOneForOneSwapsReadAsTheStoreIngredient(t *testing.T) {
	chili := oneForOne(t, "sweet-thai-chili-sauce", "Sweet Thai Chili Sauce", grocery.UnitSize{},
		grocery.Measure{Quantity: *quantityPtr(t, "1"), Unit: "tbsp"},
		grocery.Component{Name: "Bottled Sweet Chili Sauce", Quantity: quantityPtr(t, "1"), Unit: "tbsp"})
	stock := oneForOne(t, "chicken-stock-concentrate", "Chicken Stock Concentrate",
		grocery.UnitSize{Unit: "count", Quantity: *quantityPtr(t, "1"), SizeUnit: "tsp"},
		grocery.Measure{Quantity: *quantityPtr(t, "1"), Unit: "count"},
		grocery.Component{Name: "Chicken Bouillon Base", Quantity: quantityPtr(t, "1"), Unit: "tsp"})
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-chili", "Sweet Thai Chili Sauce", "1", "2", "oz"),
		tacoLine("ing-stock", "Chicken Stock Concentrate", "1", "2", "count"),
		tacoLine("ing-rice", "Jasmine Rice", "1/2", "1", "cup"),
	},
		"In a small pot, combine rice, ¾ cup water, and stock concentrate. Cook until tender.",
		"Stir in sweet Thai chili sauce until combined.")
	in := Annotate(r, 2, grocery.Specialties{"ing-chili": chili, "ing-stock": stock}, false)

	if got := joined(in.Steps[1]); got != "Stir in 2 Tbsp Bottled Sweet Chili Sauce until combined." {
		t.Errorf("step 2 = %q", got)
	}
	for _, st := range in.Steps {
		for _, n := range st.Notes {
			if strings.Contains(n.Text, "mix") {
				t.Errorf("step %d note %q, want none for a one-for-one swap", st.Index, n.Text)
			}
		}
	}
	for _, g := range in.Checklist.ByStep {
		for _, it := range g.Items {
			if it.Prep == "mix together first" {
				t.Errorf("step %d: %s %s says mix together first", g.Index, it.AmountText, it.Name)
			}
			if it.Name == "Sweet Thai Chili Sauce" || it.Name == "Chicken Stock Concentrate" {
				t.Errorf("step %d lists %q, want the store ingredient", g.Index, it.Name)
			}
		}
	}
	if it := item(t, in.Checklist, 2, "Bottled Sweet Chili Sauce"); it.AmountText != "2 Tbsp" {
		t.Errorf("chili row = %+v", it)
	}
	if it := item(t, in.Checklist, 1, "Chicken Bouillon Base"); it.AmountText != "1 tsp" {
		t.Errorf("stock row = %+v", it)
	}
}

// Butter used across steps has one Have Ready row with the whole recipe's
// amount and a line per step, so the cook sees all 3 Tbsp before starting;
// the step that softens it says so instead of "cut into pieces" (decision
// 634).
func TestHaveReadyShowsAllTheButter(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-butter", "Butter", "3", "6", "tbsp"),
		tacoLine("ing-pita", "Whole Wheat Pitas", "2", "4", "count"),
	},
		"In a small pot, melt 1 Tbsp butter over medium heat.",
		"Meanwhile, bring 2 Tbsp butter to room temperature.",
		"Toast pitas until warm. Spread with softened butter, then cut each pita into quarters.")
	in := Annotate(r, 2, nil, false)
	var rows []CookStepItem
	for _, g := range in.Checklist.ByStep {
		if g.Index != 0 {
			continue
		}
		for _, it := range g.Items {
			if it.Name == "Butter" {
				rows = append(rows, it)
			}
		}
	}
	if len(rows) != 1 {
		t.Fatalf("Have Ready butter rows = %+v, want one", rows)
	}
	b := rows[0]
	if b.AmountText != "3 Tbsp" || b.Prep != "for steps 1, 2 and 3" ||
		strings.Join(b.Parts, "|") != "1 Tbsp for step 1, melted|2 Tbsp for step 2, softened" {
		t.Errorf("butter = %+v", b)
	}
}

// Butter only one later step uses reads as before: that step's amount, cut
// into pieces, for that step.
func TestHaveReadyButterForOneStep(t *testing.T) {
	r := tacoRecipe([]RecipeIngredient{
		tacoLine("ing-pasta", "Cavatappi Pasta", "6", "9", "oz"),
		tacoLine("ing-butter", "Butter", "1", "2", "tbsp"),
	},
		"Cook cavatappi until al dente; drain.",
		"Stir drained cavatappi and 1 Tbsp butter into pan.")
	in := Annotate(r, 2, nil, false)
	if it := item(t, in.Checklist, 0, "Butter"); it.AmountText != "1 Tbsp" || it.Prep != "cut into pieces, for step 2" || len(it.Parts) != 0 {
		t.Errorf("butter = %+v", it)
	}
}
