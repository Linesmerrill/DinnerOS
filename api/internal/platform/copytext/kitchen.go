package copytext

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The app's casing rule (build 122, Hark's review item 16): names and labels
// are Title Case ("Jasmine Rice", "Household Name", "Best By"); text that is a
// sentence or a status stays a sentence ("Best by Jul 21, 2027", "3 of your
// invites counted"). These helpers apply the rule to words the app didn't
// write: ingredient names a household typed, step text and descriptions a
// source wrote. Like Curly, they run at response time.

// minorWords stay lowercase inside a name: "Salt and Pepper", "Cream of
// Mushroom Soup".
var minorWords = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "by": true, "for": true, "from": true,
	"in": true, "of": true, "on": true, "or": true, "the": true, "to": true, "with": true, "&": true,
}

// IngredientName writes an ingredient name in Title Case: "bow tie pasta" and
// "Bow tie pasta" read "Bow Tie Pasta", "sun-dried tomatoes" reads "Sun-Dried
// Tomatoes", "cream of mushroom soup" reads "Cream of Mushroom Soup". A word
// with a capital after its first letter is someone's own spelling and stays
// ("iPhone", "BBQ"); words that start with a digit stay ("2% Milk").
func IngredientName(s string) string {
	words := strings.Split(s, " ")
	first := true
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = titleWord(w, first)
		first = false
	}
	return Curly(strings.Join(words, " "))
}

// titleWord capitalizes one space-separated word and each part of it after a
// hyphen ("sun-dried").
func titleWord(w string, first bool) string {
	parts := strings.Split(w, "-")
	for i, p := range parts {
		lead := strings.TrimLeft(p, "(\"“")
		bare := strings.ToLower(strings.TrimRight(lead, ",.)\"”"))
		if !first && i == 0 && minorWords[bare] {
			continue
		}
		parts[i] = p[:len(p)-len(lead)] + capitalFirst(lead)
	}
	return strings.Join(parts, "-")
}

// capitalFirst capitalizes w's first letter unless a later letter is already
// a capital.
func capitalFirst(w string) string {
	r, size := utf8.DecodeRuneInString(w)
	if size == 0 || !unicode.IsLower(r) {
		return w
	}
	for _, later := range w[size:] {
		if unicode.IsUpper(later) {
			return w
		}
	}
	return string(unicode.ToUpper(r)) + w[size:]
}

// rangeUnits are the words a range of numbers can count: times, temperatures,
// measures and sizes.
const rangeUnits = `seconds?|secs?|minutes?|mins?|hours?|hrs?|days?|weeks?|°|degrees?|inch(?:es)?|cups?|tbsp|tsp|tablespoons?|teaspoons?|oz|ounces?|lbs?|pounds?|grams?|g|kg|ml|cloves?|slices?|pieces?|servings?|people|portions?`

// numberRangeRe is a range of two numbers before a unit: "30-60 seconds",
// "15–18 minutes", "400-425°F". The hyphen must sit between two numbers with
// nothing but spaces, and the text before the first can't be a letter, a
// digit, a slash or a hyphen, so "9-by-13-inch", "sun-dried", "1-1/2 cups" and
// "COVID-19" are left alone.
var numberRangeRe = regexp.MustCompile(`(^|[^\p{L}\p{N}/⁄.\-])(\d+(?:\.\d+)?|\d*[½¼¾⅓⅔⅛])\s*[-–—]\s*(\d+(?:\.\d+)?|\d*[½¼¾⅓⅔⅛])(\s*(?i:` + rangeUnits + `))(\b|[^\p{L}]|$)`)

// Ranges writes a range of numbers the way a person says it: "30-60 seconds"
// reads "30 to 60 seconds". Only a range before a unit changes; a hyphenated
// word, a fraction ("1-1/2") or a bare "1-2" stays.
func Ranges(s string) string {
	if !strings.ContainsAny(s, "-–—") {
		return s
	}
	return numberRangeRe.ReplaceAllString(s, "${1}${2} to ${3}${4}${5}")
}

// importLabelRe is a label a scraper or a copied page left at the start of a
// description: "Web Description: ", "Description: ", "Recipe Summary: ".
var importLabelRe = regexp.MustCompile(`(?i)^\s*(?:(?:web|recipe|short|meta|page|og)\s+)?(?:description|summary|intro(?:duction)?|overview|about(?: this recipe)?)\s*:\s*`)

// Description drops the import labels a description starts with ("Web
// Description: Fun fact: …" reads "Fun fact: …"). The words after the label
// are the source's own and stay as written.
func Description(s string) string {
	for {
		loc := importLabelRe.FindStringIndex(s)
		if loc == nil {
			return strings.TrimSpace(s)
		}
		s = s[loc[1]:]
	}
}
