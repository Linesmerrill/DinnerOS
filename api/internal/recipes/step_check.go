package recipes

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Step checks look at a rendered recipe the way a cook would read it and name
// what reads wrong: a note for another box size left in, an amount that
// fights the step's own words, an ingredient no step ever finds. They run in
// tests over a corpus of recipes written in every phrasing seen so far
// (testdata/step_corpus.json), and on the server over members' own recipes,
// where a finding is recorded so a new phrasing gets noticed and fixed
// instead of confusing someone at the stove.

// Finding codes.
const (
	// FindingBoxNote: a note for another serving size is still in the text
	// ("(2 TBSP for 4 servings)" while cooking for 2).
	FindingBoxNote = "box_note"
	// FindingDoubleAmount: an amount in front of a mention and the card's
	// own measure after it ("2 Tbsp salt (we used ½ tsp)").
	FindingDoubleAmount = "double_amount"
	// FindingAmountBeforeRelative: an amount put in front of words that
	// already say how much ("half the 1 tsp cumin", "remaining 2 oz").
	FindingAmountBeforeRelative = "amount_before_relative"
	// FindingUnitPlural: a singular unit after more than one ("1½ cup").
	FindingUnitPlural = "unit_plural"
	// FindingLeftOutNotStruck: an ingredient the household leaves out that a
	// step still shows as something to add.
	FindingLeftOutNotStruck = "left_out_not_struck"
	// FindingUnusedIngredient: an ingredient no step names, so the checklist
	// can't place it and the steps never bold it.
	FindingUnusedIngredient = "unused_ingredient"
	// FindingMarkup: card markup left in the text ("chicken*", "<strong>",
	// "TBSP").
	FindingMarkup = "markup"
	// FindingRunTogether: two numbers run together ("10-121½-inch").
	FindingRunTogether = "run_together"
	// FindingRestWithoutAmount is a checklist row that says "the rest"
	// instead of how much: the total less what earlier steps used is known
	// (decision 631).
	FindingRestWithoutAmount = "rest_without_amount"
	// FindingSpoonedByWeight is a thick ingredient a cook spoons out (tomato
	// paste) given as a weight: nobody can measure 1.5 oz of paste.
	FindingSpoonedByWeight = "spooned_by_weight"
	// FindingHaveReadyMissing is a checklist that doesn't start with Have
	// Ready listing every ingredient (decision 635).
	FindingHaveReadyMissing = "have_ready_missing"
)

// Finding is one thing that reads wrong in a rendered recipe.
type Finding struct {
	Code string
	// Step is the step's index, or 0 for the recipe as a whole.
	Step int
	// Detail is the text that reads wrong, or the ingredient's name.
	Detail string
}

var (
	boxNoteLeftRe  = regexp.MustCompile(`(?i)\(([^()]{1,90}?) for (\d+)(?: servings?)?\)`)
	doubleAmountRe = regexp.MustCompile(`(?i)\d[\d½¼¾⅓⅔⅛ /⁄.]*\s*(?:tsp|tbsp|cups?|oz)\s+[a-z][a-z ]{0,30}?\s*\(we used [\d½¼¾⅓⅔⅛]`)
	pluralRe       = regexp.MustCompile(`(?:^|[^\d/⁄.])((?:[2-9]|\d{2,})(?:\s*[½¼¾⅓⅔⅛])?|1\s*[½¼¾⅓⅔⅛])\s+(cup|clove|can|package|thumb|bunch|slice)\b`)
	markupRe       = regexp.MustCompile(`<[a-z/][^>]*>|\w\*|\bTBSP\b|&nbsp;|&amp;`)
	runTogetherRe  = regexp.MustCompile(`\d-\d{3,}[½¼¾⅓⅔⅛]|\d{2,}[½¼¾⅓⅔⅛]-inch`)
	// relativeBefore are words that already say how much of an ingredient.
	relativeBefore = []string{"half the ", "half of the ", "remaining ", "rest of the ", "the rest of ", "all the ", "more ", "some ", "as many ", "as much "}
)

// alwaysOptional are ingredients cards often list without naming in a step.
var alwaysOptional = map[string]bool{"salt": true, "pepper": true, "black pepper": true, "kosher salt": true}

// noteApplies mirrors boxNotes: whether a note for n servings is the size
// being cooked (and so rightly kept).
func noteApplies(in Instructions, n int) bool {
	base := 0
	for _, s := range in.ServingOptions {
		if base == 0 || s < base {
			base = s
		}
	}
	if base > 0 && base < n {
		return 2*in.Servings >= base+n
	}
	return in.Servings >= n
}

func startsWithAmount(s string) bool {
	for _, r := range s {
		return (r >= '0' && r <= '9') || strings.ContainsRune(fractionRunes, r)
	}
	return false
}

// CheckSteps returns what reads wrong in a rendered recipe, in step order.
func CheckSteps(in Instructions) []Finding {
	var out []Finding
	named := map[int]bool{}
	leftOut := map[int]bool{}
	for _, ing := range in.Ingredients {
		if ing.LeftOut != nil {
			leftOut[ing.Index] = true
		}
	}
	for _, st := range in.Steps {
		text := st.Text
		for _, m := range boxNoteLeftRe.FindAllStringSubmatch(text, -1) {
			if n, err := strconv.Atoi(m[2]); err == nil && !noteApplies(in, n) && !strings.Contains(strings.ToLower(m[1]), "we used") {
				out = append(out, Finding{Code: FindingBoxNote, Step: st.Index, Detail: m[0]})
			}
		}
		for _, m := range doubleAmountRe.FindAllString(text, -1) {
			out = append(out, Finding{Code: FindingDoubleAmount, Step: st.Index, Detail: m})
		}
		for _, m := range pluralRe.FindAllStringSubmatch(text, -1) {
			out = append(out, Finding{Code: FindingUnitPlural, Step: st.Index, Detail: strings.TrimSpace(m[1] + " " + m[2])})
		}
		for _, m := range markupRe.FindAllString(text, -1) {
			out = append(out, Finding{Code: FindingMarkup, Step: st.Index, Detail: m})
		}
		for _, m := range runTogetherRe.FindAllString(text, -1) {
			out = append(out, Finding{Code: FindingRunTogether, Step: st.Index, Detail: m})
		}
		for i, seg := range st.Segments {
			if seg.Kind != SegmentIngredient || seg.Ingredient == NoIngredient {
				continue
			}
			named[seg.Ingredient] = true
			if leftOut[seg.Ingredient] && !seg.LeftOut {
				out = append(out, Finding{Code: FindingLeftOutNotStruck, Step: st.Index, Detail: seg.Text})
			}
			// Only an amount written into the text: a share kept for the
			// checklist ("half the onion") reads as the card wrote it.
			if seg.Amount != nil && startsWithAmount(seg.Text) && i > 0 && st.Segments[i-1].Kind == SegmentText {
				before := strings.ToLower(st.Segments[i-1].Text)
				for _, w := range relativeBefore {
					if strings.HasSuffix(before, w) {
						out = append(out, Finding{Code: FindingAmountBeforeRelative, Step: st.Index, Detail: strings.TrimSpace(w) + " " + seg.Text})
						break
					}
				}
			}
		}
	}
	for _, ing := range in.Ingredients {
		// A heading ("For the sauce:") names no ingredient.
		if !named[ing.Index] && !alwaysOptional[strings.ToLower(trimNameTail(ing.Name))] && ing.Name != "" && !ingredientHeading(ing.Name) {
			out = append(out, Finding{Code: FindingUnusedIngredient, Detail: ing.Name})
		}
	}
	if len(in.Checklist.All) > 0 {
		if g := in.Checklist.ByStep; len(g) == 0 || g[0].Index != 0 || len(g[0].Items) < len(in.Checklist.All) {
			out = append(out, Finding{Code: FindingHaveReadyMissing})
		}
	}
	for _, g := range in.Checklist.ByStep {
		for _, it := range g.Items {
			if strings.Contains(it.Prep, "the rest") {
				out = append(out, Finding{Code: FindingRestWithoutAmount, Step: g.Index, Detail: it.Name})
			}
			if ingredients.SpoonMeasured(it.Name) && weightText(it.AmountText) {
				out = append(out, Finding{Code: FindingSpoonedByWeight, Step: g.Index, Detail: it.AmountText + " " + it.Name})
			}
		}
	}
	for _, ing := range in.Ingredients {
		if ing.Amount != nil && ingredients.SpoonMeasured(ing.Name) && weightText(ing.Amount.Text()) {
			out = append(out, Finding{Code: FindingSpoonedByWeight, Detail: ing.Amount.Text() + " " + ing.Name})
		}
	}
	return out
}

// weightText reports an amount written as a weight: "1 ½ oz", "40 g", "1 lb".
func weightText(text string) bool {
	for _, u := range []string{" oz", " g", " lb", " kg"} {
		if strings.HasSuffix(text, u) {
			return true
		}
	}
	return false
}
