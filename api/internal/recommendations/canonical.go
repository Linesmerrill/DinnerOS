package recommendations

import (
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/cuisine"
)

// Catalog sources label cuisines inconsistently: the same import can say
// "North America" and "North American", or "East Asia" and "East Asian".
// Autopilot reads every cuisine through canonicalCuisine, so vocabulary
// counts, recipe attributes, taste matching, exclusions, and weekday rules
// agree on one value (docs/autopilot.md#cuisines). The table and its rules
// live in package cuisine, so recipes can use them too (decision 720).

// canonicalCuisine returns the canonical form of a cuisine label.
func canonicalCuisine(s string) string { return cuisine.Canonical(s) }

// cuisineLabel returns a known cuisine's display label, or "".
func cuisineLabel(value string) string { return cuisine.Label(value) }

// cuisineAncestors returns a canonical cuisine's broader regions, nearest
// first.
func cuisineAncestors(value string) []string { return cuisine.Ancestors(value) }

// withAncestors returns a canonical cuisine followed by its ancestors.
func withAncestors(value string) []string {
	return append([]string{value}, cuisineAncestors(value)...)
}

// canonicalCuisines canonicalizes and deduplicates cuisines, keeping order.
func canonicalCuisines(values []string) []string {
	return canonicalAll(values, canonicalCuisine)
}

// cuisineRegions returns the ancestors of cuisines that aren't already among
// them, in order.
func cuisineRegions(cuisines []string) []string {
	var out []string
	for _, c := range cuisines {
		for _, a := range cuisineAncestors(c) {
			if !slices.Contains(cuisines, a) && !slices.Contains(out, a) {
				out = append(out, a)
			}
		}
	}
	return out
}

// matchRegions are what a recipe matches beyond the standard cuisines it
// shows: their regions, then every other value the recipe stored (a region
// like "Southern Europe", or an unknown one) with its own regions. A recipe
// stored as only "Asian" shows no cuisine (decision 732) but an exclusion of
// Asian still covers it.
func matchRegions(shown, stored []string) []string {
	out := cuisineRegions(shown)
	for _, c := range canonicalCuisines(stored) {
		for _, v := range withAncestors(c) {
			if !slices.Contains(shown, v) && !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// Tags are less messy than cuisines: normalizeValue already merges case and
// spacing, and what remains are distinct marketing labels rather than
// synonyms. tagAliases only joins spellings that differ by a missing space.
var tagAliases = map[string]string{
	"familyfriendly": "family friendly",
	"kidfriendly":    "kid friendly",
	"onepot":         "one pot",
	"onepan":         "one pan",
}

// canonicalTag returns the canonical form of a tag.
func canonicalTag(s string) string {
	v := normalizeValue(s)
	if alias, ok := tagAliases[v]; ok {
		return alias
	}
	return v
}

// canonicalTags canonicalizes and deduplicates tags, keeping order.
func canonicalTags(values []string) []string {
	return canonicalAll(values, canonicalTag)
}

func canonicalAll(values []string, canonical func(string) string) []string {
	var out []string
	for _, v := range values {
		if c := canonical(v); c != "" && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// canonicalProfile reads a stored profile with today's canonical cuisines and
// tags, so values saved before canonicalization keep working. Merging
// spellings can make a value both liked and disliked or excluded; the more
// restrictive choice wins.
func canonicalProfile(p Profile) Profile {
	sorted := func(values []string, canonical func(string) string, drop ...[]string) []string {
		out := slices.DeleteFunc(canonicalAll(values, canonical), func(v string) bool {
			return slices.ContainsFunc(drop, func(other []string) bool { return slices.Contains(other, v) })
		})
		if len(out) == 0 {
			return nil
		}
		slices.Sort(out)
		return out
	}
	t, r := &p.Taste, &p.Restrictions
	t.Dislikes.Cuisines, t.Dislikes.Tags = sorted(t.Dislikes.Cuisines, canonicalCuisine), sorted(t.Dislikes.Tags, canonicalTag)
	r.ExcludedCuisines, r.ExcludedTags = sorted(r.ExcludedCuisines, canonicalCuisine), sorted(r.ExcludedTags, canonicalTag)
	t.Likes.Cuisines = sorted(t.Likes.Cuisines, canonicalCuisine, t.Dislikes.Cuisines, r.ExcludedCuisines)
	t.Likes.Tags = sorted(t.Likes.Tags, canonicalTag, t.Dislikes.Tags)
	for i := range p.WeekdayRules {
		rule := &p.WeekdayRules[i]
		rule.Cuisines, rule.Tags = sorted(rule.Cuisines, canonicalCuisine), sorted(rule.Tags, canonicalTag)
	}
	return p
}
