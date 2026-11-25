package planning

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Hark's build 121 review, item 14: the Cream Sauce Base's Milk read "16 tsp"
// and its Flour "8 tsp" on Shop. They read "⅓ cup" and "2 ½ Tbsp".
func TestAmountTextReadsAsKitchenMeasures(t *testing.T) {
	unit := func(code string) ingredients.Unit {
		u, err := ingredients.LookupUnit(code)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	for _, tc := range []struct {
		q    ingredients.Quantity
		unit string
		want string
	}{
		{ingredients.NewQuantity(16, 1), "tsp", "⅓ cup"},
		{ingredients.NewQuantity(8, 1), "tsp", "2 ½ Tbsp"},
		{ingredients.NewQuantity(4, 1), "tsp", "1 ½ Tbsp"},
		{ingredients.NewQuantity(16, 3), "tbsp", "⅓ cup"},
		{ingredients.NewQuantity(2, 1), "clove", "2 cloves"},
		{ingredients.NewQuantity(3, 1), "count", "3"},
	} {
		if got := amountText(grocery.Amount{Quantity: tc.q, Unit: unit(tc.unit)}); got != tc.want {
			t.Errorf("amountText(%s %s) = %q, want %q", tc.q, tc.unit, got, tc.want)
		}
	}
}
