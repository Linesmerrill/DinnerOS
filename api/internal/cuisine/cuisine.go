// Package cuisine is the one list of cuisines DinnerOS knows: canonical
// values, their labels and regions, the standard list a household picks
// from, and a guess from a recipe's title and tags. Recipes, Autopilot and
// Shared Recipes all read cuisines through it, so they agree on one value
// (docs/autopilot.md#cuisines, decisions 720 to 724).
package cuisine

import (
	"slices"
	"strings"
)

// Catalog sources label cuisines inconsistently: the same import can say
// "North America" and "North American", or "East Asia" and "East Asian".
// Canonical reads them as one value.
//
// The rules, in order:
//
//  1. Normalize: trimmed, lowercase, single-spaced.
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
//
// Regions are for matching only. What a person sees is always one of the
// standard cuisines (Shown): "North America" and "American" read as
// American, and a pure region like "Southern Europe" or "Asian" is not shown
// at all, because guessing which country it meant would often be wrong
// (decision 732).

// Entry is one known cuisine.
type Entry struct {
	Value, Label string
	// Parent is the broader region, or "".
	Parent string
	// Aliases are other normalized spellings of the value.
	Aliases []string
	// Standard marks the cuisines every household is offered: countries and
	// styles, plus Mediterranean and Middle Eastern (decision 722).
	Standard bool
}

var table = []Entry{
	// Regions.
	{"asian", "Asian", "", []string{"pan asian", "pan-asian"}, false},
	{"east asian", "East Asian", "asian", nil, false},
	{"southeast asian", "Southeast Asian", "asian", []string{"south east asian", "south-east asian"}, false},
	{"south asian", "South Asian", "asian", nil, false},
	{"european", "European", "", nil, false},
	{"southern european", "Southern European", "european", nil, false},
	{"western european", "Western European", "european", nil, false},
	{"eastern european", "Eastern European", "european", nil, false},
	{"northern european", "Northern European", "european", []string{"nordic", "scandinavian"}, false},
	{"latin american", "Latin American", "", []string{"latin", "latino"}, false},
	{"caribbean", "Caribbean", "latin american", nil, false},
	{"central american", "Central American", "latin american", nil, false},
	{"south american", "South American", "latin american", nil, false},
	{"middle eastern", "Middle Eastern", "", []string{"middle east", "mideast"}, true},
	{"african", "African", "", nil, false},
	{"north african", "North African", "african", nil, false},
	{"west african", "West African", "african", nil, false},
	{"east african", "East African", "african", nil, false},
	{"pacific islander", "Pacific Islander", "", []string{"pacific islands", "pacific island", "pacific"}, false},
	// Cuisines that span regions or aren't regional.
	{"mediterranean", "Mediterranean", "", nil, true},
	{"fusion", "Fusion", "", nil, false},
	// Countries and styles.
	{"chinese", "Chinese", "east asian", nil, true},
	{"japanese", "Japanese", "east asian", nil, true},
	{"korean", "Korean", "east asian", nil, true},
	{"thai", "Thai", "southeast asian", nil, true},
	{"vietnamese", "Vietnamese", "southeast asian", nil, true},
	{"filipino", "Filipino", "southeast asian", nil, true},
	{"indonesian", "Indonesian", "southeast asian", nil, true},
	{"indian", "Indian", "south asian", nil, true},
	{"italian", "Italian", "southern european", nil, true},
	{"greek", "Greek", "southern european", nil, true},
	{"spanish", "Spanish", "southern european", nil, true},
	{"portuguese", "Portuguese", "southern european", nil, true},
	{"french", "French", "western european", nil, true},
	{"german", "German", "western european", nil, true},
	{"british", "British", "western european", []string{"english"}, true},
	{"irish", "Irish", "western european", nil, true},
	{"hungarian", "Hungarian", "eastern european", nil, true},
	{"polish", "Polish", "eastern european", nil, true},
	{"russian", "Russian", "eastern european", nil, true},
	{"american", "American", "", []string{"north american", "usa", "united states"}, true},
	{"southern", "Southern", "american", []string{"southern us", "soul food"}, true},
	{"southwestern", "Southwestern", "american", []string{"southwest"}, true},
	{"tex-mex", "Tex-Mex", "american", []string{"tex mex", "texmex"}, true},
	{"cajun", "Cajun", "american", []string{"creole"}, true},
	{"hawaiian", "Hawaiian", "pacific islander", nil, true},
	{"mexican", "Mexican", "latin american", nil, true},
	{"cuban", "Cuban", "caribbean", nil, true},
	{"jamaican", "Jamaican", "caribbean", nil, true},
	{"brazilian", "Brazilian", "south american", nil, true},
	{"peruvian", "Peruvian", "south american", nil, true},
	{"lebanese", "Lebanese", "middle eastern", nil, true},
	{"turkish", "Turkish", "middle eastern", nil, true},
	{"persian", "Persian", "middle eastern", []string{"iranian"}, true},
	{"moroccan", "Moroccan", "north african", nil, true},
	{"ethiopian", "Ethiopian", "east african", nil, true},
}

// regionAdjectives turn a trailing region noun into its adjective.
var regionAdjectives = map[string]string{"america": "american", "asia": "asian", "europe": "european", "africa": "african"}

// byKey maps every canonical value and alias to its entry.
var byKey = func() map[string]*Entry {
	m := make(map[string]*Entry, 2*len(table))
	for i := range table {
		e := &table[i]
		m[e.Value] = e
		for _, a := range e.Aliases {
			m[a] = e
		}
	}
	return m
}()

// Entries returns every known cuisine, regions first. Callers must not
// change it.
func Entries() []Entry { return table }

// Normalize is trimmed, lowercase, single-spaced.
func Normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// Canonical returns the canonical form of a cuisine label.
func Canonical(s string) string {
	v := Normalize(s)
	if e, ok := byKey[v]; ok {
		return e.Value
	}
	if words := strings.Fields(v); len(words) > 0 {
		if adj, ok := regionAdjectives[words[len(words)-1]]; ok {
			words[len(words)-1] = adj
			v = strings.Join(words, " ")
			if e, ok := byKey[v]; ok {
				return e.Value
			}
		}
	}
	return v
}

// Label returns a known canonical cuisine's display label, or "".
func Label(value string) string {
	if e, ok := byKey[value]; ok && e.Value == value {
		return e.Label
	}
	return ""
}

// Known reports whether value is a known canonical cuisine.
func Known(value string) bool { return Label(value) != "" }

// DisplayLabel is the known label, or the value with each word capitalized.
func DisplayLabel(value string) string {
	if l := Label(value); l != "" {
		return l
	}
	words := strings.Fields(value)
	for i, w := range words {
		r := []rune(w)
		words[i] = strings.ToUpper(string(r[:1])) + string(r[1:])
	}
	return strings.Join(words, " ")
}

// Ancestors returns a canonical cuisine's broader regions, nearest first.
func Ancestors(value string) []string {
	var out []string
	for e := byKey[value]; e != nil && e.Value == value && e.Parent != "" && len(out) < len(table); e = byKey[value] {
		value = e.Parent
		out = append(out, value)
	}
	return out
}

// Standard returns the standard cuisines' values in table order.
func Standard() []string {
	var out []string
	for _, e := range table {
		if e.Standard {
			out = append(out, e.Value)
		}
	}
	return out
}

// IsStandard reports whether value is a standard cuisine's canonical value.
func IsStandard(value string) bool {
	e, ok := byKey[value]
	return ok && e.Value == value && e.Standard
}

// Shown is the cuisines a person sees for values: each canonical, only the
// standard ones, without repeats, in order, at most MaxPerRecipe. Stored
// values are read through it, so a recipe saved with "North America" shows
// American and one saved with only "Southern Europe" shows none, without
// rewriting what was stored (decision 732).
func Shown(values []string) []string {
	var out []string
	for _, v := range values {
		c := Canonical(v)
		if !IsStandard(c) || slices.Contains(out, c) {
			continue
		}
		if out = append(out, c); len(out) == MaxPerRecipe {
			break
		}
	}
	return out
}

// MaxPerRecipe is how many cuisines a recipe keeps. MaxLength is the longest
// cuisine value kept.
const (
	MaxPerRecipe = 5
	MaxLength    = 60
)

// Clean canonicalizes and deduplicates cuisines, keeping order, dropping
// empty and overlong ones, and keeping at most MaxPerRecipe.
func Clean(values []string) []string {
	var out []string
	for _, v := range values {
		c := Canonical(v)
		if c == "" || len(c) > MaxLength || slices.Contains(out, c) {
			continue
		}
		if out = append(out, c); len(out) == MaxPerRecipe {
			break
		}
	}
	return out
}
