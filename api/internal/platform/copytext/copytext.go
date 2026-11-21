// Package copytext holds the small typographic rules every string the app
// shows follows: curly apostrophes ("don’t", "Chef’s Picks"). It formats at
// response time, so stored text (a recipe's source words, a household's
// name) is never rewritten.
package copytext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Apostrophe is the curly apostrophe (U+2019) the app shows.
const Apostrophe = "’"

// Curly turns each straight apostrophe inside a word ("don't", "Chef's",
// "Joe's") into a curly one. A straight quote that isn't between a letter or
// digit and a letter ('quoted', a lone ') is left alone, because it may be a
// quotation mark rather than an apostrophe.
func Curly(s string) string {
	if !strings.Contains(s, "'") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	prev := rune(-1)
	for i, r := range s {
		if r == '\'' {
			next, _ := utf8.DecodeRuneInString(s[i+1:])
			if (unicode.IsLetter(prev) || unicode.IsDigit(prev)) && unicode.IsLetter(next) {
				b.WriteString(Apostrophe)
				prev = r
				continue
			}
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

// Title writes a recipe title the one way the app shows titles: "and"
// between words becomes "&" ("Chicken & Rice Bowls"), and apostrophes are
// curly. The stored title is never rewritten.
func Title(s string) string {
	s = strings.ReplaceAll(s, " and ", " & ")
	s = strings.ReplaceAll(s, " And ", " & ")
	return Curly(s)
}
