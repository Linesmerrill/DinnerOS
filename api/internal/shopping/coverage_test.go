package shopping

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func TestDefaultCoverageByCategory(t *testing.T) {
	weekly := []string{
		ingredients.CategoryProduce, ingredients.CategoryMeatSeafood,
		ingredients.CategoryDairyEggs, ingredients.CategoryBakery, ingredients.CategoryDeli,
	}
	for _, category := range weekly {
		if got := DefaultCoverage(category); got != providers.CoveragePerWeek {
			t.Errorf("DefaultCoverage(%q) = %q, want %q: fresh categories are bought for the week", category, got, providers.CoveragePerWeek)
		}
	}
	// Pantry staples carry over, so they are measured, not assumed.
	keeps := []string{
		ingredients.CategoryPantry, ingredients.CategorySpices, ingredients.CategoryCondiments,
		ingredients.CategoryFrozen, ingredients.CategoryBeverages, ingredients.CategoryOther, "",
	}
	for _, category := range keeps {
		if got := DefaultCoverage(category); got != providers.CoveragePerAmount {
			t.Errorf("DefaultCoverage(%q) = %q, want %q: it keeps, so count it exactly", category, got, providers.CoveragePerAmount)
		}
	}
}

func TestCoverageForPrefersTheMemberOverride(t *testing.T) {
	// Produce would default to per_week; the member's choice wins.
	if got := coverageFor(providers.CoveragePerAmount, ingredients.CategoryProduce); got != providers.CoveragePerAmount {
		t.Errorf("coverageFor(override) = %q, want the member's %q", got, providers.CoveragePerAmount)
	}
	// Pantry would default to per_amount; the member's choice wins there too.
	if got := coverageFor(providers.CoveragePerWeek, ingredients.CategoryPantry); got != providers.CoveragePerWeek {
		t.Errorf("coverageFor(override) = %q, want the member's %q", got, providers.CoveragePerWeek)
	}
	// No override falls back to the category.
	if got := coverageFor(providers.CoverageAuto, ingredients.CategoryProduce); got != providers.CoveragePerWeek {
		t.Errorf("coverageFor(auto, produce) = %q, want %q", got, providers.CoveragePerWeek)
	}
}

func TestNormalizeCoverage(t *testing.T) {
	for _, good := range []providers.Coverage{providers.CoverageAuto, providers.CoveragePerWeek, providers.CoveragePerAmount} {
		if got, err := normalizeCoverage(good); err != nil || got != good {
			t.Errorf("normalizeCoverage(%q) = %q, %v; want it accepted", good, got, err)
		}
	}
	for _, bad := range []providers.Coverage{"weekly", "per week", "PER_WEEK", "forever"} {
		if _, err := normalizeCoverage(bad); err == nil {
			t.Errorf("normalizeCoverage(%q) = nil error, want a validation error", bad)
		}
	}
}

// Short of pork, a line reads "1 × 16 oz. You have 12 oz of the 20 oz
// needed." rather than "covers 8 oz": nobody shops for 8 oz (decision 623).
func TestShortLineSaysWhatsAtHome(t *testing.T) {
	src := LineSource{OnHand: &Amount{Quantity: "12", Unit: "oz"}, Needed: &Amount{Quantity: "20", Unit: "oz"}}
	if got := haveText(src); got != "You have 12\u00a0oz of the 20\u00a0oz needed." {
		t.Errorf("haveText = %q", got)
	}
	if got := haveText(LineSource{}); got != "" {
		t.Errorf("covered haveText = %q", got)
	}
	oz, _ := ingredients.LookupUnit("oz")
	size := &ingredients.Amount{Quantity: ingredients.NewQuantity(16, 1), Unit: oz}
	for _, c := range []struct {
		packages int
		size     *ingredients.Amount
		want     string
	}{{1, size, "1 × 16 oz"}, {1, nil, "1 package"}, {2, nil, "2 packages"}} {
		if got := packagesText(c.packages, c.size); got != c.want {
			t.Errorf("packagesText(%d) = %q, want %q", c.packages, got, c.want)
		}
	}
}
