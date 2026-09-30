package recipes

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// This file reads the phrasings of recipes people add themselves — pasted
// from a blog or typed from a family card — which a meal-kit card never
// uses: one serving size, amounts written in the steps ("add 2 tablespoons
// olive oil"), ranges ("2-3 cloves"), lines headed "For the sauce:", and
// names that carry their can size ("(14.5 oz) can diced tomatoes").

// leadingContainerRe is a can size at the front of a pasted name, the way
// "1 (14.5 oz) can diced tomatoes" imports.
var leadingContainerRe = regexp.MustCompile(`(?i)^\([^()]*\)\s*(?:(cans?|packages?|jars?|bags?|boxes?|containers?|cartons?|bottles?)\s+)?`)

// homeCount is a counted amount as the steps read it. A pasted "1 onion"
// imports with no unit, which reads as a count ("half the onion" is ½
// onion), and a count of "(14.5 oz) can diced tomatoes" is cans. A meal-kit
// card's lines keep their units as imported.
func homeCount(m Measure, name string, home bool) *Measure {
	if m.Unit == "" && home {
		m.Unit = "count"
	}
	if m.Unit == "count" {
		if loc := leadingContainerRe.FindStringSubmatch(name); loc != nil {
			switch w := strings.ToLower(loc[1]); {
			case strings.HasPrefix(w, "can"):
				m.Unit = "can"
			case strings.HasPrefix(w, "package"):
				m.Unit = "package"
			}
		}
	}
	return &m
}

// ingredientHeading reports whether a line heads a group of ingredients
// ("For the sauce:") rather than naming one.
func ingredientHeading(name string) bool {
	name = strings.TrimSpace(name)
	return strings.HasSuffix(name, ":") && !strings.ContainsAny(name, "0123456789")
}

// nameTails are words a pasted line puts after the name: "salt to taste",
// "cheddar, for serving", "parsley (optional)".
var nameTails = []string{
	" to taste", " for serving", " for garnish", " for topping", " for the pan", " optional",
	" or more", " as needed", " if desired", " if you like",
}

// trimNameTail drops those words.
func trimNameTail(name string) string {
	for changed := true; changed; {
		changed = false
		lower := strings.ToLower(name)
		for _, tail := range nameTails {
			if strings.HasSuffix(lower, tail) && len(lower) > len(tail) {
				name = strings.TrimRight(name[:len(name)-len(tail)], " ,")
				changed = true
				break
			}
		}
	}
	return name
}

// prepWords are how a step says an ingredient is readied right before its
// name ("the melted butter", "beaten egg"). The amount goes in front of them.
var prepWords = map[string]bool{
	"diced": true, "minced": true, "chopped": true, "sliced": true, "melted": true, "softened": true,
	"beaten": true, "drained": true, "rinsed": true, "grated": true, "shredded": true, "crushed": true,
	"cubed": true, "peeled": true, "halved": true, "quartered": true, "toasted": true,
	"thawed": true, "torn": true, "mashed": true, "whisked": true, "pitted": true,
	"cored": true, "trimmed": true, "julienned": true, "smashed": true,
	"finely": true, "thinly": true, "roughly": true, "coarsely": true, "freshly": true, "lightly": true,
}

// trailingPrepLen is how many runes at the end of text are those words and
// the spaces after them ("melted ", "peeled and diced "), or 0.
func trailingPrepLen(text []rune) int {
	end := len(text)
	if trailingSpaces(text, end) == 0 {
		return 0
	}
	start := end
	pos := end - trailingSpaces(text, end)
	for pos > 0 {
		word, wordStart := lastWord(text, pos)
		lower := strings.ToLower(word)
		if !prepWords[lower] {
			// "peeled and diced": "and" only between two of them.
			if lower != "and" || start == end {
				break
			}
			before, _ := lastWord(text, wordStart-trailingSpaces(text, wordStart))
			if !prepWords[strings.ToLower(before)] {
				break
			}
		}
		start = wordStart
		gap := trailingSpaces(text, wordStart)
		if gap == 0 {
			break
		}
		pos = wordStart - gap
	}
	// "lightly oil a sheet": an adverb alone describes the verb, not the
	// ingredient, so there's no prep to put the amount in front of.
	run := strings.Fields(strings.ToLower(string(text[start:end])))
	if !slices.ContainsFunc(run, func(w string) bool { return prepWords[w] && !howWords[w] }) {
		return 0
	}
	return end - start
}

// howWords are the prep words that only say how ("finely chopped").
var howWords = map[string]bool{
	"finely": true, "thinly": true, "roughly": true, "coarsely": true, "freshly": true, "lightly": true,
}

// rangeUnits matches any unit word, longest first.
var rangeUnits = func() string {
	words := make([]string, 0, len(unitCodes))
	for w := range unitCodes {
		words = append(words, regexp.QuoteMeta(w))
	}
	sort.Slice(words, func(i, j int) bool { return len(words[i]) > len(words[j]) })
	return strings.Join(words, "|")
}()

// rangeBeforeRe is a range of amounts right before a name: "2-3 cloves ",
// "1 to 2 tablespoons of ", "3–4 ".
var rangeBeforeRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}/⁄.])\d+(?:[./⁄]\d+)?\s*(?:-|–|to)\s*\d+(?:[./⁄]\d+)?(?:\s+(?:` + rangeUnits + `))?(?:\s+of(?:\s+the)?)?\s+$`)

// statedRange reports whether the text before a mention ends in a range of
// amounts.
func statedRange(text []rune) bool {
	return rangeBeforeRe.MatchString(string(text))
}

// listJoinRe is what joins the names of a list: ", ", " and ", ", and ".
var listJoinRe = regexp.MustCompile(`^(?:,\s*(?:and\s+|or\s+)?|\s+(?:and|or)\s+)$`)

// sharedList reports whether a share ("half of the garlic powder") goes on
// to the rest of its sentence: a home recipe's list of bare names joined by
// "and" ("…, paprika, salt and pepper."), with no amount, note, or other
// words that could mean the share stops. A meal-kit card's "half the
// Parmesan, and 1 TBSP butter" is never one: when in doubt, it doesn't.
func sharedList(home bool, after []rune) bool {
	if !home {
		return false
	}
	rest := string(after)
	if end := strings.IndexAny(rest, ".;!\n"); end >= 0 {
		rest = rest[:end]
	}
	if strings.ContainsAny(rest, "()0123456789"+fractionRunes+"⅛⅜⅝⅞") || !strings.HasPrefix(rest, ",") && !strings.HasPrefix(rest, " and ") {
		return false
	}
	items := listSplitRe.Split(strings.TrimSpace(rest), -1)
	joinedByAnd := strings.Contains(rest, " and ")
	for _, item := range items {
		if n := len(strings.Fields(item)); n > 3 {
			return false
		}
	}
	return joinedByAnd && len(items) > 1
}

// listSplitRe splits a list into its names.
var listSplitRe = regexp.MustCompile(`\s*,\s*(?:and\s+)?|\s+and\s+`)

// endsWithWord reports whether s ends in phrase as whole words: "more" ends
// "add more", not "anymore".
func endsWithWord(s, phrase string) bool {
	if !strings.HasSuffix(s, phrase) {
		return false
	}
	rest := []rune(s[:len(s)-len(phrase)])
	return len(rest) == 0 || !unicode.IsLetter(rest[len(rest)-1])
}
