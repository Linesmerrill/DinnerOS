package ingredients

import "testing"

// Hark's build 121 review, item 14: Shop showed Milk "16 tsp", Flour "8 tsp",
// "Milk 4 tsp" and "5 Tbsp + 1 tsp" of milk. Spoons and cups read as a
// kitchen measures them, never 16 tsp.
func TestKitchenSpoons(t *testing.T) {
	for _, tc := range []struct {
		q    Quantity
		unit string
		want string
	}{
		{NewQuantity(16, 1), "tsp", "⅓ cup"},
		{NewQuantity(8, 1), "tsp", "2 Tbsp + 2 tsp"},
		{NewQuantity(4, 1), "tsp", "1 Tbsp + 1 tsp"},
		{NewQuantity(16, 3), "tbsp", "⅓ cup"}, // 5 Tbsp + 1 tsp
		{NewQuantity(10, 1), "tsp", "3 Tbsp + 1 tsp"},
		{NewQuantity(2, 1), "tsp", "2 tsp"},
		{NewQuantity(1, 2), "tsp", "½ tsp"},
		{NewQuantity(1, 3), "tsp", "¼ tsp"},
		{NewQuantity(1, 8), "tsp", "⅛ tsp"},
		{NewQuantity(1, 16), "tsp", "⅛ tsp"},
		{NewQuantity(3, 1), "tsp", "1 Tbsp"},
		{NewQuantity(59, 20), "tsp", "1 Tbsp"},
		{NewQuantity(9, 2), "tsp", "1 ½ Tbsp"},
		{NewQuantity(3, 1), "tbsp", "3 Tbsp"},
		{NewQuantity(12, 1), "tsp", "¼ cup"},
		{NewQuantity(4, 1), "tbsp", "¼ cup"},
		{NewQuantity(6, 1), "tbsp", "6 Tbsp"},
		{NewQuantity(12, 1), "tbsp", "¾ cup"},
		{NewQuantity(14, 1), "tsp", "⅓ cup"},
		{NewQuantity(3, 2), "cup", "1 ½ cups"},
		{NewQuantity(1, 1), "cup", "1 cup"},
		{NewQuantity(11, 10), "cup", "1 cup"},
		{NewQuantity(2, 3), "cup", "⅔ cup"},
		{NewQuantity(49, 1), "tsp", "1 cup"},
	} {
		got, ok := KitchenSpoons(tc.q, tc.unit)
		if !ok || got != tc.want {
			t.Errorf("KitchenSpoons(%s %s) = %q, %v; want %q", tc.q, tc.unit, got, ok, tc.want)
		}
	}
	for _, unit := range []string{"ml", "oz", "g", "count", "floz", "clove"} {
		if got, ok := KitchenSpoons(NewQuantity(16, 1), unit); ok {
			t.Errorf("KitchenSpoons(16 %s) = %q; want it left alone", unit, got)
		}
	}
	if got, ok := KitchenSpoons(NewQuantity(0, 1), "tsp"); ok {
		t.Errorf("KitchenSpoons(0 tsp) = %q; want it left alone", got)
	}
}
