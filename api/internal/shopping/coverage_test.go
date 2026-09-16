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
