package recipes

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// This file finds the cooking times in a rendered step ("2-3 minutes") and
// names each timer for what the sentence is cooking, the way you'd ask a
// speaker for one: "a timer for the pasta". The app draws the times and uses
// these names and start times as sent (decision 591).

// StepTimer is one cooking time in a step, in the order the text gives them.
type StepTimer struct {
	// Text is the time as written: "2-3 minutes".
	Text        string
	LowSeconds  int
	HighSeconds int
	// StartSeconds is where the timer starts: the high end of a range, so
	// nothing comes off early.
	StartSeconds int
	// Subject names the timer ("Pasta", "Ground Beef"), or "".
	Subject string
}

// "8-10 minutes", "8 to 10 min", "5 mins", "1½ hours", "30 seconds".
var durationRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?|\d*[½¼¾])\s*(?:(?:-|–|—|to)\s*(\d+(?:\.\d+)?|\d*[½¼¾]))?\s*(hours?|hrs?|minutes?|mins?|seconds?|secs?)\b`)

// stepTimers finds the times in text and names them from the step's
// ingredients (the names its segments stand for).
func stepTimers(text string, ingredients []string) []StepTimer {
	var out []StepTimer
	for _, m := range durationRe.FindAllStringSubmatchIndex(text, -1) {
		low, ok := durationNumber(text[m[2]:m[3]])
		if !ok {
			continue
		}
		high := low
		if m[4] >= 0 {
			if h, ok := durationNumber(text[m[4]:m[5]]); ok && h > low {
				high = h
			}
		}
		unit := strings.ToLower(text[m[6]:m[7]])
		scale := 60.0
		switch {
		case strings.HasPrefix(unit, "h"):
			scale = 3600
		case strings.HasPrefix(unit, "s"):
			scale = 1
		}
		lowSeconds, highSeconds := int(low*scale+0.5), int(high*scale+0.5)
		if lowSeconds <= 0 || highSeconds > 12*3600 {
			continue
		}
		out = append(out, StepTimer{
			Text: text[m[0]:m[1]], LowSeconds: lowSeconds, HighSeconds: highSeconds, StartSeconds: highSeconds,
			Subject: timerSubject(text[:m[0]], ingredients),
		})
	}
	return out
}

func durationNumber(s string) (float64, bool) {
	fractions := map[string]float64{"½": 0.5, "¼": 0.25, "¾": 0.75}
	for glyph, value := range fractions {
		if head, ok := strings.CutSuffix(s, glyph); ok {
			whole := 0.0
			if head != "" {
				n, err := strconv.ParseFloat(head, 64)
				if err != nil {
					return 0, false
				}
				whole = n
			}
			return whole + value, true
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	return n, err == nil
}

// timerNouns are things a step cooks that aren't always an ingredient the
// recipe lists ("the pasta" when the list says "Rigatoni").
var timerNouns = []string{
	"pasta", "rice", "tortillas", "water", "sauce", "potatoes", "vegetables", "veggies", "noodles",
	"chicken", "beef", "pork", "steak", "salmon", "fish", "shrimp", "bread", "croutons", "eggs",
	"onion", "onions", "broccoli", "carrots", "green beans", "peppers", "mushrooms", "couscous",
}

// timerSeasonings are added along the way; never what a timer is for.
var timerSeasonings = map[string]bool{
	"salt": true, "pepper": true, "black pepper": true, "kosher salt": true, "oil": true, "olive oil": true,
	"cooking oil": true, "vegetable oil": true, "water": true, "sugar": true, "garlic powder": true, "chili flakes": true,
}

var timerProteins = []string{"beef", "pork", "chicken", "turkey", "sausage", "lamb", "steak", "shrimp", "salmon"}

var meatRe = regexp.MustCompile(`\bmeat\b`)

// timerSubject is what a timer is for, from the text up to the time: what the
// sentence cooks ("meat" is the step's meat, "pasta"), else the last
// ingredient the sentence names that isn't a seasoning, else the step's last
// such one before the time.
func timerSubject(before string, ingredients []string) string {
	sentence := lastSentence(before)
	lower := strings.ToLower(sentence)
	var cooking []string
	for _, name := range ingredients {
		if !timerSeasonings[strings.ToLower(name)] {
			cooking = append(cooking, name)
		}
	}
	if meatRe.MatchString(lower) {
		for i := len(cooking) - 1; i >= 0; i-- {
			if slices.ContainsFunc(timerProteins, func(p string) bool { return strings.Contains(strings.ToLower(cooking[i]), p) }) {
				return titleWords(cooking[i])
			}
		}
		return "Meat"
	}
	if noun := timerNoun(sentence); noun != "" {
		return noun
	}
	if named := lastNamed(sentence, cooking); named != "" {
		return named
	}
	return lastNamed(before, cooking)
}

func lastSentence(text string) string {
	pieces := strings.FieldsFunc(text, func(r rune) bool { return r == '.' || r == ';' || r == '\n' })
	if len(pieces) == 0 {
		return text
	}
	return pieces[len(pieces)-1]
}

// timerNoun is the last of timerNouns the sentence names, as a whole word.
func timerNoun(sentence string) string {
	lower := strings.ToLower(sentence)
	best, at := "", -1
	for _, noun := range timerNouns {
		i := strings.LastIndex(lower, noun)
		if i < 0 {
			continue
		}
		startOK := i == 0 || !unicode.IsLetter(lastRune(lower[:i]))
		end := i + len(noun)
		endOK := end == len(lower) || !unicode.IsLetter(firstRune(lower[end:]))
		if startOK && endOK && i > at {
			best, at = noun, i
		}
	}
	if best == "" {
		return ""
	}
	return strings.ToUpper(best[:1]) + best[1:]
}

// lastNamed is the ingredient named last in text, by its name or any word of
// it longer than three letters.
func lastNamed(text string, names []string) string {
	lower := strings.ToLower(text)
	best, at := "", -1
	for _, name := range names {
		n := strings.ToLower(name)
		for _, form := range append([]string{n}, strings.Fields(n)...) {
			if len([]rune(form)) <= 3 {
				continue
			}
			if i := strings.LastIndex(lower, form); i > at {
				best, at = name, i
			}
		}
	}
	if best == "" {
		return ""
	}
	return titleWords(best)
}

func titleWords(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

func lastRune(s string) rune {
	r := []rune(s)
	return r[len(r)-1]
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}
