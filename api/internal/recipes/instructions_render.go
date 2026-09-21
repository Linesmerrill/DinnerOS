package recipes

import (
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

func buildUnitWords() map[string]bool {
	out := map[string]bool{}
	for _, code := range ingredients.UnitCodes() {
		u, err := ingredients.LookupUnit(code)
		if err != nil {
			continue
		}
		for _, w := range []string{u.Code, u.Singular, u.Plural} {
			if w != "" {
				out[strings.ToLower(w)] = true
			}
		}
	}
	return out
}

// renderStep annotates one step against the recipe's ingredients.
func renderStep(step Step, mentions []mention) InstructionStep {
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
		if amountShown[hit] {
			// The same ingredient twice in one step: the amount belongs to
			// the first mention, so the second is only marked.
			amount = nil
		}
		if amount != nil {
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
			Amount: amount, Spicy: m.spicy, Substituted: m.substituted,
			SpecialtyID: m.specialtyID, SpecialtyName: m.specialtyName,
		})
		if m.note != "" && !noted[m.note] {
			noted[m.note] = true
			out.Notes = append(out.Notes, StepNote{Kind: NoteSubstitution, SpecialtyID: m.specialtyID, Text: m.note})
		}
		i += length
	}
	flush()
	var b strings.Builder
	for _, s := range segments {
		b.WriteString(s.Text)
	}
	out.Segments = segments
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
		case r == '/' || r == '.' || r == ',':
		case unicode.IsNumber(r):
			digits = true
		default:
			return false
		}
	}
	return digits
}
