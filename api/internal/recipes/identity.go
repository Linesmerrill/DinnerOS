package recipes

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// publicSources are the sources whose recipes may enter the global recipe
// catalog without a household opting in. There are none (decision 672): a
// meal kit's recipes are its own writing and photos, and copying them to
// every household is what invites a takedown.
//
// Everything else is private to the household that owns it until the
// household sets Recipe.SharedToCatalog.
var publicSources = []string{}

// withheldSources are never shown in, or copied to, the global catalog, even
// when a household shares one. There are none: a household may share recipes
// it imported itself, meal-kit imports included, exactly as it shares any
// other recipe (decision 685, replacing decision 672's withhold). Nothing is
// published without the household opting in; publicSources stays empty.
var withheldSources = []string{}

// WithheldSources returns the withheld sources, for store queries.
func WithheldSources() []string { return slices.Clone(withheldSources) }

// CatalogWithheld reports whether recipes from this source are kept out of
// the global catalog entirely.
func CatalogWithheld(source string) bool {
	return slices.Contains(withheldSources, strings.ToLower(strings.TrimSpace(source)))
}

// PublicSource reports whether recipes from this source may be published to
// the global catalog without the owning household opting in.
func PublicSource(source string) bool {
	return slices.Contains(publicSources, strings.ToLower(strings.TrimSpace(source)))
}

// PublicSources returns the known public sources, for documentation and
// error messages.
func PublicSources() []string { return slices.Clone(publicSources) }

// CatalogKey returns the stable identity of "the same recipe", independent of
// which household holds it. Two households that both received the same
// HelloFresh meal produce the same key, so the catalog collapses them into one
// entry and a household's library can be marked against the catalog with a
// single indexed lookup.
//
// The primary identity is the source's own: "<source>:<sourceRecipeId>". It is
// exact, survives a renamed or re-photographed recipe, and is what the source
// itself considers one recipe.
//
// A recipe with no source identifier — a typed recipe, a page a member pasted
// — falls back to a fingerprint of its name and its ingredient set:
// "fp:<sha256 prefix>" over the normalized name and the sorted, de-duplicated
// normalized ingredient names. Name alone is too weak (every household has a
// "Chicken Tacos") and the full text is too strong (a reworded step would
// split one recipe in two); the pair is what a person means by "that's the
// same recipe". Amounts, servings, and steps are deliberately left out, so a
// household that halves the quantities still matches.
//
// The key is empty only for a recipe with no name at all, which never
// validates.
func CatalogKey(r Recipe) string {
	source := strings.ToLower(strings.TrimSpace(r.Source))
	id := strings.TrimSpace(r.SourceRecipeID)
	if source != "" && id != "" && source != SourceManual && source != SourceUser {
		return source + ":" + id
	}
	name := ingredients.NormalizeName(r.Name)
	if name == "" {
		return ""
	}
	keys := make([]string, 0, len(r.Ingredients))
	for _, line := range r.Ingredients {
		if k := ingredients.NormalizeName(line.Name); k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	sum := sha256.Sum256([]byte(name + "\n" + strings.Join(keys, ",")))
	return "fp:" + hex.EncodeToString(sum[:16])
}

// Publishable reports whether this recipe may be copied into the global
// catalog: it comes from a known public source, or the household explicitly
// shared it. Add-ons are publishable like any other recipe.
func Publishable(r Recipe) bool {
	return CatalogKey(r) != "" && !CatalogWithheld(r.Source) && (PublicSource(r.Source) || r.SharedToCatalog)
}
