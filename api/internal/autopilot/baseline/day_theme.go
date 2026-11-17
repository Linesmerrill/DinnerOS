package baseline

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// A household's weekdays have habits: tacos most Tuesdays, pasta most
// Mondays, Italian most Fridays. Some are a dish whatever the cuisine
// (Korean tacos are tacos), some a cuisine whatever the dish. For each day
// the planned and cooked history is read both ways, and the one that
// explains the day better becomes its theme (decision 637).
//
// Cuisines are also graded: every cuisine seen on the day at least twice
// gets its share of the day's meals, so Mexican on most Tuesdays lifts a
// Mexican meal on Tuesday even when tacos are the day's theme, and a looser
// lean (2 of 5) still counts a little (decision 724).

// Day theme thresholds: enough meals on the day to say anything, the theme
// on enough of them, and on at least half. A graded cuisine share needs the
// same meals on the day and the cuisine on at least themeMinGradedMeals.
const (
	themeMinDayMeals    = 4
	themeMinMeals       = 3
	themeMinShare       = 0.5
	themeMinGradedMeals = 2
)

type dayTheme struct {
	// dish or cuisine is the day's clear theme, at most one of them, or
	// neither when the day only has graded cuisines.
	dish, cuisine string
	share         float64
	// cuisines is each cuisine's share of the day's meals, for those seen
	// at least themeMinGradedMeals times.
	cuisines map[string]float64
	// strong are the graded cuisines that also meet the clear theme
	// thresholds, so they can say "Usually Mexican on Tuesdays".
	strong map[string]bool
}

// value is how strongly the day leans on the meal: the dish theme's share
// when the meal is that dish, or its best graded cuisine share, whichever is
// more. The text names what earned it, or is "" for a loose lean.
func (t *dayTheme) value(it *item, day autopilot.Day) (float64, string) {
	var best float64
	var text string
	if t.dish != "" && slices.Contains(it.dishTypes, t.dish) {
		best, text = t.share, "Usually "+dishPlural(t.dish)+" on "+day.Plural()
	}
	bestCuisine, cuisineShare := "", 0.0
	for _, cu := range it.cuisines {
		v := t.cuisines[cu]
		if v > cuisineShare || v > 0 && v == cuisineShare && cu < bestCuisine {
			bestCuisine, cuisineShare = cu, v
		}
	}
	if cuisineShare > best {
		best, text = cuisineShare, ""
		if t.strong[bestCuisine] {
			text = "Usually " + displayName(bestCuisine) + " on " + day.Plural()
		}
	}
	return best, text
}

// dishPlural is a dish as said of several nights: "tacos", "curries",
// "pasta".
func dishPlural(d string) string {
	switch {
	case d == "pasta" || d == "chili" || d == "risotto" || d == "meatloaf" || d == "pizza":
		return d
	case d == "stir-fry":
		return "stir-fries"
	case strings.HasSuffix(d, "ch") || strings.HasSuffix(d, "sh"):
		return d + "es"
	case strings.HasSuffix(d, "y") && !strings.HasSuffix(d, "ey"):
		return strings.TrimSuffix(d, "y") + "ies"
	}
	return d + "s"
}

type themeCounts struct {
	meals    [7]int
	dishes   [7]map[string]int
	cuisines [7]map[string]int
	// seen keeps a meal planned and then cooked on the same day of the same
	// week from counting twice.
	seen map[string]bool
}

func newThemeCounts() *themeCounts {
	c := &themeCounts{seen: map[string]bool{}}
	for i := range 7 {
		c.dishes[i], c.cuisines[i] = map[string]int{}, map[string]int{}
	}
	return c
}

// addOnce counts a planned or cooked meal on day of week, once per meal,
// week, and day.
func (c *themeCounts) addOnce(week string, day int, it *item) {
	key := week + "|" + strconv.Itoa(day) + "|" + it.id
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.add(day, it)
}

// add counts one meal on day.
func (c *themeCounts) add(day int, it *item) {
	c.meals[day]++
	for _, d := range it.dishTypes {
		c.dishes[day][d]++
	}
	for _, cu := range it.cuisines {
		c.cuisines[day][cu]++
	}
}

func (c *themeCounts) themes() [7]*dayTheme {
	var out [7]*dayTheme
	for day := range 7 {
		n := c.meals[day]
		if n < themeMinDayMeals {
			continue
		}
		t := &dayTheme{cuisines: map[string]float64{}, strong: map[string]bool{}}
		for cu, k := range c.cuisines[day] {
			if k >= themeMinGradedMeals {
				t.cuisines[cu] = float64(k) / float64(n)
				t.strong[cu] = k >= themeMinMeals && float64(k)/float64(n) >= themeMinShare
			}
		}
		dish, dishN := top(c.dishes[day])
		cuisine, cuisineN := top(c.cuisines[day])
		// The dish wins a tie: "tacos" says more than "Mexican".
		switch {
		case dishN >= themeMinMeals && dishN >= cuisineN && float64(dishN)/float64(n) >= themeMinShare:
			t.dish, t.share = dish, float64(dishN)/float64(n)
		case cuisineN >= themeMinMeals && float64(cuisineN)/float64(n) >= themeMinShare:
			t.cuisine, t.share = cuisine, float64(cuisineN)/float64(n)
		}
		if t.dish != "" || t.cuisine != "" || len(t.cuisines) > 0 {
			out[day] = t
		}
	}
	return out
}

// top is the most counted value, the alphabetically first on a tie.
func top(counts map[string]int) (string, int) {
	best, bestN := "", 0
	for v, n := range counts {
		if n > bestN || n == bestN && v < best {
			best, bestN = v, n
		}
	}
	return best, bestN
}
