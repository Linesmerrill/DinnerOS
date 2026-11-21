package recipes

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
)

func TestNutrientText(t *testing.T) {
	for _, tc := range []struct {
		n    Nutrient
		want string
	}{
		{Nutrient{Name: "Calories", Amount: 640, Unit: "kcal"}, "640 Cal"},
		{Nutrient{Name: "Calories", Amount: 640, Unit: "Calories"}, "640 Cal"},
		{Nutrient{Name: "Protein", Amount: 39, Unit: "g"}, "39 g"},
		{Nutrient{Name: "Saturated Fat", Amount: 1.5, Unit: "g"}, "1.5 g"},
		{Nutrient{Name: "Sodium", Amount: 820, Unit: "mg"}, "820 mg"},
		{Nutrient{Name: "Energy (kJ)", Amount: 2678, Unit: "kJ"}, "2678 kJ"},
		{Nutrient{Name: "Fiber", Amount: 4}, "4"},
	} {
		if got := NutrientText(tc.n); got != tc.want {
			t.Errorf("NutrientText(%+v) = %q, want %q", tc.n, got, tc.want)
		}
	}
	if r := NewNutrientResponse(Nutrient{Name: "Calories", Amount: 610, Unit: "kcal"}); r.Text != "610 Cal" || r.Unit != "kcal" {
		t.Errorf("response = %+v; the stored unit stays and the text says Cal", r)
	}
}

func TestPublicMessageReadsLikeASentence(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: Calories can be up to 5000 Cal.", ErrInvalidDraft), "Calories can be up to 5000 Cal."},
		{fmt.Errorf("%w: name is required", ErrInvalidDraft), "Name is required"},
		{fmt.Errorf("%w: line 3 has no name", ErrInvalidImport), "Line 3 has no name"},
		{errors.New("recipes: something else"), "Something else"},
	} {
		if got := publicMessage(tc.err); got != tc.want {
			t.Errorf("publicMessage(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestAmountTextUsesKitchenFractions(t *testing.T) {
	for _, tc := range []struct {
		a    Amount
		want string
	}{
		{Amount{Servings: 2, Quantity: "1 1/2", Unit: "cup"}, "1 ½ cups"},
		{Amount{Servings: 2, Quantity: "3/2", Unit: "tbsp"}, "1 ½ Tbsp"},
		{Amount{Servings: 2, Quantity: "10", Unit: "oz"}, "10 oz"},
		{Amount{Servings: 2, Quantity: "2", Unit: "count"}, "2"},
		{Amount{Servings: 2, Quantity: "1/4", Unit: "tsp"}, "¼ tsp"},
		{Amount{Servings: 2, RawText: "to taste"}, ""},
	} {
		if got := NewAmountResponse(tc.a).Text; got != tc.want {
			t.Errorf("text(%+v) = %q, want %q", tc.a, got, tc.want)
		}
	}
}

func TestSummaryTitleUsesAmpersand(t *testing.T) {
	got := NewRecipeSummaryResponse(RecipeSummary{Name: "Orange and Cashew Chicken Stir-Fry"}, ratings.Summary{}, autopilot.TimeBands{})
	if got.Name != "Orange & Cashew Chicken Stir-Fry" {
		t.Errorf("name = %q", got.Name)
	}
}
