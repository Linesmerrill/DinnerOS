package recipes

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Swapping an ingredient in a recipe the household edits (decision 667):
// "Ramen Noodles" becomes "Lo Mein Noodles", so every step that names the
// ramen should name the lo mein, and the step that boils it should give lo
// mein's time, not ramen's. SwapSteps does both on the steps as the member
// has them in the editor, and nothing is stored until they save.

// SwapInput is one ingredient renamed in the editor.
type SwapInput struct {
	OldName string
	NewName string
	Steps   []string
	// Minutes, when set, is the cook time the member picked: it replaces the
	// time in each step that cooks the new ingredient.
	Minutes int
}

// SwapResult is the steps with the new name, and what's known about its
// cook time.
type SwapResult struct {
	Steps []string
	// Changed marks the steps that read differently than they came in.
	Changed []bool
	// CookTime is how long the new ingredient usually cooks, when known.
	CookTime *ingredients.CookTime
	// Times are the cook times the steps give the new ingredient, in step
	// order: what a picked Minutes replaces.
	Times []StepTime
}

// StepTime is a cook time a step gives an ingredient.
type StepTime struct {
	// Step is the step's position, from 0.
	Step int
	// Text is the time as the step writes it ("3 minutes", "2-3 min").
	Text string
	// Minutes is the most it says ("2-3 minutes" is 3).
	Minutes int
}

// Swap limits: generous for a real recipe.
const (
	maxSwapName  = 200
	maxSwapSteps = 100
	maxStepText  = 4000
	maxMinutes   = 240
)

// SwapSteps renames an ingredient through steps and finds its cook times.
func SwapSteps(in SwapInput) (SwapResult, error) {
	oldName, newName := strings.TrimSpace(in.OldName), strings.TrimSpace(in.NewName)
	switch {
	case oldName == "" || newName == "":
		return SwapResult{}, invalidEdit("both names are required")
	case utf8.RuneCountInString(oldName) > maxSwapName || utf8.RuneCountInString(newName) > maxSwapName:
		return SwapResult{}, invalidEdit("names must be at most 200 characters")
	case len(in.Steps) > maxSwapSteps:
		return SwapResult{}, invalidEdit("a recipe may have at most 100 steps")
	case in.Minutes < 0 || in.Minutes > maxMinutes:
		return SwapResult{}, invalidEdit("minutes must be between 1 and 240")
	}
	for _, s := range in.Steps {
		if len(s) > maxStepText {
			return SwapResult{}, invalidEdit("each step must be at most 4000 characters")
		}
	}
	rename := renamer(oldName, newName)
	res := SwapResult{Steps: make([]string, len(in.Steps)), Changed: make([]bool, len(in.Steps))}
	newForms := nameForms(newName, nil)
	// The first time the steps give the new ingredient is the one that cooks
	// it ("boil until tender, 3 minutes"); a later one finishes the dish
	// ("toss with the sauce, 1 minute more") and keeps its own time.
	picked := false
	for i, step := range in.Steps {
		text := rename(step)
		spots := cookTimesIn(text, newForms)
		for j := len(spots) - 1; j >= 0; j-- {
			spot := spots[j]
			if in.Minutes > 0 && !picked && j == 0 && len(res.Times) == 0 {
				text = text[:spot.start] + minutesText(in.Minutes) + text[spot.end:]
				spot.text, spot.minutes = minutesText(in.Minutes), in.Minutes
				picked = true
			}
			spots[j] = spot
		}
		for _, spot := range spots {
			res.Times = append(res.Times, StepTime{Step: i, Text: spot.text, Minutes: spot.minutes})
		}
		res.Steps[i], res.Changed[i] = text, text != step
	}
	if ct, ok := ingredients.CookTimeFor(newName); ok {
		res.CookTime = &ct
	}
	return res, nil
}

// cookTimeSentence is "Lo mein noodles usually cook 4 to 6 minutes."
func cookTimeSentence(name, time string) string {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	if lower == "" {
		return ""
	}
	first, size := utf8.DecodeRuneInString(lower)
	return string(unicode.ToUpper(first)) + lower[size:] + " usually cook " + time + "."
}

func minutesText(n int) string {
	if n == 1 {
		return "1 minute"
	}
	return strconv.Itoa(n) + " minutes"
}

// renamer returns a function that writes newName wherever a step names
// oldName: the whole name in any of the forms a step uses, then the words
// that set the old one apart ("Ramen Noodles" → "Lo Mein Noodles" makes a
// bare "ramen" "lo mein"). A match keeps the step's capitalization.
func renamer(oldName, newName string) func(string) string {
	type pair struct{ from, to string }
	var pairs []pair
	seen := map[string]bool{}
	add := func(from, to string) {
		key := strings.ToLower(from)
		if len(from) < 3 || seen[key] || strings.EqualFold(from, to) {
			return
		}
		seen[key] = true
		pairs = append(pairs, pair{from, to})
	}
	add(oldName, newName)
	add(pluralize(oldName), pluralize(newName))
	add(singularize(oldName), singularize(newName))
	if from, to := distinctWords(oldName, newName); from != "" && to != "" {
		add(from, to)
	}
	for _, form := range nameForms(oldName, nil) {
		// "noodles" is a shorter way to say both: the step is right as it is,
		// and renaming it again would say "lo mein lo mein noodles".
		if containsWords(newName, form) {
			continue
		}
		switch {
		case strings.EqualFold(form, singularize(form)) && !strings.EqualFold(form, pluralize(form)):
			add(form, singularize(newName))
		default:
			add(form, newName)
		}
	}
	if len(pairs) == 0 {
		return func(s string) string { return s }
	}
	slices.SortStableFunc(pairs, func(a, b pair) int { return len(b.from) - len(a.from) })
	alts := make([]string, len(pairs))
	to := map[string]string{}
	for i, p := range pairs {
		alts[i] = regexp.QuoteMeta(p.from)
		to[strings.ToLower(p.from)] = p.to
	}
	re := regexp.MustCompile(`(?i)\b(?:` + strings.Join(alts, "|") + `)\b`)
	return func(s string) string {
		return re.ReplaceAllStringFunc(s, func(m string) string {
			return swapCase(m, to[strings.ToLower(m)])
		})
	}
}

// containsWords reports whether name has form's words in a row.
func containsWords(name, form string) bool {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(form) + `\b`)
	return re.MatchString(name)
}

// distinctWords drops the words two names end with in common: "Ramen
// Noodles" and "Lo Mein Noodles" differ in "Ramen" and "Lo Mein".
func distinctWords(oldName, newName string) (string, string) {
	a, b := strings.Fields(oldName), strings.Fields(newName)
	for len(a) > 1 && len(b) > 1 && strings.EqualFold(a[len(a)-1], b[len(b)-1]) {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	if len(a) == len(strings.Fields(oldName)) {
		return "", ""
	}
	return strings.Join(a, " "), strings.Join(b, " ")
}

// swapCase writes to the way the step wrote match: lower case for "ramen
// noodles", the name's own capitals for "Ramen Noodles", and a capital first
// letter at the start of a sentence.
func swapCase(match, to string) string {
	r, _ := utf8.DecodeRuneInString(match)
	if unicode.IsLower(r) {
		return strings.ToLower(to)
	}
	if strings.ToLower(match[1:]) == match[1:] && strings.Contains(match, " ") {
		// "Ramen noodles" at a sentence start.
		lower := strings.ToLower(to)
		first, size := utf8.DecodeRuneInString(lower)
		return string(unicode.ToUpper(first)) + lower[size:]
	}
	return to
}

// timeSpot is a cook time in a step, by byte position.
type timeSpot struct {
	start, end int
	text       string
	minutes    int
}

var (
	stepMinutes = regexp.MustCompile(`(?i)\b(\d{1,3})(?:\s*(?:-|–|to)\s*(\d{1,3}))?\s*(?:minutes?|mins?)\b`)
	sentenceEnd = regexp.MustCompile(`[.!?](?:\s|$)`)
)

// cookTimesIn finds, in each sentence of step that names the ingredient (any
// of forms), the first cook time after the name. "Add noodles to the pot and
// cook until tender, 3 minutes" gives "3 minutes"; a time in another sentence
// belongs to something else.
func cookTimesIn(step string, forms []string) []timeSpot {
	if len(forms) == 0 {
		return nil
	}
	alts := make([]string, len(forms))
	for i, f := range forms {
		alts[i] = regexp.QuoteMeta(f)
	}
	name := regexp.MustCompile(`(?i)\b(?:` + strings.Join(alts, "|") + `)\b`)
	var out []timeSpot
	start := 0
	for _, end := range append(sentenceEnd.FindAllStringIndex(step, -1), []int{len(step), len(step)}) {
		sentence := step[start:end[0]]
		if loc := name.FindStringIndex(sentence); loc != nil {
			if m := stepMinutes.FindStringSubmatchIndex(sentence[loc[1]:]); m != nil {
				offset := start + loc[1]
				minutes, _ := strconv.Atoi(sentence[loc[1]+m[2] : loc[1]+m[3]])
				if m[4] >= 0 {
					minutes, _ = strconv.Atoi(sentence[loc[1]+m[4] : loc[1]+m[5]])
				}
				out = append(out, timeSpot{
					start: offset + m[0], end: offset + m[1], text: sentence[loc[1]+m[0] : loc[1]+m[1]], minutes: minutes,
				})
			}
		}
		start = end[1]
		if start >= len(step) {
			break
		}
	}
	return out
}
