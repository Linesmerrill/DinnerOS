package shopping

import (
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file decides how one bought package maps to a week's need.
//
// The question it answers: a recipe asks for a clove of garlic, the
// household buys a bulb, and a bulb holds a dozen cloves. Buying a bulb per
// clove is wrong, and so is carrying the bulb forward forever. The rule is
// that fresh categories are bought for the week: one package is assumed to
// cover that week's need, and the next week's list — rebuilt from that
// week's recipes — buys another.
//
// The rule is per grocery category, which every line already carries, and
// a member can override it per saved product.

// perishableCategories are bought fresh for the week and assumed not to
// carry over to the next one. The pantry categories are deliberately absent:
// rice, oil and spices do carry over, so their packages are counted by exact
// measure and not by the week.
var perishableCategories = map[string]bool{
	ingredients.CategoryProduce:     true,
	ingredients.CategoryMeatSeafood: true,
	ingredients.CategoryDairyEggs:   true,
	ingredients.CategoryBakery:      true,
	ingredients.CategoryDeli:        true,
}

// DefaultCoverage is the coverage rule for a grocery category, used when the
// household hasn't overridden it for the product.
func DefaultCoverage(category string) providers.Coverage {
	if perishableCategories[category] {
		return providers.CoveragePerWeek
	}
	return providers.CoveragePerAmount
}

// coverageFor is the rule to apply to one line: the member's saved override
// when there is one, otherwise the category's default.
func coverageFor(saved providers.Coverage, category string) providers.Coverage {
	if saved != providers.CoverageAuto {
		return saved
	}
	return DefaultCoverage(category)
}

// normalizeCoverage validates a coverage a member sent. The empty value is
// allowed and means "follow the category".
func normalizeCoverage(c providers.Coverage) (providers.Coverage, error) {
	switch c {
	case providers.CoverageAuto, providers.CoveragePerWeek, providers.CoveragePerAmount:
		return c, nil
	}
	return "", invalid("coverage must be %q or %q, or omitted to follow the ingredient's category",
		providers.CoveragePerWeek, providers.CoveragePerAmount)
}
