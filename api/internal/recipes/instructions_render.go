package recipes

import (
	"math/big"
	"sort"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file finds the recipe's own ingredients inside a step's text. It never
// guesses: only the spellings nameForms derived from the recipe's ingredient
// list (and a specialty ingredient's aliases) are looked for, so a step that
// names something the recipe doesn't list is left as plain text.

// candidate is one spelling of one ingredient to look for.
type candidate struct {
	mention int
	form    []rune
}

// articles are dropped when an amount is written in front of the name, so
// "Add the Tex-Mex Paste" reads "Add 1 tbsp Tex-Mex Paste" rather than "Add
// the 1 tbsp Tex-Mex Paste".
var articles = map[string]bool{"the": true, "a": true, "an": true}

// unitWords are the unit codes and labels a step may already use in front of
// an ingredient ("2 tbsp gochujang"). An amount written like that is replaced
// by the amount for the servings being cooked, rather than left to contradict
// it.
var unitWords = buildUnitWords()

// unitCodes maps each of those words to its unit code.
var unitCodes = buildUnitCodes()

func buildUnitWords() map[string]bool {
	out := map[string]bool{}
	for w := range buildUnitCodes() {
		out[w] = true
	}
	return out
}

func buildUnitCodes() map[string]string {
	out := map[string]string{}
	for _, code := range ingredients.UnitCodes() {
		u, err := ingredients.LookupUnit(code)
		if err != nil {
			continue
		}
		for _, w := range []string{u.Code, u.Singular, u.Plural} {
			if w != "" {
				out[strings.ToLower(w)] = code
			}
		}
	}
	return out
}

// statedAmount reads the amount a step wrote right before a mention ("1
// TBSP", "1 ½ cups", "2"), and how many runes of text it spans with its
// trailing spaces. ok is false when the text doesn't end in one.
func statedAmount(text []rune) (m Measure, length int, ok bool) {
	spaces := trailingSpaces(text, len(text))
	if spaces == 0 {
		return Measure{}, 0, false
	}
	end := len(text) - spaces
	word, start := lastWord(text, end)
	unit := ""
	numEnd := end
	if code, isUnit := unitCodes[strings.ToLower(word)]; isUnit {
		unit = code
		numEnd = start - trailingSpaces(text, start)
	} else if !isNumberWord(word) {
		return Measure{}, 0, false
	}
	n := trailingNumbers(text, numEnd)
	if n == 0 {
		return Measure{}, 0, false
	}
	// Cards write fractions with the fraction slash ("1⁄4") as often as with "/".
	q, err := ingredients.ParseQuantity(strings.ReplaceAll(string(text[numEnd-n:numEnd]), "\u2044", "/"))
	if err != nil {
		return Measure{}, 0, false
	}
	return Measure{Quantity: q, Unit: unit}, len(text) - (numEnd - n), true
}

// otherServings reads the note meal-kit cards put after a mention for the
// other box size, " (2 TBSP for 4 servings)", at the start of text. It
// returns the amount, the serving size it's for, and how many runes it spans.
func otherServings(text []rune) (m Measure, servings, length int, ok bool) {
	i := 0
	for i < len(text) && text[i] == ' ' {
		i++
	}
	if i >= len(text) || text[i] != '(' {
		return Measure{}, 0, 0, false
	}
	closeAt := -1
	for j := i + 1; j < len(text) && j < i+48; j++ {
		if text[j] == ')' {
			closeAt = j
			break
		}
	}
	if closeAt < 0 {
		return Measure{}, 0, 0, false
	}
	inner := strings.Fields(strings.ToLower(string(text[i+1 : closeAt])))
	// "<amount…> for <n> serving(s)"
	if len(inner) < 4 || inner[len(inner)-3] != "for" ||
		!strings.HasPrefix(inner[len(inner)-1], "serving") {
		return Measure{}, 0, 0, false
	}
	n := 0
	for _, r := range inner[len(inner)-2] {
		if r < '0' || r > '9' {
			return Measure{}, 0, 0, false
		}
		n = n*10 + int(r-'0')
	}
	amount := []rune(strings.Join(inner[:len(inner)-3], " ") + " ")
	stated, used, ok := statedAmount(amount)
	if !ok || used != len(amount) || n == 0 {
		return Measure{}, 0, 0, false
	}
	return stated, n, closeAt + 1, true
}

// renderStep annotates one step against the recipe's ingredients.
// stepAmounts carries what renderStep needs to read the amounts a step
// wrote itself: the size being cooked, the size the card's own numbers are for
// (its smallest), and which ingredients have an amount written in some step.
type stepAmounts struct {
	servings, base int
	stated         map[int]bool
	// found, when set, collects the ingredients this step writes an amount
	// for (statedMentions).
	found map[int]bool
}

func renderStep(step Step, mentions []mention, amounts stepAmounts) InstructionStep {
	out := InstructionStep{Index: step.Index, Text: step.Text, ImageURL: step.ImageURL}
	candidates := make([]candidate, 0, len(mentions)*3)
	for i, m := range mentions {
		for _, form := range m.forms {
			candidates = append(candidates, candidate{mention: i, form: []rune(form)})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return len(candidates[i].form) > len(candidates[j].form) })

	runes := []rune(step.Text)
	lower := make([]rune, len(runes))
	for i, r := range runes {
		lower[i] = unicode.ToLower(r)
	}
	var segments []Segment
	var plain []rune
	amountShown := map[int]bool{}
	noted := map[string]bool{}
	flush := func() {
		if len(plain) > 0 {
			segments = append(segments, Segment{Kind: SegmentText, Text: string(plain)})
			plain = plain[:0]
		}
	}
	for i := 0; i < len(runes); {
		hit, length := matchAt(lower, i, candidates)
		if hit < 0 {
			plain = append(plain, runes[i])
			i++
			continue
		}
		m := mentions[hit]
		amount := m.amount
		shownBefore := amountShown[hit]
		part := false
		skipAfter := 0
		// The card wrote this step's share ("1 TBSP butter (2 TBSP for 4
		// servings)"): that share is the amount here, not the recipe's total,
		// which belongs on the ingredient list.
		if stated, statedLen, ok := statedAmount(plain); ok && !shownBefore && !m.substituted {
			if amounts.found != nil {
				amounts.found[hit] = true
			}
			other, otherSize, otherLen, hasOther := otherServings(runes[i+length:])
			if stated.Unit == "" && m.amount != nil {
				stated.Unit = m.amount.Unit
			}
			switch {
			case hasOther && amounts.servings == otherSize:
				stated = other
			case amounts.servings == amounts.base || amounts.base == 0:
			case m.baseAmount != nil && m.amount != nil && stated.Unit == m.baseAmount.Unit &&
				m.amount.Unit == m.baseAmount.Unit && !m.baseAmount.Quantity.IsZero():
				// The card's numbers are for its smallest box; the recipe's own
				// amounts for the two sizes say how this step's share grows.
				ratio := new(big.Rat).Quo(m.amount.Quantity.Rat(), m.baseAmount.Quantity.Rat())
				stated.Quantity = stated.Quantity.MulRat(ratio)
			default:
				// No way to know this step's share at this size: the step keeps
				// its words, and no amount is claimed.
				amount = nil
			}
			if amount != nil {
				amount = &stated
				part = m.amount == nil || stated.Quantity.Cmp(m.amount.Quantity) != 0 || stated.Unit != m.amount.Unit
				plain = plain[:len(plain)-statedLen]
				if len(plain) > 0 && plain[len(plain)-1] != ' ' {
					plain = append(plain, ' ')
				}
				amountShown[hit] = true
			}
			if hasOther {
				skipAfter = otherLen
			}
		} else if amounts.stated[hit] && !shownBefore {
			// Another step wrote its own share of this ingredient, so the
			// total would be wrong here ("the remaining butter").
			amount = nil
		}
		if m.leftOut {
			// A left-out ingredient keeps the step's own words and gets no
			// amount: nothing of it goes in.
			amount = nil
		}
		if shownBefore {
			// The same ingredient twice in one step: the amount belongs to
			// the first mention, so the second is only marked.
			amount = nil
		}
		if amount != nil && !amountShown[hit] {
			plain = plain[:len(plain)-trailingAmountLen(plain)]
			amountShown[hit] = true
		}
		flush()
		surface := string(runes[i : i+length])
		if m.substituted {
			surface = m.display
		}
		text := surface
		if amount != nil {
			text = amount.Text() + " " + surface
		}
		segments = append(segments, Segment{
			Kind: SegmentIngredient, Text: text, IngredientID: m.ingredientID, Name: m.display,
			Amount: amount, Part: part && amount != nil, Spicy: m.spicy, Substituted: m.substituted,
			SpecialtyID: m.specialtyID, SpecialtyName: m.specialtyName, LeftOut: m.leftOut,
		})
		if m.leftOut {
			note := "You leave out the " + m.name + "."
			if !noted[note] {
				noted[note] = true
				out.Notes = append(out.Notes, StepNote{Kind: NoteLeftOut, SpecialtyID: m.specialtyID, Text: note})
			}
		} else if m.note != "" && !noted[m.note] {
			noted[m.note] = true
			out.Notes = append(out.Notes, StepNote{Kind: NoteSubstitution, SpecialtyID: m.specialtyID, Text: m.note})
		}
		i += length + skipAfter
	}
	flush()
	var b strings.Builder
	for _, s := range segments {
		b.WriteString(s.Text)
	}
	out.Segments = segments
	named, left := 0, 0
	for _, s := range segments {
		if s.Kind == SegmentIngredient {
			named++
			if s.LeftOut {
				left++
			}
		}
	}
	out.LeftOut = named > 0 && left == named
	if rendered := b.String(); rendered != step.Text {
		out.Text, out.Original = rendered, step.Text
	}
	return out
}

// matchAt returns the longest candidate that starts at i on a word boundary
// and ends on one, or -1.
func matchAt(lower []rune, i int, candidates []candidate) (mention, length int) {
	if i > 0 && isWordRune(lower[i-1]) {
		return -1, 0
	}
	for _, c := range candidates {
		n := len(c.form)
		if i+n > len(lower) {
			continue
		}
		if i+n < len(lower) && isWordRune(lower[i+n]) {
			continue
		}
		if equalFoldRunes(lower[i:i+n], c.form) {
			return c.mention, n
		}
	}
	return -1, 0
}

// equalFoldRunes compares an already-lowered slice of the text with a form,
// lowering the form as it goes.
func equalFoldRunes(text, form []rune) bool {
	for i, r := range form {
		if text[i] != unicode.ToLower(r) {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// trailingAmountLen is how many runes at the end of the text before a mention
// the rendered amount replaces: the spaces, an article ("the"), or an amount
// the step already wrote ("2 tbsp", "1 ½ cups"). It is 0 when the text ends in
// anything else, so nothing a person wrote is dropped on a guess.
func trailingAmountLen(text []rune) int {
	spaces := trailingSpaces(text, len(text))
	if spaces == 0 && len(text) > 0 {
		return 0
	}
	end := len(text) - spaces
	word, start := lastWord(text, end)
	switch {
	case word == "":
		return 0
	case articles[strings.ToLower(word)]:
		return len(text) - start
	case unitWords[strings.ToLower(word)]:
		// A unit is only part of an amount when a number comes before it.
		gap := trailingSpaces(text, start)
		if n := trailingNumbers(text, start-gap); n > 0 {
			return len(text) - (start - gap - n)
		}
		return 0
	case isNumberWord(word):
		if n := trailingNumbers(text, end); n > 0 {
			return len(text) - (end - n)
		}
		return 0
	}
	return 0
}

// trailingNumbers is the length of the run of number words ending at end
// ("1 ½", "1/2"), including the spaces between them.
func trailingNumbers(text []rune, end int) int {
	consumed, pos := 0, end
	for pos > 0 {
		word, start := lastWord(text, pos)
		if word == "" || !isNumberWord(word) {
			break
		}
		// The space before a number word is only consumed when another
		// number word comes before it, so the space that separates the
		// amount from the rest of the sentence survives.
		consumed = end - start
		gap := trailingSpaces(text, start)
		if gap == 0 {
			break
		}
		pos = start - gap
	}
	return consumed
}

func trailingSpaces(text []rune, end int) int {
	n := 0
	for end-n > 0 && unicode.IsSpace(text[end-n-1]) {
		n++
	}
	return n
}

// lastWord returns the run of non-space runes ending at end and where it
// starts.
func lastWord(text []rune, end int) (word string, start int) {
	start = end
	for start > 0 && !unicode.IsSpace(text[start-1]) {
		start--
	}
	return string(text[start:end]), start
}

// isNumberWord reports whether a word is a written amount: digits, a fraction
// ("1/2"), or a fraction glyph ("½").
func isNumberWord(word string) bool {
	if word == "" {
		return false
	}
	digits := false
	for _, r := range word {
		switch {
		case unicode.IsDigit(r):
			digits = true
		case r == '/' || r == '\u2044' || r == '.' || r == ',':
		case unicode.IsNumber(r):
			digits = true
		default:
			return false
		}
	}
	return digits
}
