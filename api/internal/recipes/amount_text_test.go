package recipes

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// The amount the app shows reads in US kitchen units: a meal kit's "18 ml"
// of ponzu is "1 Tbsp" (build 122 review). Other units are as before.
func TestAmountTextReadsMetricVolumesAsSpoonsAndCups(t *testing.T) {
	for _, tc := range []struct {
		q    ingredients.Quantity
		unit string
		want string
	}{
		{ingredients.NewQuantity(18, 1), "ml", "1 Tbsp"},
		{ingredients.NewQuantity(36, 1), "ml", "2 ½ Tbsp"},
		{ingredients.NewQuantity(240, 1), "ml", "1 cup"},
		{ingredients.NewQuantity(3, 2), "tbsp", "1 ½ Tbsp"},
		{ingredients.NewQuantity(10, 1), "oz", "10 oz"},
		{ingredients.NewQuantity(2, 1), "count", "2"},
	} {
		if got := AmountText(tc.q, tc.unit); got != tc.want {
			t.Errorf("AmountText(%v %s) = %q; want %q", tc.q, tc.unit, got, tc.want)
		}
	}
	a := Amount{Servings: 2, Quantity: "18", Unit: "ml", SourceUnit: "milliliters", RawText: "18 milliliters Ponzu Sauce"}
	if got := NewAmountResponse(a); got.Text != "1 Tbsp" || got.Unit != "ml" || *got.Quantity != "18" {
		t.Errorf("NewAmountResponse = %+v; want text 1 Tbsp with the exact 18 ml kept", got)
	}
}
