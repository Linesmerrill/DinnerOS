package pantry

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func TestFreezerLifeFollowsTheFDAChart(t *testing.T) {
	meat := ingredients.CategoryMeatSeafood
	for _, tc := range []struct{ name, category, want string }{
		{"Ground Pork", meat, "3–4 months"},
		{"Ground Turkey", meat, "3–4 months"},
		{"Chicken Breast Strips", meat, "9 months"},
		{"Whole Chicken", meat, "1 year"},
		{"Pork Loin", meat, "4–12 months"},
		{"Ribeye Steak", meat, "4–12 months"},
		{"Bacon", meat, "1 month"},
		{"Italian Sausage", meat, "1–2 months"},
		{"Salmon Fillets", meat, "2–3 months"},
		{"Cod", meat, "6–8 months"},
		{"Shrimp", meat, "3–6 months"},
		{"Mystery Protein", meat, ""},
		{"Brioche Buns", ingredients.CategoryBakery, ""},
	} {
		if got := FreezerLife(tc.name, tc.category); got != tc.want {
			t.Errorf("FreezerLife(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBestByIsTheShortEndOfTheFreezerLife(t *testing.T) {
	meat := ingredients.CategoryMeatSeafood
	if got := BestBy("2026-09-27", "Ground Pork", meat); got != "2026-12-27" {
		t.Errorf("ground pork best by = %q, want 2026-12-27", got)
	}
	if got := BestBy("2026-09-27", "Chicken Breast Strips", meat); got != "2027-06-27" {
		t.Errorf("chicken best by = %q, want 2027-06-27", got)
	}
	if got := BestBy("2026-09-27", "Mystery Protein", meat); got != "" {
		t.Errorf("unknown meat best by = %q, want empty", got)
	}
}

// Vacuum sealed raw meat gets the long end of the FDA's freezer range; a
// freezer bag or the store's package, with air on the meat, the short end.
func TestBestByWrapped(t *testing.T) {
	for _, tc := range []struct{ name, wrap, want string }{
		{"Ground Pork", households.FreezerWrapVacuum, "2027-01-29"},
		{"Ground Pork", "", "2027-01-29"},
		{"Ground Pork", households.FreezerWrapBag, "2026-12-29"},
		{"Ground Pork", households.FreezerWrapPackage, "2026-12-29"},
		{"Ribeye Steak", households.FreezerWrapVacuum, "2027-09-29"},
		{"Chicken Breasts", households.FreezerWrapBag, "2027-06-29"},
		{"Flour Tortillas", households.FreezerWrapVacuum, ""},
	} {
		category := ingredients.CategoryMeatSeafood
		if tc.name == "Flour Tortillas" {
			category = ingredients.CategoryBakery
		}
		if got := BestByWrapped("2026-09-29", tc.name, category, tc.wrap); got != tc.want {
			t.Errorf("BestByWrapped(%s, %q) = %q, want %q", tc.name, tc.wrap, got, tc.want)
		}
	}
}
