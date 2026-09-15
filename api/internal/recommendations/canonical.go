package recommendations

import (
	"slices"
	"strings"
)

// Catalog sources label cuisines inconsistently: the same import can say
// "North America" and "North American", or "East Asia" and "East Asian".
// Autopilot reads every cuisine through canonicalCuisine, so vocabulary
// counts, recipe attributes, taste matching, exclusions, and weekday rules
// agree on one value (docs/autopilot.md#cuisines).
//
// The rules, in order:
//
//  1. Normalize: trimmed, lowercase, single-spaced (normalizeValue).
//  2. A known value or alias maps to its canonical value ("latin" → "latin
//     american", "middle east" → "middle eastern").
//  3. Otherwise a trailing region noun becomes its adjective ("southern
//     europe" → "southern european"), and step 2 is tried again.
//  4. Anything else is kept as normalized, so unknown cuisines still work.
//
// Canonical values form a small hierarchy: a country cuisine has a region
// parent ("italian" → "southern european" → "european"). A recipe matches its
// own cuisines and every ancestor, so liking or excluding "european" covers
// Italian recipes, while "italian" stays a distinct, more specific choice.
// "asian" is a parent region rather than a synonym of "east asian": catalogs
// use it for East and Southeast Asian dishes alike.

type cuisineEntry struct {
	value, label string
	// parent is the broader region, or "".
	parent string
	// aliases are other normalized spellings of the value.
	aliases []string
}

var cuisineTable = []cuisineEntry{
	// Regions.
	{"asian", "Asian", "", []string{"pan asian", "pan-asian"}},
	{"east asian", "East Asian", "asian", nil},
	{"southeast asian", "Southeast Asian", "asian", []string{"south east asian", "south-east asian"}},
	{"south asian", "South Asian", "asian", nil},
	{"european", "European", "", nil},
	{"southern european", "Southern European", "european", nil},
	{"western european", "Western European", "european", nil},
	{"eastern european", "Eastern European", "european", nil},
	{"northern european", "Northern European", "european", []string{"nordic", "scandinavian"}},
	{"north american", "North American", "", []string{"american", "usa"}},
	{"latin american", "Latin American", "", []string{"latin", "latino"}},
	{"caribbean", "Caribbean", "latin american", nil},
	{"central american", "Central American", "latin american", nil},
	{"south american", "South American", "latin american", nil},
	{"middle eastern", "Middle Eastern", "", []string{"middle east", "mideast"}},
	{"african", "African", "", nil},
	{"north african", "North African", "african", nil},
	{"west african", "West African", "african", nil},
	{"east african", "East African", "african", nil},
	{"pacific islander", "Pacific Islander", "", []string{"pacific islands", "pacific island", "pacific"}},
	// Cuisines that span regions or aren't regional.
	{"mediterranean", "Mediterranean", "", nil},
	{"fusion", "Fusion", "", nil},
	// Countries and styles.
	{"chinese", "Chinese", "east asian", nil},
	{"japanese", "Japanese", "east asian", nil},
	{"korean", "Korean", "east asian", nil},
	{"thai", "Thai", "southeast asian", nil},
	{"vietnamese", "Vietnamese", "southeast asian", nil},
	{"filipino", "Filipino", "southeast asian", nil},
	{"indonesian", "Indonesian", "southeast asian", nil},
	{"indian", "Indian", "south asian", nil},
	{"italian", "Italian", "southern european", nil},
	{"greek", "Greek", "southern european", nil},
	{"spanish", "Spanish", "southern european", nil},
	{"portuguese", "Portuguese", "southern european", nil},
	{"french", "French", "western european", nil},
	{"german", "German", "western european", nil},
	{"british", "British", "western european", []string{"english"}},
	{"irish", "Irish", "western european", nil},
	{"hungarian", "Hungarian", "eastern european", nil},
	{"polish", "Polish", "eastern european", nil},
	{"russian", "Russian", "eastern european", nil},
	{"southern", "Southern", "north american", []string{"southern us", "soul food"}},
	{"southwestern", "Southwestern", "north american", []string{"southwest"}},
	{"tex-mex", "Tex-Mex", "north american", []string{"tex mex", "texmex"}},
	{"cajun", "Cajun", "north american", []string{"creole"}},
	{"hawaiian", "Hawaiian", "pacific islander", nil},
	{"mexican", "Mexican", "latin american", nil},
	{"cuban", "Cuban", "caribbean", nil},
	{"jamaican", "Jamaican", "caribbean", nil},
	{"brazilian", "Brazilian", "south american", nil},
	{"peruvian", "Peruvian", "south american", nil},
	{"lebanese", "Lebanese", "middle eastern", nil},
	{"turkish", "Turkish", "middle eastern", nil},
	{"persian", "Persian", "middle eastern", []string{"iranian"}},
	{"moroccan", "Moroccan", "north african", nil},
	{"ethiopian", "Ethiopian", "east african", nil},
}

// regionAdjectives turn a trailing region noun into its adjective.
var regionAdjectives = map[string]string{"america": "american", "asia": "asian", "europe": "european", "africa": "african"}

// cuisineByKey maps every canonical value and alias to its entry.
var cuisineByKey = func() map[string]*cuisineEntry {
	m := make(map[string]*cuisineEntry, 2*len(cuisineTable))
	for i := range cuisineTable {
		e := &cuisineTable[i]
		m[e.value] = e
		for _, a := range e.aliases {
			m[a] = e
		}
	}
	return m
}()

// canonicalCuisine returns the canonical form of a cuisine label.
func canonicalCuisine(s string) string {
	v := normalizeValue(s)
	if e, ok := cuisineByKey[v]; ok {
		return e.value
	}
	if words := strings.Fields(v); len(words) > 0 {
		if adj, ok := regionAdjectives[words[len(words)-1]]; ok {
			words[len(words)-1] = adj
			v = strings.Join(words, " ")
			if e, ok := cuisineByKey[v]; ok {
				return e.value
			}
		}
	}
	return v
}

// cuisineLabel returns a known cuisine's display label, or "".
func cuisineLabel(value string) string {
	if e, ok := cuisineByKey[value]; ok && e.value == value {
		return e.label
	}
	return ""
}

// cuisineAncestors returns a canonical cuisine's broader regions, nearest
// first.
func cuisineAncestors(value string) []string {
	var out []string
	for e := cuisineByKey[value]; e != nil && e.value == value && e.parent != "" && len(out) < len(cuisineTable); e = cuisineByKey[value] {
		value = e.parent
		out = append(out, value)
	}
	return out
}

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
