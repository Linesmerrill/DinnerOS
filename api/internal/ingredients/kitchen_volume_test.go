package ingredients

import "testing"

// Metric volumes read as US spoons and cups (build 122 review: "18 ml").
func TestKitchenVolume(t *testing.T) {
	for _, tc := range []struct {
		q    Quantity
		unit string
		want string
	}{
		{NewQuantity(18, 1), "ml", "1 Tbsp"},
		{NewQuantity(27, 1), "ml", "2 Tbsp"},
		{NewQuantity(36, 1), "ml", "2 ½ Tbsp"},
		{NewQuantity(54, 1), "ml", "3 ½ Tbsp"},
		{NewQuantity(5, 1), "ml", "1 tsp"},
		{NewQuantity(12, 1), "ml", "2 ½ tsp"},
		{NewQuantity(1, 1), "ml", "¼ tsp"},
		{NewQuantity(15, 1), "ml", "1 Tbsp"},
		{NewQuantity(60, 1), "ml", "¼ cup"},
		{NewQuantity(240, 1), "ml", "1 cup"},
		{NewQuantity(400, 1), "ml", "1 ⅔ cups"},
		{NewQuantity(1, 2), "l", "2 cups"},
	} {
		q, unit, ok := KitchenVolume(tc.q, tc.unit)
		if !ok {
			t.Fatalf("KitchenVolume(%v %s) not converted", tc.q, tc.unit)
		}
		u, err := LookupUnit(unit)
		if err != nil {
			t.Fatal(err)
		}
		if got := q.Format() + " " + u.Label(q); got != tc.want {
			t.Errorf("KitchenVolume(%v %s) = %q; want %q", tc.q, tc.unit, got, tc.want)
		}
	}
	for _, unit := range []string{"tsp", "cup", "oz", "g", "count"} {
		if _, got, ok := KitchenVolume(NewQuantity(2, 1), unit); ok || got != unit {
			t.Errorf("KitchenVolume(2 %s) = %s, %v; want it left alone", unit, got, ok)
		}
	}
}
