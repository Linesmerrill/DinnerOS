package recipes

import (
	"regexp"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Ingredient lines as recipe sites and people write them:
//
//	"1 ½ cups all-purpose flour (sifted)"
//	"2-3 cloves garlic, minced"
//	"1 (14.5 oz) can diced tomatoes"
//	"1 cup plus 2 tablespoons milk"
//	"1 lb / 450 g ground beef"
//	"Salt and pepper, to taste"
//
// parseIngredientLine splits one into an amount, a unit, a name, and a note.
// It never drops what the line said: RawText keeps the whole line and Note
// keeps everything that isn't the amount or the name, so the person
// reviewing sees exactly what the page said next to what was read.

var (
	// amountPart is one amount: "1 1/2", "1/2", "1.5", "1".
	amountPart = `[0-9]+\s+[0-9]+/[0-9]+|[0-9]+/[0-9]+|[0-9]*\.[0-9]+|[0-9]+`
	// plusRe is a second amount added to the first: "plus 2 tablespoons", "+ 2 Tbsp".
	plusRe = regexp.MustCompile(`(?i)^(?:plus|\+|and)\s+(` + amountPart + `)\s*(.*)$`)
	// altAmountRe is the same amount in other units: "/ 450 g", "or 450g".
	altAmountRe = regexp.MustCompile(`(?i)^(?:/|or)\s*(?:` + amountPart + `)\s*[a-z]+\.?\s+(.*)$`)
	// parenRe is a parenthetical anywhere in the name.
	parenRe = regexp.MustCompile(`\s*\(([^()]*)\)`)
	// leadingOfRe is the "of" in "2 cups of flour".
	leadingOfRe = regexp.MustCompile(`(?i)^of\s+`)
	// toTasteRe is a line with no amount meant: "to taste", "as needed".
	toTasteRe = regexp.MustCompile(`(?i)\b(?:to taste|as needed|for serving|for garnish)\b`)
	// mixedNumberRe is a mixed number written "1-1/2" or "2 and 1/2".
	mixedNumberRe = regexp.MustCompile(`(?i)^([0-9]+)(?:-|\s+and\s+)([0-9]+/[0-9]+)\b`)
	// containerSizeRe is a package size written before its container with no
	// parentheses: "15oz. can black beans", "15-ounce can chickpeas".
	containerSizeRe = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?\s*-?\s*(?:oz|ounces?|g|grams?|ml)\.?)\s+((?:cans?|jars?|packages?|box(?:es)?|bags?|cartons?)\b.*)$`)
	// unitSlashRe is a unit run into its other-units twin: "250g/8oz".
	unitSlashRe = regexp.MustCompile(`(?i)^([a-z]+\.?)/([0-9])`)
	// bonelessRe is "boneless, skinless", whose comma isn't a prep note's.
	bonelessRe = regexp.MustCompile(`(?i)\b(boneless|skinless),\s*(boneless|skinless)\b`)
	// compoundUnits are two-word units, checked before one-word ones.
	compoundUnits = map[string]string{"fl oz": "floz", "fluid ounce": "floz", "fluid ounces": "floz", "fl. oz": "floz"}
)

func parseIngredientLine(line string) DraftIngredient {
	raw := clip(line, maxDraftLine)
	rest := strings.TrimSpace(whitespaceRe.ReplaceAllString(bulletRe.ReplaceAllString(raw, ""), " "))
	rest = strings.ReplaceAll(rest, "⁄", "/") // the fraction slash some sites use
	out := DraftIngredient{RawText: raw}
	var notes []string

	rest = expandFractions(rest)
	// "1-1/2 cups" is one and a half cups, not a range ending at a half.
	rest = mixedNumberRe.ReplaceAllString(rest, "$1 $2")
	if m := leadAmountRe.FindStringSubmatch(rest); m != nil {
		if _, ok := parseQuantity(m[1]); ok {
			out.Quantity = strings.TrimSpace(m[1])
			rest = m[2]
		}
		// "2-3 cloves garlic": the larger amount, so there's enough on hand.
		// RawText keeps the range.
		if r := rangeRestRe.FindStringSubmatch(rest); r != nil {
			if _, ok := parseQuantity(r[1]); ok {
				out.Quantity = strings.TrimSpace(r[1])
				rest = r[2]
			}
		}
		// "1 (14.5 oz) can diced tomatoes": the size is the can's, and the
		// line is a can of diced tomatoes.
		if m := sizeNoteRe.FindStringSubmatch(rest); m != nil {
			notes = append(notes, strings.Trim(strings.TrimSpace(m[0]), "()"))
			rest = rest[len(m[0]):]
		} else if m := containerSizeRe.FindStringSubmatch(rest); m != nil {
			notes = append(notes, strings.TrimSuffix(m[1], "."))
			rest = m[2]
		}
	}

	if out.Quantity != "" {
		var unit string
		rest = unitSlashRe.ReplaceAllString(rest, "$1 / $2")
		unit, rest = takeUnit(rest)
		out.Unit = unit
		// "1 unit Yellow Onion": a meal kit's way of saying one, counted.
		if f := strings.Fields(rest); unit == "" && len(f) > 1 && (strings.EqualFold(f[0], "unit") || strings.EqualFold(f[0], "units")) {
			rest = strings.Join(f[1:], " ")
		}
		// "1 cup plus 2 tablespoons": the same kind of unit adds up.
		if m := plusRe.FindStringSubmatch(rest); m != nil && unit != "" {
			if extraUnit, after := takeUnit(m[2]); extraUnit != "" {
				if sum, ok := addAmounts(out.Quantity, unit, m[1], extraUnit); ok {
					out.Quantity = sum
					rest = after
				}
			}
		}
		// "1 lb / 450 g ground beef": the first is kept.
		if m := altAmountRe.FindStringSubmatch(rest); m != nil {
			rest = m[1]
		}
		rest = leadingOfRe.ReplaceAllString(rest, "")
	}

	// Parentheticals are notes: "(sifted)", "(about 2 cups)", "(optional)".
	for _, m := range parenRe.FindAllStringSubmatch(rest, -1) {
		if n := strings.TrimSpace(m[1]); n != "" {
			notes = append(notes, n)
		}
	}
	rest = strings.TrimSpace(parenRe.ReplaceAllString(rest, ""))

	// "1 onion, diced": what follows the comma is how it's readied.
	name := bonelessRe.ReplaceAllString(rest, "$1 $2")
	if before, after, ok := strings.Cut(name, ","); ok {
		name = before
		if n := strings.TrimSpace(after); n != "" {
			notes = append(notes, n)
		}
	}
	// "Salt to taste": the words after the name are a note.
	if trimmed := trimNameTail(name); trimmed != name {
		notes = append(notes, strings.TrimSpace(strings.TrimLeft(name[len(trimmed):], " ,")))
		name = trimmed
	}
	if out.Quantity == "" && toTasteRe.MatchString(raw) && !containsNote(notes, "to taste") {
		if m := toTasteRe.FindString(raw); m != "" && !containsNote(notes, m) {
			notes = append(notes, strings.ToLower(m))
		}
	}

	out.Name = clip(strings.Trim(strings.TrimSpace(name), " ,;:-"), maxDraftLine)
	if out.Name == "" {
		out.Name = clip(strings.TrimSpace(rest), maxDraftLine)
	}
	out.Note = clip(strings.Join(notes, ", "), maxDraftLine)
	return out
}

// takeUnit reads a unit word off the front of s, returning its code and the
// rest. A word that isn't a unit is left alone, and so is a unit that would
// leave no name ("2 cans" with nothing after is two cans of nothing).
func takeUnit(s string) (string, string) {
	lower := strings.ToLower(s)
	for words, code := range compoundUnits {
		if strings.HasPrefix(lower, words) {
			after := strings.TrimLeft(s[len(words):], ". ")
			if after != "" {
				return code, after
			}
		}
	}
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return "", s
	}
	word := strings.Trim(fields[0], ".,")
	code, ok := manualUnitWords[strings.ToLower(word)]
	switch {
	case word == "T" || word == "Tbs" || word == "TB":
		code, ok = "tbsp", true
	case word == "t":
		code, ok = "tsp", true
	case strings.EqualFold(word, "c"):
		code, ok = "cup", true
	}
	if !ok {
		return "", s
	}
	return code, strings.Join(fields[1:], " ")
}

// addAmounts adds b (in unitB) to a (in unitA), in unitA, when the units are
// the same kind. The sum is written as a fraction the import contract keeps.
func addAmounts(a, unitA, b, unitB string) (string, bool) {
	ua, err1 := ingredients.LookupUnit(unitA)
	ub, err2 := ingredients.LookupUnit(unitB)
	qa, ok1 := parseQuantity(a)
	qb, ok2 := parseQuantity(b)
	if err1 != nil || err2 != nil || !ok1 || !ok2 || !ub.CanConvertTo(ua) {
		return "", false
	}
	x, err1 := ingredients.QuantityFromFloat(qa)
	y, err2 := ingredients.QuantityFromFloat(qb)
	if err1 != nil || err2 != nil {
		return "", false
	}
	converted, err := ingredients.Convert(y, ub, ua)
	if err != nil {
		return "", false
	}
	sum := x.Add(converted)
	f := sum.Float64()
	// Keep it readable: to an eighth, which is how a kitchen measures.
	eighths := int(f*8 + 0.5)
	if eighths == 0 {
		return "", false
	}
	whole, frac := eighths/8, eighths%8
	switch {
	case frac == 0:
		return itoa(whole), true
	case whole == 0:
		return fraction(frac), true
	default:
		return itoa(whole) + " " + fraction(frac), true
	}
}

func fraction(eighths int) string {
	n, d := eighths, 8
	for d > 1 && n%2 == 0 {
		n, d = n/2, d/2
	}
	return itoa(n) + "/" + itoa(d)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func containsNote(notes []string, s string) bool {
	for _, n := range notes {
		if strings.Contains(strings.ToLower(n), strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// groupName is a heading's name without its colon or "For the": "For the
// sauce:" is "Sauce".
func groupName(heading string) string {
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(heading), ":"))
	lower := strings.ToLower(name)
	for _, prefix := range []string{"for the ", "for "} {
		if strings.HasPrefix(lower, prefix) {
			name = name[len(prefix):]
			break
		}
	}
	if name == "" {
		return ""
	}
	return clip(strings.ToUpper(name[:1])+name[1:], 100)
}
