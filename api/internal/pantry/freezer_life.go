package pantry

import (
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// freezerLives are the FDA's freezer times for raw meat and seafood kept at
// 0°F (foodsafety.gov, Cold Food Storage Chart). Frozen food stays safe
// indefinitely; these are how long it keeps its best quality. The first
// entry whose words all appear in the name wins, so the specific cuts come
// before the animal.
var freezerLives = []struct {
	words []string
	life  string
	// months is the short end of life, for a best-by date on the label.
	months int
}{
	{[]string{"ground"}, "3–4 months", 3},
	{[]string{"burger"}, "3–4 months", 3},
	{[]string{"bacon"}, "1 month", 1},
	{[]string{"sausage"}, "1–2 months", 1},
	{[]string{"chorizo"}, "1–2 months", 1},
	{[]string{"hot dog"}, "1–2 months", 1},
	{[]string{"ham"}, "1–2 months", 1},
	{[]string{"whole", "chicken"}, "1 year", 12},
	{[]string{"whole", "turkey"}, "1 year", 12},
	{[]string{"salmon"}, "2–3 months", 2},
	{[]string{"tuna"}, "2–3 months", 2},
	{[]string{"mackerel"}, "2–3 months", 2},
	{[]string{"trout"}, "2–3 months", 2},
	{[]string{"shrimp"}, "3–6 months", 3},
	{[]string{"scallop"}, "3–6 months", 3},
	{[]string{"squid"}, "3–6 months", 3},
	{[]string{"crab"}, "3–6 months", 3},
	{[]string{"lobster"}, "3–6 months", 3},
	{[]string{"cod"}, "6–8 months", 6},
	{[]string{"tilapia"}, "6–8 months", 6},
	{[]string{"halibut"}, "6–8 months", 6},
	{[]string{"haddock"}, "6–8 months", 6},
	{[]string{"pollock"}, "6–8 months", 6},
	{[]string{"mahi"}, "6–8 months", 6},
	{[]string{"fish"}, "6–8 months", 6},
	{[]string{"chicken"}, "9 months", 9},
	{[]string{"turkey"}, "9 months", 9},
	{[]string{"duck"}, "9 months", 9},
	{[]string{"steak"}, "4–12 months", 4},
	{[]string{"chop"}, "4–12 months", 4},
	{[]string{"roast"}, "4–12 months", 4},
	{[]string{"loin"}, "4–12 months", 4},
	{[]string{"brisket"}, "4–12 months", 4},
	{[]string{"rib"}, "4–12 months", 4},
	{[]string{"pork"}, "4–12 months", 4},
	{[]string{"beef"}, "4–12 months", 4},
	{[]string{"lamb"}, "4–12 months", 4},
	{[]string{"veal"}, "4–12 months", 4},
}

// FreezerLife is how long raw meat or seafood keeps its best quality in the
// freezer ("3–4 months"), or "" when it isn't meat or the name doesn't say
// which kind.
func FreezerLife(name, category string) string {
	life, _ := freezerLife(name, category)
	return life
}

// BestBy is the best-by date for a label: the date it was frozen plus the
// short end of its freezer life, or "" when the kind of meat is unknown.
func BestBy(frozenOn, name, category string) string {
	_, months := freezerLife(name, category)
	t, err := time.Parse(DateLayout, frozenOn)
	if months == 0 || err != nil {
		return ""
	}
	return t.AddDate(0, months, 0).Format(DateLayout)
}

func freezerLife(name, category string) (string, int) {
	if category != ingredients.CategoryMeatSeafood {
		return "", 0
	}
	lower := " " + strings.ToLower(name) + " "
	for _, f := range freezerLives {
		match := true
		for _, w := range f.words {
			if !strings.Contains(lower, w) {
				match = false
				break
			}
		}
		if match {
			return f.life, f.months
		}
	}
	return "", 0
}
