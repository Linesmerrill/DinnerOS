package recipes

import (
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// Build 122, Hark's review items 6 and 16: what the app shows of a recipe
// reads the same way everywhere. Ingredient names are Title Case, a scraped
// "Web Description:" label is gone, and ranges in steps read "30 to 60
// seconds". The stored recipe keeps its own words.
func TestRecipeResponseServesCleanCopy(t *testing.T) {
	r := Recipe{
		ID: "r1", Name: "Pesto Bow Tie Pasta",
		Description: "Web Description: Fun fact: “Rosa” means “pink” in Italian.",
		Ingredients: []RecipeIngredient{{Name: "bow tie pasta"}, {Name: "Jasmine Rice"}, {Name: "sun-dried tomatoes"}},
		Steps:       []Step{{Index: 1, Text: "Stir until fragrant, 30-60 seconds. Use a 9-by-13-inch dish."}},
	}
	resp := newRecipeResponse(r, autopilot.TimeBands{})
	if resp.Description != "Fun fact: “Rosa” means “pink” in Italian." {
		t.Errorf("description = %q", resp.Description)
	}
	var names []string
	for _, l := range resp.Ingredients {
		names = append(names, l.Name)
	}
	if want := []string{"Bow Tie Pasta", "Jasmine Rice", "Sun-Dried Tomatoes"}; !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
	if got := resp.Steps[0].Text; got != "Stir until fragrant, 30 to 60 seconds. Use a 9-by-13-inch dish." {
		t.Errorf("step = %q", got)
	}
	if r.Ingredients[0].Name != "bow tie pasta" || r.Description[:4] != "Web " {
		t.Error("the stored recipe was rewritten")
	}
}

func TestInstructionsResponseServesCleanCopy(t *testing.T) {
	in := Instructions{
		Ingredients: []IngredientState{{Index: 0, Name: "bow tie pasta", SwapName: "chicken breast strips"}},
		Steps: []InstructionStep{{
			Index: 1, Text: "Cook the pasta, 15-18 minutes.", Original: "Cook the bow tie pasta, 15-18 minutes.",
			Segments: []Segment{{Kind: SegmentText, Text: "Cook the "}, {Kind: SegmentIngredient, Text: "pasta", Name: "bow tie pasta"}, {Kind: SegmentText, Text: ", 15-18 minutes."}},
			Timers:   []StepTimer{{Text: "15-18 minutes", LowSeconds: 900, HighSeconds: 1080, StartSeconds: 1080}},
		}},
		Checklist: Checklist{
			All:    []CookIngredient{{ID: "0-x", Name: "bow tie pasta"}},
			ByStep: []CookStepGroup{{Index: 1, Items: []CookStepItem{{ID: "0-x", Name: "scallions", Parts: []string{"scallion whites", "scallion greens"}}}}},
		},
	}
	resp := newInstructionsResponse(in)
	if got := resp.Ingredients[0]; got.Name != "Bow Tie Pasta" || got.SwapName != "Chicken Breast Strips" {
		t.Errorf("ingredient = %q / %q", got.Name, got.SwapName)
	}
	step := resp.Steps[0]
	if step.Text != "Cook the pasta, 15 to 18 minutes." || step.OriginalText != "Cook the bow tie pasta, 15 to 18 minutes." {
		t.Errorf("step = %q / %q", step.Text, step.OriginalText)
	}
	// The app finds each timer in the text by its words, so they change together.
	if step.Timers[0].Text != "15 to 18 minutes" || step.Segments[2].Text != ", 15 to 18 minutes." {
		t.Errorf("timer %q, segment %q", step.Timers[0].Text, step.Segments[2].Text)
	}
	if step.Segments[1].Text != "pasta" {
		t.Errorf("a mention's own words changed: %q", step.Segments[1].Text)
	}
	if resp.Checklist.All[0].Name != "Bow Tie Pasta" {
		t.Errorf("checklist = %q", resp.Checklist.All[0].Name)
	}
	item := resp.Checklist.ByStep[0].Items[0]
	if item.Name != "Scallions" || !slices.Equal(item.Parts, []string{"Scallion Whites", "Scallion Greens"}) {
		t.Errorf("step item = %q %q", item.Name, item.Parts)
	}
}

// An import drops the label before it stores the description.
func TestImportsDropDescriptionLabels(t *testing.T) {
	d := ParseText("Rigatoni Rosa\nWeb Description: Fun fact: rosa means pink.\n\nIngredients\n8 oz rigatoni\n\nSteps\nBoil the pasta.")
	if d.Description != "Fun fact: rosa means pink." {
		t.Errorf("pasted description = %q", d.Description)
	}
	r := buildRecipe(ImportRecipe{Name: "Rigatoni Rosa", Description: " Description: A pink sauce. "}, func(ImportIngredient) string { return "" })
	if r.Description != "A pink sauce." {
		t.Errorf("imported description = %q", r.Description)
	}
}
