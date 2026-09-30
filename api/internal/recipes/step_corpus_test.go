package recipes

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// corpusRecipe is one recipe in testdata/step_corpus.json.
type corpusRecipe struct {
	Name        string `json:"name"`
	Servings    []int  `json:"servings"`
	Ingredients []struct {
		Name string `json:"name"`
		// Amounts are "quantity unit" by serving size ("1/2 cup").
		Amounts map[string]string `json:"amounts"`
	} `json:"ingredients"`
	Steps   []string `json:"steps"`
	LeftOut []string `json:"leftOut"`
	// Expect maps a serving size to a step number to text the step must
	// contain.
	Expect map[string]map[string][]string `json:"expect"`
}

func loadCorpus(t *testing.T) []corpusRecipe {
	t.Helper()
	data, err := os.ReadFile("testdata/step_corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Recipes []corpusRecipe `json:"recipes"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f.Recipes
}

func (c corpusRecipe) recipe() Recipe {
	r := Recipe{ID: "corpus", Name: c.Name, Servings: c.Servings}
	for _, ing := range c.Ingredients {
		line := RecipeIngredient{Name: ing.Name}
		for _, n := range c.Servings {
			a := Amount{Servings: n}
			if text, ok := ing.Amounts[strconv.Itoa(n)]; ok {
				if q, u, found := strings.Cut(text, " "); found {
					a.Quantity, a.Unit = q, u
				}
			}
			line.Amounts = append(line.Amounts, a)
		}
		r.Ingredients = append(r.Ingredients, line)
	}
	for i, s := range c.Steps {
		r.Steps = append(r.Steps, Step{Index: i + 1, Text: s})
	}
	return r
}

// TestStepCorpus renders every corpus recipe at every size: none may read
// wrong by CheckSteps, and each pinned phrase must appear.
func TestStepCorpus(t *testing.T) {
	for _, c := range loadCorpus(t) {
		leftOut := grocery.LeftOutSet{}
		for _, name := range c.LeftOut {
			leftOut["name:"+strings.ToLower(name)] = grocery.LeftOut{SkipID: "skip", Scope: grocery.SkipRecipe}
		}
		for _, n := range c.Servings {
			in := AnnotateWith(c.recipe(), n, nil, true, leftOut, true)
			for _, f := range CheckSteps(in) {
				t.Errorf("%s for %d, step %d: %s %q", c.Name, n, f.Step, f.Code, f.Detail)
			}
			for step, wants := range c.Expect[strconv.Itoa(n)] {
				i, _ := strconv.Atoi(step)
				if i < 1 || i > len(in.Steps) {
					t.Errorf("%s: no step %s", c.Name, step)
					continue
				}
				got := joined(in.Steps[i-1])
				for _, want := range wants {
					if !strings.Contains(got, want) {
						t.Errorf("%s for %d, step %d:\n got %q\nwant it to contain %q", c.Name, n, i, got, want)
					}
				}
			}
		}
	}
}

// TestCheckStepsFindsWhatReadsWrong feeds the checks text that reads wrong,
// so a check that stops working fails here rather than passing silently.
func TestCheckStepsFindsWhatReadsWrong(t *testing.T) {
	in := Instructions{
		Servings: 2, ServingOptions: []int{2, 4},
		Ingredients: []IngredientState{{Index: 0, Name: "Cumin"}, {Index: 1, Name: "Saffron"}, {Index: 2, Name: "Cilantro", LeftOut: &grocery.LeftOut{}}},
		Steps: []InstructionStep{{
			Index: 1,
			Text:  "Add 2 Tbsp salt (we used ½ tsp) and 1½ cup water (2 TBSP for 4 servings). Form 10-121½-inch balls. Pat chicken* dry.",
			Segments: []Segment{
				{Kind: SegmentText, Text: "Stir in half the "},
				{Kind: SegmentIngredient, Ingredient: 0, Text: "1 tsp cumin", Amount: &Measure{Unit: "tsp"}},
				{Kind: SegmentText, Text: " and "},
				{Kind: SegmentIngredient, Ingredient: 2, Text: "cilantro"},
			},
		}},
	}
	got := map[string]bool{}
	for _, f := range CheckSteps(in) {
		got[f.Code] = true
	}
	for _, code := range []string{
		FindingBoxNote, FindingDoubleAmount, FindingUnitPlural, FindingMarkup, FindingRunTogether,
		FindingAmountBeforeRelative, FindingLeftOutNotStruck, FindingUnusedIngredient,
	} {
		if !got[code] {
			t.Errorf("CheckSteps missed %s", code)
		}
	}
}
