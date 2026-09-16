package shopping

import (
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file derives the words a member should search for to find the right
// product for a grocery line. It is pure text: nothing here calls a product
// search API, because none is available (docs/shopping-providers.md).
//
// Walmart's search ranks a bag of words against product titles, so the only
// lever DinnerOS has without an API is which words it suggests. "garlic"
// surfaces garlic powder, garlic salt and garlic snacks; "fresh whole
// garlic" surfaces the bulb.
//
// The bias comes from the grocery category, which every line already
// carries, and not from a list of ingredients: what form a shopper wants
// follows from the aisle. There is deliberately no per-ingredient table. The
// one cross-cutting rule is that an ingredient whose own name already names
// a form ("garlic powder", "minced garlic", "frozen peas") keeps that name
// untouched, because the recipe asked for that form on purpose.

// SearchTerms is the search DinnerOS suggests for one grocery line, with the
// reasoning shown to the member rather than hidden.
type SearchTerms struct {
	// Query is what to search for: the qualifiers, then the ingredient.
	Query string
	// Qualifiers are the words added ahead of the ingredient name, in order.
	// Empty when the category has no useful bias, or when the name already
	// names a form.
	Qualifiers []string
	// Avoid are forms of the ingredient that are the wrong aisle for this
	// line. They are guidance for the person searching: without a product
	// search API, DinnerOS cannot filter them out itself.
	Avoid []string
	// Why is one sentence explaining the bias, or "" when none was applied.
	Why string
}

// formProfile is the form bias for one grocery category.
type formProfile struct {
	qualifiers []string
	avoid      []string
	why        string
}

// formProfiles biases a search per grocery category. Categories absent from
// the map get the ingredient name alone: a pantry or condiment line has no
// single right form to steer towards, and inventing one would make the
// search worse, not better.
var formProfiles = map[string]formProfile{
	ingredients.CategoryProduce: {
		qualifiers: []string{"fresh", "whole"},
		avoid: []string{
			"powder", "powdered", "granulated", "dried", "dehydrated", "minced", "chopped", "sliced",
			"jarred", "canned", "frozen", "pickled", "roasted", "seasoning", "snack", "chips", "juice",
		},
		why: "Produce: the fresh whole item, not a dried, powdered or prepared form.",
	},
	ingredients.CategoryMeatSeafood: {
		qualifiers: []string{"fresh"},
		avoid:      []string{"canned", "jerky", "dried", "freeze dried", "snack"},
		why:        "Meat and seafood: the fresh cut, not a canned or dried form.",
	},
	ingredients.CategoryDairyEggs: {
		avoid: []string{"powder", "powdered", "dried"},
		why:   "Dairy and eggs: the refrigerated item, not a powdered one.",
	},
	ingredients.CategorySpices: {
		avoid: []string{"fresh"},
		why:   "Spices: the dried jar from the spice aisle, not the fresh herb.",
	},
	ingredients.CategoryFrozen: {
		qualifiers: []string{"frozen"},
		avoid:      []string{"fresh", "canned"},
		why:        "Frozen: the freezer-aisle item.",
	},
}

// SearchTermsFor returns the search to suggest for a grocery line. An empty
// name returns an empty SearchTerms: there is nothing to search for.
func SearchTermsFor(name, category string) SearchTerms {
	name = strings.TrimSpace(name)
	if name == "" {
		return SearchTerms{}
	}
	profile, ok := formProfiles[category]
	if !ok {
		return SearchTerms{Query: name}
	}
	// A name that already names a form is left alone: "garlic powder" is a
	// spice the recipe asked for, not a failed search for fresh garlic.
	if namesAForm(name, profile) {
		return SearchTerms{Query: name, Avoid: cloneWords(profile.avoid)}
	}
	terms := SearchTerms{
		Query:      strings.TrimSpace(strings.Join(append(cloneWords(profile.qualifiers), name), " ")),
		Qualifiers: cloneWords(profile.qualifiers),
		Avoid:      cloneWords(profile.avoid),
		Why:        profile.why,
	}
	return terms
}

// namesAForm reports whether name already contains one of the profile's
// qualifiers or avoided forms, matched on whole words so "fresh" doesn't
// match "freshwater" and "chips" doesn't match "chipotle".
func namesAForm(name string, profile formProfile) bool {
	words := wordSet(name)
	for _, list := range [][]string{profile.qualifiers, profile.avoid} {
		for _, phrase := range list {
			if containsPhrase(words, phrase) {
				return true
			}
		}
	}
	return false
}

// wordSet splits a name into lower-case words, dropping punctuation.
func wordSet(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9')
	})
}

// containsPhrase reports whether words contains every word of phrase,
// consecutively ("freeze dried" matches "freeze dried beef").
func containsPhrase(words []string, phrase string) bool {
	parts := strings.Fields(phrase)
	if len(parts) == 0 || len(parts) > len(words) {
		return false
	}
	for i := 0; i+len(parts) <= len(words); i++ {
		if matchesAt(words, parts, i) {
			return true
		}
	}
	return false
}

func matchesAt(words, parts []string, i int) bool {
	for j, part := range parts {
		if words[i+j] != part {
			return false
		}
	}
	return true
}

// cloneWords copies a word list so callers can't write into the table.
func cloneWords(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}
