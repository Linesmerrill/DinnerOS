package customize

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Build 122, Hark's review item 16: a choice names its ingredient in Title Case
// like the recipe's list does, even when a household typed it in lowercase.
func TestCustomizationsNameIngredientsInTitleCase(t *testing.T) {
	q := ingredients.NewQuantity(10, 1)
	resp := newCustomizationsResponse(Options{Groups: []Group{{
		Line:    Line{Key: "k", Name: "diced chicken thighs", Quantity: q, Unit: "oz"},
		Choices: []ChoiceOption{{Choice: Choice{ID: "swap", Label: "Chicken Breasts"}, IngredientName: "chicken breasts", Quantity: q, Unit: "oz"}},
	}}})
	g := resp.Groups[0]
	if g.IngredientName != "Diced Chicken Thighs" || g.Choices[0].IngredientName != "Chicken Breasts" {
		t.Errorf("names = %q, %q", g.IngredientName, g.Choices[0].IngredientName)
	}
}
