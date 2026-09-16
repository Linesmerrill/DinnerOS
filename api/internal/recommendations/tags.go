package recommendations

import "slices"

// Catalog tags mix food characteristics ("One Pot", "Kid Friendly", "Spicy")
// with the source's own bookkeeping ("SEO", "dinners"). Only the first kind is
// worth offering, so hiddenTag drops the rest wherever tags are presented as
// choices: the menu's Food Types filter, the Autopilot vocabulary, and the
// weekday-rule and taste-profile tag pickers that read it
// (docs/autopilot.md#food-types).
//
// Hiding changes only what is *offered*. Canonicalization, validation, stored
// profiles, and tag matching are untouched, so a household that already likes
// or excludes a hidden tag keeps working exactly as before, and a hidden tag
// passed to the menu's ?tag= filter still filters.

// internalTags are source bookkeeping, never food characteristics. Values are
// canonical (canonicalTag). Chosen from an imported catalog of 399 main meals:
//
//   - seo                   search-engine bookkeeping, and the largest tag (69%)
//   - dinner, dinners       the meal slot, not the food: every main meal is one
//   - lunch, lunches        the same, for the source's other catalog
//   - breakfast, breakfasts the same
//   - grocery, sides        where the source files an item, not what it is
//   - lto                   "limited time offer" merchandising
//   - deals-of-theweek      merchandising
//   - static-position       merchandising placement
//   - ineligible-reco       the source's own recommendation bookkeeping
//   - free-addon            pricing, not food
//   - quick prep internal   labeled internal by the source
var internalTags = []string{
	"breakfast", "breakfasts", "deals-of-theweek", "dinner", "dinners", "free-addon", "grocery",
	"ineligible-reco", "lto", "lunch", "lunches", "quick prep internal", "seo", "sides", "static-position",
}

// NearUniversalTagShare is the share of the catalog above which a tag stops
// being useful as a filter: it matches nearly everything, so picking it
// narrows nothing.
const NearUniversalTagShare = 0.9

// VisibleTag reports whether a tag is a food characteristic worth offering as
// a filter or a preference, rather than source bookkeeping. count is how many
// of the catalog's total main meals carry it.
func VisibleTag(value string, count, total int) bool { return !hiddenTag(value, count, total) }

// hiddenTag reports whether a tag should never be offered as a choice. count
// is how many of the catalog's total main meals carry it.
func hiddenTag(value string, count, total int) bool {
	if slices.Contains(internalTags, value) {
		return true
	}
	return total > 0 && float64(count) > NearUniversalTagShare*float64(total)
}
