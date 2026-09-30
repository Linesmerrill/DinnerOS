package recipes

import (
	"regexp"
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
// onion), and a count of "(14.5 oz) can diced tomatoes" is cans.
func homeCount(m Measure, name string) *Measure {
	if m.Unit == "" {
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
	"cubed": true, "peeled": true, "halved": true, "quartered": true, "cooked": true, "toasted": true,
	"thawed": true, "torn": true, "mashed": true, "cooled": true, "whisked": true, "pitted": true,
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
	return end - start
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

// endsWithWord reports whether s ends in phrase as whole words: "more" ends
// "add more", not "anymore".
func endsWithWord(s, phrase string) bool {
	if !strings.HasSuffix(s, phrase) {
		return false
	}
	rest := []rune(s[:len(s)-len(phrase)])
	return len(rest) == 0 || !unicode.IsLetter(rest[len(rest)-1])
}
