package recipes

import (
	"math/big"
	"regexp"
	"sort"
	"strconv"
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
	// Home recipes spell units out: "2 tablespoons olive oil", "1 lb beef".
	for w, code := range manualUnitWords {
		if _, ok := out[w]; !ok {
			out[w] = code
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
	// "1 ½ tablespoons of the olive oil", "1 cup of broth": the amount is
	// still the step's own, with "of" between.
	end -= unitOfLen(text[:end])
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

// unitOfLen is how many runes at the end of text are " of" or " of the"
// after a unit ("tablespoons of the"), or 0. Only after a unit: "2 of the
// eggs" and "¼ of the onion" are shares, read elsewhere.
func unitOfLen(text []rune) int {
	end := len(text)
	word, start := lastWord(text, end)
	if strings.EqualFold(word, "the") {
		end = start - trailingSpaces(text, start)
		word, start = lastWord(text, end)
	}
	if !strings.EqualFold(word, "of") {
		return 0
	}
	gap := trailingSpaces(text, start)
	if gap == 0 {
		return 0
	}
	if unit, _ := lastWord(text, start-gap); !unitWords[strings.ToLower(unit)] {
		return 0
	}
	return len(text) - (start - gap)
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
	// "<amount…> for <n> serving(s)", or just "<amount…> for <n>".
	if len(inner) >= 2 && !strings.HasPrefix(inner[len(inner)-1], "serving") {
		inner = append(inner, "servings")
	}
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

// weUsedRe is a card's note of how much of a seasoning this step takes,
// right after the mention: "salt (we used ½ tsp; 1 tsp for 4)", "(we used ¾
// tsp salt; 1 ½ tsp for 4 servings)".
var (
	weUsedRe       = regexp.MustCompile(`(?i)^\s*\(we used ([^;()]+?)(?:[;,]\s*([^()]+?) for (\d+)(?: servings?)?)?\)`)
	weUsedAmountRe = regexp.MustCompile(`(?i)^\s*([\d½¼¾⅓⅔⅛][\d½¼¾⅓⅔⅛⁄/. ]*?)\s*(tsp|tbsp|teaspoons?|tablespoons?|cups?|oz)\b`)
)

// weUsed reads that note at the start of text: the amount for the card's
// smallest box, the amount for the other box and its size when given, and
// how many runes the note spans.
func weUsed(text []rune) (base, other Measure, otherSize, length int, ok bool) {
	m := weUsedRe.FindStringSubmatch(string(text))
	if m == nil {
		return Measure{}, Measure{}, 0, 0, false
	}
	parse := func(s string) (Measure, bool) {
		a := weUsedAmountRe.FindStringSubmatch(s)
		if a == nil {
			return Measure{}, false
		}
		stated, used, ok := statedAmount([]rune(strings.TrimSpace(a[1]) + " " + a[2] + " "))
		if !ok || used == 0 {
			return Measure{}, false
		}
		return stated, true
	}
	base, ok = parse(m[1])
	if !ok {
		return Measure{}, Measure{}, 0, 0, false
	}
	if m[2] != "" {
		if o, ok := parse(m[2]); ok {
			if n, err := strconv.Atoi(m[3]); err == nil {
				other, otherSize = o, n
			}
		}
	}
	return base, other, otherSize, len([]rune(m[0])), true
}

// renderStep annotates one step against the recipe's ingredients.
// wedgeCitrus is "lime" or "lemon" for those ingredients, else "".
func wedgeCitrus(name string) string {
	switch n := strings.ToLower(name); n {
	case "lime", "lemon":
		return n
	}
	return ""
}

// stepAmounts carries what renderStep needs to read the amounts a step
// wrote itself: the size being cooked, the size the card's own numbers are for
// (its smallest), and which ingredients have an amount written in some step.
type stepAmounts struct {
	servings, base int
	stated         map[int]bool
	// found, when set, collects the ingredients this step writes an amount
	// for (statedMentions).
	found map[int]bool
	// notedBefore holds the specialty ingredients an earlier step already
	// explained, and seen the ingredients an earlier step already named.
	notedBefore map[string]bool
	seen        map[int]bool
	// home is set for a recipe written for one serving size: one a person
	// added, not a meal-kit card.
	home bool
}

// relativeWords end the text before a mention that is a share of it, not the
// whole amount: "the remaining onion", "the rest of the butter".
var relativeWords = []string{
	"remaining", "rest of the", "rest of", "reserved",
	// "Add more chili flakes if you like": to taste, not a measure.
	"add more", "more",
	// "a drizzle of oil": the step says how much, in its own words.
	"drizzle of", "splash of", "pinch of", "dash of", "sprinkle of", "handful of", "knob of", "pat of",
	"drizzle of the", "splash of the", "pinch of the",
	// "Open package of chicken", "one packet of sour cream": the whole pack.
	"package of", "packet of", "packets of", "container of", "can of", "jar of", "bag of",
	"layer of", "dollop of", "tops of",
	// Home cooks measure by eye: "most of the cheese", "a glug of oil", "a
	// couple shakes of Worcestershire".
	"most of the", "most of", "some of the", "some", "a little", "a bit of", "a few",
	"glug of", "shake of", "shakes of", "squeeze of", "spoonful of", "scoop of",
	// "Wash and dry produce (except green beans)".
	"except",
}

// fractionOf reads "half the", "half of the", or "¼ of the" at the end of the
// text before a mention, and returns the fraction and how many runes of the
// text it spans with the space after it.
func fractionOf(text []rune) (*big.Rat, int, bool) {
	lower := strings.ToLower(string(text))
	trimmed := strings.TrimRightFunc(lower, unicode.IsSpace)
	if len(trimmed) == len(lower) {
		return nil, 0, false
	}
	for _, phrase := range []string{"half of the", "half the"} {
		if strings.HasSuffix(trimmed, phrase) && (len(trimmed) == len(phrase) || trimmed[len(trimmed)-len(phrase)-1] == ' ') {
			return big.NewRat(1, 2), len([]rune(lower)) - len([]rune(trimmed)) + len([]rune(phrase)), true
		}
	}
	if !strings.HasSuffix(trimmed, " of the") {
		return nil, 0, false
	}
	head := []rune(strings.TrimSuffix(trimmed, " of the"))
	word, start := lastWord(head, len(head))
	if !isNumberWord(word) {
		return nil, 0, false
	}
	q, err := ingredients.ParseQuantity(strings.ReplaceAll(word, "\u2044", "/"))
	if err != nil || q.Cmp(ingredients.NewQuantity(1, 1)) >= 0 {
		return nil, 0, false
	}
	return q.Rat(), len([]rune(lower)) - start, true
}

// measuredUnits are units a share can be measured in: half of "1 thumb"
// ginger isn't an amount anyone measures.
var measuredUnits = map[string]bool{"tsp": true, "tbsp": true, "cup": true, "oz": true, "fl oz": true, "ml": true, "l": true, "g": true, "kg": true, "lb": true}

// weUsedStartRe is any "(we used" note right after a mention.
var weUsedStartRe = regexp.MustCompile(`(?i)^\s*\(we used`)

// toTasteNoteRe is "to taste (we used …)" right after a mention.
var toTasteNoteRe = regexp.MustCompile(`(?i)^\s+(?:to taste|as you like|if desired|or to taste)\s*\(we used`)

// dropForeignNotes removes a card's notes for a box size not being cooked
// that aren't a single amount ("(2 tsp water and 1½ tsp salt for 4
// servings)"), before ingredient names are found, so a name inside one
// doesn't split it and leave it behind. Single amounts stay: the mention
// they follow reads them (otherServings).
func dropForeignNotes(text string, servings, base int) string {
	locs := boxNoteRe.FindAllStringSubmatchIndex(text, -1)
	for j := len(locs) - 1; j >= 0; j-- {
		loc := locs[j]
		inner := strings.TrimSpace(text[loc[2]:loc[3]])
		n, err := strconv.Atoi(text[loc[4]:loc[5]])
		if err != nil || n <= 0 || noteAmountRe.MatchString(inner) || strings.HasPrefix(strings.ToLower(inner), "we used") {
			continue
		}
		useNote := servings >= n
		if base > 0 && base < n {
			useNote = 2*servings >= base+n
		}
		if !useNote {
			text = text[:loc[0]] + text[loc[1]:]
		}
	}
	return text
}

// noteAhead reports whether the sentence ahead carries a box-size note,
// which settles the amount instead ("(1 cup for 4 servings)").
func noteAhead(text []rune) bool {
	s := string(text)
	if end := strings.IndexAny(s, ".;"); end >= 0 {
		s = s[:end]
	}
	return boxNoteRe.MatchString(s)
}

// describesWater reports whether the text after a name makes it describe
// water ("pasta cooking water", "noodle water").
func describesWater(after []rune) bool {
	s := string(after)
	return strings.HasPrefix(s, " cooking water") || strings.HasPrefix(s, " water") || strings.HasPrefix(s, " cooking liquid")
}

// countedUnitWords are units written in the singular that take an "s" after
// more than one.
var countedUnitWords = map[string]bool{"cup": true, "clove": true, "can": true, "package": true, "thumb": true, "bunch": true, "slice": true}

// allNoteRe is a card's "(all for 4 servings)" after a share.
var allNoteRe = regexp.MustCompile(`^\s*\(all for (\d+)(?: servings)?\)`)

// allNote reads "(all for N servings)" at the start of text: N and how many
// runes it spans.
func allNote(text []rune) (int, int, bool) {
	m := allNoteRe.FindStringSubmatch(string(text))
	if m == nil {
		return 0, 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	return n, len([]rune(m[0])), true
}

// wedgesOf is n lime or lemon wedges.
func wedgesOf(n ingredients.Quantity) *Measure {
	return &Measure{Quantity: n, Unit: "wedge"}
}

// waterPhrase is an amount of water a step asks for: "¾ cup water", "1 ½ cups
// hot water", "2 TBSP of water".
var waterPhrase = regexp.MustCompile(`(?i)(?:\d+(?:[./]\d+)?\s*)?[\d½¼¾⅓⅔⅛]+\s*(?:cups?|tbsp|tablespoons?|tsp|teaspoons?|ml|fl oz|oz|quarts?|qt)\s+(?:of\s+)?(?:(?:hot|warm|cold|boiling|lukewarm)\s+)?water\b`)

// NoIngredient is Segment.Ingredient for a mention that isn't one of the
// recipe's ingredients (water).
const NoIngredient = -1

// markWater splits each amount of water out of the text segments as its own
// mention, named Water and tied to no recipe ingredient.
func markWater(segments []Segment) []Segment {
	var out []Segment
	for _, s := range segments {
		if s.Kind != SegmentText {
			out = append(out, s)
			continue
		}
		text := s.Text
		for {
			loc := waterPhrase.FindStringIndex(text)
			if loc == nil {
				break
			}
			if loc[0] > 0 {
				out = append(out, Segment{Kind: SegmentText, Text: text[:loc[0]]})
			}
			out = append(out, Segment{Kind: SegmentIngredient, Text: text[loc[0]:loc[1]], Name: "Water", Ingredient: NoIngredient})
			text = text[loc[1]:]
		}
		if text != "" {
			out = append(out, Segment{Kind: SegmentText, Text: text})
		}
	}
	return out
}

func renderStep(step Step, mentions []mention, amounts stepAmounts) InstructionStep {
	// Recipes imported before HTML was cleaned still read as plain text.
	step.Text = CleanStepText(step.Text)
	original := step.Text
	step.Text = dropForeignNotes(step.Text, amounts.servings, amounts.base)
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
	seenHere := map[int]bool{}
	noted := map[string]bool{}
	flush := func() {
		if len(plain) > 0 {
			text := boxNotes(string(plain), amounts.servings, amounts.base)
			segments = append(segments, Segment{Kind: SegmentText, Text: text})
			plain = plain[:0]
		}
	}
	// Mentions this step gives their own amount somewhere ("Shake coconut
	// milk … Stir ⅔ cup coconut milk"): an earlier mention without one
	// doesn't take the whole amount.
	statedLater := map[int]bool{}
	for i := 0; i < len(runes); {
		hit, length := matchAt(lower, i, candidates)
		if hit < 0 || describesWater(lower[i+length:]) {
			i++
			continue
		}
		head := runes[max(0, i-40):i]
		head = head[:len(head)-trailingPrepLen(head)]
		if _, _, ok := statedAmount(head); ok || statedRange(head) {
			statedLater[hit] = true
		}
		i += length
	}
	// listShare is the share a list started with ("half of the garlic
	// powder, paprika, and salt"), carried to each name after it.
	var listShare *big.Rat
	for i := 0; i < len(runes); {
		hit, length := matchAt(lower, i, candidates)
		// "½ cup pasta cooking water": the water, not the pasta. The card
		// wrote it for its smallest box, so a bigger one reserves more.
		if hit >= 0 && describesWater(lower[i+length:]) {
			if st, stLen, ok := statedAmount(plain); ok && st.Unit != "" && amounts.base > 0 &&
				amounts.servings != amounts.base && !noteAhead(runes[i:]) {
				scaled := kitchenSpoon(Measure{Quantity: st.Quantity.MulRat(big.NewRat(int64(amounts.servings), int64(amounts.base))), Unit: st.Unit})
				plain = append(plain[:len(plain)-stLen], []rune(scaled.Text()+" ")...)
			}
			hit = -1
		}
		if hit < 0 {
			plain = append(plain, runes[i])
			i++
			continue
		}
		// Two lines of the same name ("brown sugar" in the rub and again in
		// the sauce): once a step has used the first, the next step's mention
		// is the second.
		if amounts.seen != nil {
			for _, twin := range mentions[hit].twins {
				if !amounts.seen[hit] || seenHere[hit] {
					break
				}
				hit = twin
			}
		}
		m := mentions[hit]
		// Only a bare name right after the shared one takes its share.
		var carried *big.Rat
		if listShare != nil && listJoinRe.MatchString(string(plain)) {
			carried = listShare
		}
		listShare = nil
		// "the melted butter": the amount goes before how it's prepared
		// ("⅓ cup melted butter"), so the words before it are read without
		// them and put back after.
		prepLen := trailingPrepLen(plain)
		prep := string(plain[len(plain)-prepLen:])
		plain = plain[:len(plain)-prepLen]
		amount := m.amount
		if _, _, ok := statedAmount(plain); statedLater[hit] && !ok && !amountShown[hit] {
			amount = nil
		}
		// "Add 2-3 cloves garlic": a range is the cook's call, so the step's
		// own words stand and no amount goes in front.
		ranged := statedRange(plain)
		if ranged {
			amount = nil
			if amounts.found != nil {
				amounts.found[hit] = true
			}
		}
		shownBefore := amountShown[hit]
		before := strings.TrimRightFunc(string(plain), unicode.IsSpace)
		squeeze := wedgeCitrus(m.name) != "" && len(before) < len(string(plain)) &&
			strings.HasSuffix(strings.ToLower(before), "squeeze of")
		part := false
		skipAfter := 0
		// The card wrote this step's share ("1 TBSP butter (2 TBSP for 4
		// servings)"): that share is the amount here, not the recipe's total,
		// which belongs on the ingredient list.
		keepsName := !m.substituted || m.display == m.name
		if stated, statedLen, ok := statedAmount(plain); ok && !ranged && !shownBefore && keepsName && !m.swapped {
			if amounts.found != nil {
				amounts.found[hit] = true
			}
			other, otherSize, otherLen, hasOther := otherServings(runes[i+length:])
			if stated.Unit == "" && m.amount != nil {
				stated.Unit = m.amount.Unit
			}
			claim := true
			switch {
			case hasOther && amounts.servings == otherSize:
				stated = other
			case amounts.servings == amounts.base || amounts.base == 0:
			case m.baseAmount != nil && m.amount != nil &&
				m.amount.Unit == m.baseAmount.Unit && !m.baseAmount.Quantity.IsZero():
				// The card's numbers are for its smallest box; the recipe's own
				// amounts for the two sizes say how this step's share grows.
				ratio := new(big.Rat).Quo(m.amount.Quantity.Rat(), m.baseAmount.Quantity.Rat())
				stated.Quantity = stated.Quantity.MulRat(ratio)
			default:
				// No way to know this step's share at this size: the step keeps
				// its words, and no amount is claimed.
				amount, claim = nil, false
			}
			// "1 teaspoon salt" for a "salt to taste" line: the step's amount
			// is the only one there is.
			if claim && (m.amount != nil || stated.Unit != "") {
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
		} else if amounts.seen[hit] {
			// An earlier step already said how much ("Quarter 1 lime" … "While
			// pork cooks"): the whole amount again would read as more.
			amount = nil
		}
		lowerBefore := strings.ToLower(before)
		for _, w := range relativeWords {
			if endsWithWord(lowerBefore, w) {
				amount = nil
			}
		}
		// The text keeps its own words when the amount is only for the list:
		// "¼ of the onion" is ¼ onion to have ready, and reads as written.
		inText := true
		fraction, fractionLen, isFraction := fractionOf(plain)
		if !isFraction && carried != nil && !amountShown[hit] {
			fraction, isFraction = carried, true
		}
		// "half the lemon zest" is half the zest, not half the lemon.
		if isFraction && strings.HasPrefix(strings.ToLower(string(runes[i+length:])), " zest") {
			isFraction, amount = false, nil
		}
		if isFraction && (carried != nil || sharedList(amounts.home, runes[i+length:])) {
			listShare = fraction
		}
		if isFraction {
			amount = nil
			if m.amount != nil && m.amount.Unit == "count" && !m.leftOut && !shownBefore {
				share := &Measure{Quantity: m.amount.Quantity.MulRat(fraction), Unit: "count"}
				if m.wedges {
					// "juice from half the lime" after "Quarter lime": 2 wedges.
					share = wedgesOf(share.Quantity.Mul(ingredients.NewQuantity(4, 1)))
					plain = plain[:len(plain)-fractionLen]
				} else {
					inText = false
				}
				amount, part = share, true
				amountShown[hit] = true
			} else if m.amount != nil && measuredUnits[m.amount.Unit] && !m.leftOut && !shownBefore && !m.swapped {
				// "Stir in half the curry powder (all for 4 servings)": this
				// step's share, as an amount to measure.
				if n, noteLen, ok := allNote(runes[i+length:]); ok {
					skipAfter = noteLen
					if amounts.servings >= n {
						fraction = big.NewRat(1, 1)
					}
				}
				share := kitchenSpoon(tidySpoons(Measure{Quantity: m.amount.Quantity.MulRat(fraction), Unit: m.amount.Unit}))
				plain = plain[:len(plain)-fractionLen]
				amount, part = &share, fraction.Cmp(big.NewRat(1, 1)) != 0
				amountShown[hit] = true
			}
		}
		// "chili flakes to taste (we used ⅛ tsp)": the card's words say how
		// much, so no total goes in front.
		// "chili flakes (we used ½ tsp; add a pinch more if you like)": a tip
		// the note keeps, so the card's words stand, with no total in front.
		if rest := string(runes[i+length:]); toTasteNoteRe.MatchString(rest) ||
			(weUsedStartRe.MatchString(rest) && !weUsedRe.MatchString(rest)) {
			amount = nil
		}
		// "salt (we used ½ tsp; 1 tsp for 4)": the card's own measure for this
		// step, for the size being cooked, in place of the recipe's total.
		if base, other, otherSize, noteLen, ok := weUsed(runes[i+length:]); ok && !m.leftOut && !shownBefore && !m.swapped {
			share := base
			switch {
			case otherSize > 0 && amounts.servings == otherSize:
				share = other
			case otherSize > 0 && amounts.servings > otherSize:
				share.Quantity = other.Quantity.MulRat(big.NewRat(int64(amounts.servings), int64(otherSize)))
				share.Unit = other.Unit
			case amounts.base > 0 && amounts.servings != amounts.base:
				share.Quantity = base.Quantity.MulRat(big.NewRat(int64(amounts.servings), int64(amounts.base)))
			}
			tidy := kitchenSpoon(tidySpoons(share))
			amount, part, skipAfter = &tidy, true, noteLen
			// The card measures this step's share, so other steps don't get
			// the recipe's total ("season with salt" elsewhere stays as is).
			if amounts.found != nil {
				amounts.found[hit] = true
			}
			amountShown[hit] = true
		}
		if squeeze {
			// One wedge for the card's smallest box, more for a bigger one.
			n := ingredients.NewQuantity(1, 1)
			if amounts.base > 0 && amounts.servings > 0 {
				n = ingredients.NewQuantity(int64(amounts.servings), int64(amounts.base))
			}
			amount, part = wedgesOf(n), true
			if m.leftOut || shownBefore {
				amount = nil
			}
		}
		if i > 0 && runes[i-1] == '-' {
			// Part of a compound word ("soy-sriracha sauce"): mark it, but an
			// amount there would read "soy-1 tsp sriracha".
			amount = nil
		}
		if m.leftOut {
			// A left-out ingredient keeps the step's own words and gets no
			// amount: nothing of it goes in.
			amount = nil
		}
		if m.packaged && amount != nil && amount.Unit == "count" {
			// "1 Black Beans" is a can: "rinse 1 beans" would be wrong.
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
		movePrep := prep != "" && amount != nil && inText && amount.Unit != "wedge"
		if !movePrep {
			plain = append(plain, []rune(prep)...)
			prep = ""
		}
		flush()
		surface := string(runes[i : i+length])
		switch {
		case m.swapped:
			surface = matchCase(m.display, surface)
		case m.substituted:
			surface = m.display
		case amount != nil && inText && amount.Unit == "count" && amount.Quantity.Cmp(ingredients.NewQuantity(1, 1)) > 0 &&
			singularize(surface) == surface && len(strings.Fields(surface)) <= 2:
			// "Quarter 3 limes", not "Quarter 3 lime". A longer name is a
			// product ("Sesame Ginger Crunch"), not something counted.
			surface = countPlural(surface)
		}
		text := surface
		if amount != nil && inText {
			text = amount.Text() + " " + prep + surface
		}
		// "a squeeze of lime juice" is a wedge, not the whole lime: the card
		// quartered it earlier. It reads "a squeeze of 1 lime wedge".
		if amount != nil && amount.Unit == "wedge" {
			noun := "wedge"
			if amount.Quantity.Cmp(ingredients.NewQuantity(1, 1)) > 0 {
				noun = "wedges"
			}
			text = amount.Quantity.Format() + " " + strings.ToLower(singularize(surface)) + " " + noun
			rest := strings.ToLower(string(runes[i+length:]))
			for _, tail := range []string{" wedges", " wedge", " juice"} {
				if strings.HasPrefix(rest, tail) {
					skipAfter = len(tail)
					break
				}
			}
		}
		segments = append(segments, Segment{
			Kind: SegmentIngredient, Text: text, IngredientID: m.ingredientID, Ingredient: hit, Name: m.display,
			Amount: amount, Part: part && amount != nil, Spicy: m.spicy, Substituted: m.substituted,
			SpecialtyID: m.specialtyID, SpecialtyName: m.specialtyName, LeftOut: m.leftOut,
			Prep: strings.TrimSpace(prep),
		})
		if m.leftOut {
			note := "You leave out the " + m.name + "."
			if !noted[note] {
				noted[note] = true
				out.Notes = append(out.Notes, StepNote{Kind: NoteLeftOut, SpecialtyID: m.specialtyID, Text: note})
			}
		} else if m.note != "" && !noted[m.note] {
			noted[m.note] = true
			note := m.note
			mixed := strings.HasPrefix(note, "Instead of ") || strings.HasPrefix(note, "To make ")
			switch {
			case mixed && amounts.notedBefore[m.specialtyID]:
				// An earlier step said how to mix it all.
				note = "Use the rest of the " + m.specialtyName + " you mixed."
			case mixed && part && amount != nil:
				// Mix it all now, since a later step uses the rest.
				note += " Use " + amount.Text() + " here and save the rest."
			}
			if m.specialtyID != "" && amounts.notedBefore != nil {
				amounts.notedBefore[m.specialtyID] = true
			}
			out.Notes = append(out.Notes, StepNote{Kind: NoteSubstitution, SpecialtyID: m.specialtyID, Text: note})
		}
		if amounts.seen != nil {
			seenHere[hit] = true
		}
		i += length + skipAfter
	}
	flush()
	for hit := range seenHere {
		amounts.seen[hit] = true
	}
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
	// Water isn't on the card's ingredient list, but "¾ cup water" is
	// something to measure, so it's bold like the rest.
	out.Segments = markWater(out.Segments)
	if rendered := b.String(); rendered != original {
		out.Text, out.Original = rendered, original
	}
	return out
}

var (
	// " (7-10 minutes for 4 servings)", " (2 cups for 4)"
	boxNoteRe = regexp.MustCompile(`\s*\(([^()]{1,90}?) for (\d+)(?: servings?)?\)`)
	// "7-10 minutes", "1½ cups": a number or range, then a word.
	noteAmountRe = regexp.MustCompile(`^([\d½¼¾⅓⅔⅛][\d½¼¾⅓⅔⅛\-–/. ]*?)\s*([A-Za-z]+)$`)
)

// boxNotes settles the notes a card writes for its other box size in text
// that isn't an ingredient ("5-7 minutes (7-10 minutes for 4 servings)"),
// since the size being cooked is already chosen. The note's value replaces
// the one before it when the note's size is the closer one; the note goes
// either way. A note with no single value to swap ("middle position (middle
// and top positions for 4 servings)", "(2 tsp water and 1½ tsp salt for 4
// servings)") stays as written when that size is the closer one, and goes
// otherwise.
func boxNotes(text string, servings, base int) string {
	locs := boxNoteRe.FindAllStringSubmatchIndex(text, -1)
	for j := len(locs) - 1; j >= 0; j-- {
		loc := locs[j]
		inner := text[loc[2]:loc[3]]
		n, err := strconv.Atoi(text[loc[4]:loc[5]])
		if err != nil || n <= 0 {
			continue
		}
		useNote := servings >= n
		if base > 0 && base < n {
			useNote = 2*servings >= base+n
		}
		prefix, rest := text[:loc[0]], text[loc[1]:]
		note := noteAmountRe.FindStringSubmatch(strings.TrimSpace(inner))
		if note == nil {
			if !useNote {
				text = prefix + rest
			}
			continue
		}
		unit := strings.TrimSuffix(strings.ToLower(note[2]), "s")
		before := regexp.MustCompile(`(?i)([\d½¼¾⅓⅔⅛][\d½¼¾⅓⅔⅛\-–/. ]*?)(\s*` + regexp.QuoteMeta(unit) + `s?\b)`)
		if useNote {
			if all := before.FindAllStringSubmatchIndex(prefix, -1); len(all) > 0 {
				last := all[len(all)-1]
				// The note's own unit too: "1½ cups", not "1½ cup".
				unitWord := matchCase(note[2], strings.TrimSpace(prefix[last[4]:last[5]]))
				// A card's "(1½ cup for 4)" still reads "1½ cups".
				if q, err := ingredients.ParseQuantity(strings.ReplaceAll(strings.TrimSpace(note[1]), "\u2044", "/")); err == nil &&
					q.Cmp(ingredients.NewQuantity(1, 1)) > 0 && countedUnitWords[strings.ToLower(unitWord)] {
					unitWord += "s"
				}
				prefix = prefix[:last[2]] + strings.TrimSpace(note[1]) + " " + unitWord + prefix[last[5]:]
			} else {
				// Nothing to swap it into: keep what the note says.
				text = prefix + " (" + strings.TrimSpace(inner) + ")" + rest
				continue
			}
		}
		text = prefix + rest
	}
	return text
}

// countPlural is the plural a step shows after a count: "3 limes", "2
// jalapeños", "3 tomatoes".
func countPlural(s string) string {
	head, last := splitLastWord(s)
	switch strings.ToLower(last) {
	case "tomato", "potato":
		return head + last + "es"
	}
	if strings.HasSuffix(strings.ToLower(last), "o") {
		return head + last + "s"
	}
	return pluralize(s)
}

// matchCase writes name the way the step wrote what it replaces: "ground
// beef" for "pork", "Ground beef" for "Pork".
func matchCase(name, surface string) string {
	lower := []rune(strings.ToLower(name))
	if first := []rune(surface); len(first) > 0 && len(lower) > 0 && unicode.IsUpper(first[0]) {
		lower[0] = unicode.ToUpper(lower[0])
	}
	return string(lower)
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
