package pantry

import (
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
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
	// months is the short end of life, and longest the long end: a best-by
	// date for a bag with air in it, and for a vacuum-sealed one.
	months, longest int
}{
	{[]string{"ground"}, "3–4 months", 3, 4},
	{[]string{"burger"}, "3–4 months", 3, 4},
	{[]string{"bacon"}, "1 month", 1, 1},
	{[]string{"sausage"}, "1–2 months", 1, 2},
	{[]string{"chorizo"}, "1–2 months", 1, 2},
	{[]string{"hot dog"}, "1–2 months", 1, 2},
	{[]string{"ham"}, "1–2 months", 1, 2},
	{[]string{"whole", "chicken"}, "1 year", 12, 12},
	{[]string{"whole", "turkey"}, "1 year", 12, 12},
	{[]string{"salmon"}, "2–3 months", 2, 3},
	{[]string{"tuna"}, "2–3 months", 2, 3},
	{[]string{"mackerel"}, "2–3 months", 2, 3},
	{[]string{"trout"}, "2–3 months", 2, 3},
	{[]string{"shrimp"}, "3–6 months", 3, 6},
	{[]string{"scallop"}, "3–6 months", 3, 6},
	{[]string{"squid"}, "3–6 months", 3, 6},
	{[]string{"crab"}, "3–6 months", 3, 6},
	{[]string{"lobster"}, "3–6 months", 3, 6},
	{[]string{"cod"}, "6–8 months", 6, 8},
	{[]string{"tilapia"}, "6–8 months", 6, 8},
	{[]string{"halibut"}, "6–8 months", 6, 8},
	{[]string{"haddock"}, "6–8 months", 6, 8},
	{[]string{"pollock"}, "6–8 months", 6, 8},
	{[]string{"mahi"}, "6–8 months", 6, 8},
	{[]string{"fish"}, "6–8 months", 6, 8},
	{[]string{"chicken"}, "9 months", 9, 9},
	{[]string{"turkey"}, "9 months", 9, 9},
	{[]string{"duck"}, "9 months", 9, 9},
	{[]string{"steak"}, "4–12 months", 4, 12},
	{[]string{"chop"}, "4–12 months", 4, 12},
	{[]string{"roast"}, "4–12 months", 4, 12},
	{[]string{"loin"}, "4–12 months", 4, 12},
	{[]string{"brisket"}, "4–12 months", 4, 12},
	{[]string{"rib"}, "4–12 months", 4, 12},
	{[]string{"pork"}, "4–12 months", 4, 12},
	{[]string{"beef"}, "4–12 months", 4, 12},
	{[]string{"lamb"}, "4–12 months", 4, 12},
	{[]string{"veal"}, "4–12 months", 4, 12},
}

// FreezerLife is how long raw meat or seafood keeps its best quality in the
// freezer ("3–4 months"), or "" when it isn't meat or the name doesn't say
// which kind.
func FreezerLife(name, category string) string {
	life, _, _ := freezerLife(name, category)
	return life
}

// BestBy is the best-by date for a label: the date it was frozen plus the
// short end of its freezer life, or "" when the kind of meat is unknown.
func BestBy(frozenOn, name, category string) string {
	return BestByWrapped(frozenOn, name, category, households.FreezerWrapBag)
}

// BestByWrapped is BestBy for raw meat frozen the household's way
// (households.Household.Wrap): vacuum sealed keeps no air on it, so it gets
// the long end of the FDA's range; a freezer bag or the store's package gets
// the short end.
func BestByWrapped(frozenOn, name, category, wrap string) string {
	_, months, longest := freezerLife(name, category)
	if wrap == households.FreezerWrapVacuum || wrap == "" {
		months = longest
	}
	t, err := time.Parse(DateLayout, frozenOn)
	if months == 0 || err != nil {
		return ""
	}
	return t.AddDate(0, months, 0).Format(DateLayout)
}

func freezerLife(name, category string) (string, int, int) {
	if category != ingredients.CategoryMeatSeafood {
		return "", 0, 0
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
			return f.life, f.months, f.longest
		}
	}
	return "", 0, 0
}
