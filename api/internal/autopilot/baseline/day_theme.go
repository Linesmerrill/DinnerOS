package baseline

import (
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// A household's weekdays have habits: tacos most Tuesdays, pasta most
// Mondays, Italian most Fridays. Some are a dish whatever the cuisine
// (Korean tacos are tacos), some a cuisine whatever the dish. For each day
// the planned history is read both ways, and the one that explains the day
// better becomes its theme (decision 637).

// Day theme thresholds: enough meals on the day to say anything, the theme
// on enough of them, and on at least half.
const (
	themeMinDayMeals = 4
	themeMinMeals    = 3
	themeMinShare    = 0.5
)

type dayTheme struct {
	// dish or cuisine is set, not both.
	dish, cuisine string
	share         float64
}

func (t *dayTheme) matches(it *item) bool {
	if t.dish != "" {
		return slices.Contains(it.dishTypes, t.dish)
	}
	return slices.Contains(it.cuisines, t.cuisine)
}

// text is "Usually tacos on Tuesdays" or "Usually Italian on Mondays".
func (t *dayTheme) text(day autopilot.Day) string {
	if t.dish != "" {
		return "Usually " + dishPlural(t.dish) + " on " + day.Plural()
	}
	return "Usually " + displayName(t.cuisine) + " on " + day.Plural()
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
}

func newThemeCounts() *themeCounts {
	c := &themeCounts{}
	for i := range 7 {
		c.dishes[i], c.cuisines[i] = map[string]int{}, map[string]int{}
	}
	return c
}

// add counts one planned meal on day.
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
		dish, dishN := top(c.dishes[day])
		cuisine, cuisineN := top(c.cuisines[day])
		// The dish wins a tie: "tacos" says more than "Mexican".
		switch {
		case dishN >= themeMinMeals && dishN >= cuisineN && float64(dishN)/float64(n) >= themeMinShare:
			out[day] = &dayTheme{dish: dish, share: float64(dishN) / float64(n)}
		case cuisineN >= themeMinMeals && float64(cuisineN)/float64(n) >= themeMinShare:
			out[day] = &dayTheme{cuisine: cuisine, share: float64(cuisineN) / float64(n)}
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
