package recommendations

import (
	"context"
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// This file exposes Autopilot's view of recipes to modules that present
// recipes the same way, such as the menu, so chips, filters, and badges use
// one vocabulary instead of a copy.

// AttributesOf derives a recipe's attributes as Autopilot sees them. It only
// reads ingredient names, so a catalog without amounts is enough. override
// may be nil.
func AttributesOf(r recipes.Recipe, override *RecipeOverride, bands autopilot.TimeBands) RecipeAttributes {
	return attributes(r, override, bands)
}

// Suits reports whether the recipe suits a cooking method, honoring overrides.
func (a RecipeAttributes) Suits(method string) bool {
	return slices.ContainsFunc(a.Methods, func(m MethodAttribute) bool { return m.Method == method && m.Suits })
}

// CanonicalCuisine returns the canonical form of a cuisine label
// ("North America" → "north american").
func CanonicalCuisine(s string) string { return canonicalCuisine(s) }

// CuisineLabel returns a known canonical cuisine's display label, or "".
func CuisineLabel(value string) string { return cuisineLabel(value) }

// CanonicalTag returns the canonical form of a tag.
func CanonicalTag(s string) string { return canonicalTag(s) }

// CatalogVocabulary counts cuisines (rolled up to regions), tags, and
// proteins across the catalog's main meals, as GET .../autopilot/vocabulary
// does.
func CatalogVocabulary(catalog []recipes.Recipe) Vocabulary { return buildVocabulary(catalog) }

// TimeBands returns the profile's cook-time bands.
func (p Profile) TimeBands() autopilot.TimeBands { return p.bands() }

// TimeBands returns the household's cook-time bands: the profile's, or the
// defaults when it has none.
func (s *Service) TimeBands(ctx context.Context, householdID string) (autopilot.TimeBands, error) {
	p, err := s.Profile(ctx, householdID)
	if err != nil {
		return autopilot.TimeBands{}, err
	}
	return p.bands(), nil
}
