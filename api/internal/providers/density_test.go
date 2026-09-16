package providers

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Recipes measure in spoons, stores sell by weight. The owner's Shop tab
// flagged every one of these; each is obviously one package.
func TestCountPackagesForEstimatesVolumeAgainstWeight(t *testing.T) {
	tests := []struct {
		name, category string
		need, size     [2]string
		packages       int
		reason         Reason
		coverage       string
	}{
		// The owner's four.
		{name: "Chili Powder", category: ingredients.CategorySpices, need: [2]string{"5/3", "tbsp"}, size: [2]string{"19/20", "oz"}, packages: 1, coverage: "1 × 0.95 oz covers 1.67 tbsp"},
		{name: "Paprika", category: ingredients.CategorySpices, need: [2]string{"3/2", "tsp"}, size: [2]string{"5/2", "oz"}, packages: 1, coverage: "1 × 2 ½ oz covers 1 ½ tsp"},
		{name: "Apricot Jam", category: ingredients.CategoryCondiments, need: [2]string{"2", "tbsp"}, size: [2]string{"18", "oz"}, packages: 1, coverage: "1 × 18 oz covers 2 tbsp"},
		{name: "Balsamic Vinegar", category: ingredients.CategoryCondiments, need: [2]string{"5", "tsp"}, size: [2]string{"17/2", "oz"}, packages: 1, coverage: "1 × 8 ½ oz covers 5 tsp"},
		// A large need gets a real count: 2 cups of honey is about 23 oz.
		{name: "Honey", category: ingredients.CategoryCondiments, need: [2]string{"2", "cup"}, size: [2]string{"12", "oz"}, packages: 2, coverage: "2 × 12 oz covers 2 cups"},
		// Weight against a bottle measured in fluid ounces.
		{name: "Honey", category: ingredients.CategoryCondiments, need: [2]string{"8", "oz"}, size: [2]string{"12", "floz"}, packages: 1, coverage: "1 × 12 fl oz covers 8 oz"},
		// Nothing specific is known, and a heavy ingredient would need a
		// second package: the count stands, and says it's an estimate.
		{name: "House Glaze", category: ingredients.CategoryOther, need: [2]string{"1", "cup"}, size: [2]string{"10", "oz"}, packages: 1, coverage: "1 × 10 oz covers about 1 cup"},
		// Cloves still can't be weighed, so this one is flagged.
		{name: "Minced Garlic", category: ingredients.CategoryCondiments, need: [2]string{"2", "clove"}, size: [2]string{"8", "oz"}, packages: 1, reason: ReasonUnitNotConvertible},
	}
	for _, tt := range tests {
		t.Run(tt.name+" "+tt.need[1]+" vs "+tt.size[1], func(t *testing.T) {
			size := amount(t, tt.size[0], tt.size[1])
			got := CountPackagesFor([]ingredients.Amount{amount(t, tt.need[0], tt.need[1])}, &size, CoveragePerAmount, Item{Name: tt.name, Category: tt.category})
			if got.Packages != tt.packages || got.Reason != tt.reason {
				t.Errorf("= %d packages, reason %q; want %d, %q", got.Packages, got.Reason, tt.packages, tt.reason)
			}
			if tt.coverage != "" {
				if c := CoverageText(got, &size); c != tt.coverage {
					t.Errorf("CoverageText = %q, want %q", c, tt.coverage)
				}
			}
		})
	}
}

// Exact math plus an estimate: 22 oz measured and a cup of honey (about 12 oz)
// is 3 × 16 oz.
func TestCountPackagesForAddsEstimateToMeasured(t *testing.T) {
	size := amount(t, "16", "oz")
	needs := []ingredients.Amount{amount(t, "22", "oz"), amount(t, "1", "cup")}
	got := CountPackagesFor(needs, &size, CoveragePerAmount, Item{Name: "Honey", Category: ingredients.CategoryCondiments})
	if got.Packages != 3 || got.Reason != "" {
		t.Errorf("= %+v, want 3 packages unflagged", got)
	}
	if c := CoverageText(got, &size); c != "3 × 16 oz covers 22 oz + 1 cup" {
		t.Errorf("CoverageText = %q", c)
	}
}

// CountPackages is exact and never estimates: volume against weight is still
// flagged there, so only package counting for a known item uses density.
func TestCountPackagesStaysExact(t *testing.T) {
	size := amount(t, "18", "oz")
	if got := CountPackages([]ingredients.Amount{amount(t, "2", "tbsp")}, &size); got.Reason != ReasonUnitNotConvertible {
		t.Errorf("CountPackages = %+v, want unit_not_convertible", got)
	}
}

func TestDensityFor(t *testing.T) {
	for _, tt := range []struct {
		item     Item
		want     string
		specific bool
	}{
		{Item{"Peanut Butter", ingredients.CategoryCondiments}, "11/10", true},
		{Item{"Unsalted Butter", ingredients.CategoryDairyEggs}, "19/20", true},
		{Item{"Fresh Basil", ingredients.CategoryProduce}, "3/20", true},
		{Item{"Dried Basil", ingredients.CategorySpices}, "3/10", true},
		{Item{"Olive Oil", ingredients.CategoryCondiments}, "23/25", true},
		{Item{"Za'atar", ingredients.CategorySpices}, "1/2", false},
		{Item{"Something", ""}, "1", false},
	} {
		d, specific := densityFor(tt.item)
		if d.RatString() != tt.want || specific != tt.specific {
			t.Errorf("densityFor(%s) = %s, %v; want %s, %v", tt.item.Name, d.RatString(), specific, tt.want, tt.specific)
		}
	}
}
