package recipes

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"slices"
	"strconv"
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
// A recipe with no source identifier (a typed recipe, a page a member pasted)
// falls back to a fingerprint of its whole content (decision 697):
// "fp2:<sha256 prefix>" over the normalized name, the ingredient lines (name,
// servings, amount, and unit, in any order), and the steps (in order), with
// amounts, units, and steps case-folded and their whitespace collapsed. Two
// recipes called "Brownies" with the same ingredient names but different
// steps or amounts are two recipes, so sharing one never overwrites the other
// in the catalog, and a household that has one has not got the other.
//
// A household that edits and re-shares its own recipe still updates its own
// catalog entry: the catalog finds it by CatalogOrigin, not by this key.
//
// The key is empty only for a recipe with no name at all, which never
// validates.
func CatalogKey(r Recipe) string {
	if key := importerKey(r); key != "" {
		return key
	}
	name := ingredients.NormalizeName(r.Name)
	if name == "" {
		return ""
	}
	lines := make([]string, 0, len(r.Ingredients))
	for _, line := range r.Ingredients {
		k := ingredients.NormalizeName(line.Name)
		if k == "" {
			continue
		}
		amounts := make([]string, 0, len(line.Amounts))
		for _, a := range line.Amounts {
			amounts = append(amounts, strconv.Itoa(a.Servings)+" "+normalizeQuantityText(a.Quantity)+" "+foldSpace(a.Unit))
		}
		slices.Sort(amounts)
		lines = append(lines, k+"|"+strings.Join(amounts, ";"))
	}
	slices.Sort(lines)
	ordered := slices.Clone(r.Steps)
	slices.SortStableFunc(ordered, func(a, b Step) int { return cmp.Compare(a.Index, b.Index) })
	steps := make([]string, 0, len(ordered))
	for _, s := range ordered {
		if t := foldSpace(s.Text); t != "" {
			steps = append(steps, t)
		}
	}
	sum := sha256.Sum256([]byte(name + "\x00" + strings.Join(lines, "\x01") + "\x00" + strings.Join(steps, "\x01")))
	return "fp2:" + hex.EncodeToString(sum[:16])
}

// importerKey is "<source>:<sourceRecipeId>" for a source with its own
// recipe IDs, and "" for a typed or pasted recipe, whose IDs are random.
func importerKey(r Recipe) string {
	source := strings.ToLower(strings.TrimSpace(r.Source))
	id := strings.TrimSpace(r.SourceRecipeID)
	if source != "" && id != "" && source != SourceManual && source != SourceUser {
		return source + ":" + id
	}
	return ""
}

// LegacyCatalogKey is the key CatalogKey gave a typed or pasted recipe before
// decision 697: "fp:" over the normalized name and ingredient names only. A
// household recipe not rewritten since still stores it, and a catalog entry
// published before still carries it, so lookups try it too until both are
// rewritten. It is "" for an importer's recipe, whose key never changed.
func LegacyCatalogKey(r Recipe) string {
	if importerKey(r) != "" {
		return ""
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

// CatalogOrigin identifies which household recipe a catalog entry was shared
// from, for a typed or pasted recipe: an opaque SHA-256 of the household and
// the recipe's own source identity, which a recipe keeps through edits. The
// catalog stores the hash, never the household, and uses it to update the
// entry when the household re-shares an edited recipe. It is "" for an
// importer's recipe, whose catalog entry is shared by every household that
// has it, and for a recipe with nothing stable to name it by.
func CatalogOrigin(r Recipe) string {
	if importerKey(r) != "" || strings.TrimSpace(r.HouseholdID) == "" {
		return ""
	}
	var own string
	switch {
	case strings.TrimSpace(r.SourceRecipeID) != "":
		own = strings.ToLower(strings.TrimSpace(r.Source)) + "\n" + strings.TrimSpace(r.SourceRecipeID)
	case strings.TrimSpace(r.ID) != "":
		own = "id\n" + strings.TrimSpace(r.ID)
	default:
		return ""
	}
	sum := sha256.Sum256([]byte("origin\n" + strings.TrimSpace(r.HouseholdID) + "\n" + own))
	return hex.EncodeToString(sum[:])
}

// foldSpace case-folds s, collapses its whitespace, and trims it.
func foldSpace(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// normalizeQuantityText writes "0.5", "1/2", and "2/4" the same way.
func normalizeQuantityText(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	if r, ok := new(big.Rat).SetString(q); ok {
		return r.RatString()
	}
	return foldSpace(q)
}

// Publishable reports whether this recipe may be copied into the global
// catalog: it comes from a known public source, or the household explicitly
// shared it. Add-ons are publishable like any other recipe.
func Publishable(r Recipe) bool {
	return CatalogKey(r) != "" && !CatalogWithheld(r.Source) && (PublicSource(r.Source) || r.SharedToCatalog)
}
