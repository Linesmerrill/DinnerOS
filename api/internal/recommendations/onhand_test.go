package recommendations

import (
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
)

// What the household has on hand, weighted for Autopilot: frozen meat and
// produce first, staples and spices left out.
func TestOnHandFromThePantry(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	items := []pantry.Item{
		{DisplayName: "Ground Pork", Category: ingredients.CategoryMeatSeafood, Storage: pantry.StorageFreezer, Quantity: "10", Unit: "oz"},
		{DisplayName: "Poblano Pepper", Category: ingredients.CategoryProduce, Quantity: "4", Unit: "count"},
		{DisplayName: "Sour Cream", Category: ingredients.CategoryDairyEggs, ExpiresOn: "2026-10-02"},
		{DisplayName: "Black Beans", Category: ingredients.CategoryPantry},
		{DisplayName: "Olive Oil", Category: ingredients.CategoryPantry, IsStaple: true},
		{DisplayName: "Chili Powder", Category: ingredients.CategorySpices},
	}
	got := map[string][2]any{}
	for _, h := range onHandFrom(items, now) {
		got[h.Name] = [2]any{h.Weight, h.Label}
	}
	want := map[string][2]any{
		"ground pork":    {1.0, "your frozen ground pork"},
		"poblano pepper": {0.7, "your poblano peppers"},
		"sour cream":     {0.8, "your sour cream"},
		"black beans":    {0.2, "your black beans"},
	}
	if len(got) != len(want) {
		t.Fatalf("on hand = %v, want %v", got, want)
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok || g[1] != w[1] || g[0].(float64) < w[0].(float64)-0.001 || g[0].(float64) > w[0].(float64)+0.001 {
			t.Errorf("%s = %v, want %v", name, g, w)
		}
	}
}
