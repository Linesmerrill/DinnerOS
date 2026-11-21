package catalog

import (
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Fingerprints (fingerprint.go) group copies of one recipe exactly, but a
// meal kit re-releases a dinner under a new ID and a new link, and two
// households can type the same dish, so a row could still show "Brown Sugar
// Bourbon Chicken" twice side by side. Rows (and their See All) drop near
// copies too (decision 733): two recipes are near copies when their titles
// match once case, punctuation, and filler words ("recipe", "easy",
// "homemade") are set aside, and they also come from the same site or use
// mostly the same main ingredients. The one ranked higher stays. Search and
// the full list still show both, since two households' brownies with
// different steps are different recipes (decision 697); a row is a short
// pick, where two of one dinner look like a mistake.

// titleFiller are words a title can carry or drop without being another
// dish.
var titleFiller = []string{"a", "an", "the", "recipe", "recipes", "easy", "homemade", "simple", "best"}

// nearTitle is a title's comparison form: lowercase words, "&" read as
// "and", without punctuation or filler words, or "" for a title with
// nothing else.
func nearTitle(name string) string {
	var out []string
	for _, w := range strings.Fields(words(strings.ReplaceAll(strings.ReplaceAll(name, "&", " and "), "'", ""))) {
		if !slices.Contains(titleFiller, w) {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

// mainIngredientsOverlap is the share of main ingredients two copies have
// in common, out of all they have between them, for a near copy.
const mainIngredientsOverlap = 0.6

// nearKey is what near copies compare, computed once per recipe.
type nearKey struct {
	title string
	// site is the link's host, or for a recipe with no link a source that
	// isn't a household's (import, provider, partner); "" for neither.
	site string
	// main are the normalized names of the ingredients that aren't pantry
	// staples.
	main []string
}

func nearKeyOf(c recipes.Recipe) nearKey {
	k := nearKey{title: nearTitle(c.Name)}
	if u := NormalizeSourceURL(c.SourceURL); u != "" {
		host, _, _ := strings.Cut(u, "/")
		k.site = "link:" + host
	} else if s := strings.ToLower(strings.TrimSpace(c.Source)); s != "" && s != recipes.SourceManual && s != recipes.SourceUser {
		k.site = "source:" + s
	}
	for _, line := range c.Ingredients {
		if line.PantryStaple {
			continue
		}
		if n := ingredients.NormalizeName(line.Name); n != "" && !slices.Contains(k.main, n) {
			k.main = append(k.main, n)
		}
	}
	return k
}

// nearCopies reports whether two recipes are near copies.
func nearCopies(a, b nearKey) bool {
	if a.title == "" || a.title != b.title {
		return false
	}
	if a.site != "" && a.site == b.site {
		return true
	}
	if len(a.main) == 0 || len(b.main) == 0 {
		// Nothing to tell them apart but the title.
		return len(a.main) == len(b.main)
	}
	shared := 0
	for _, n := range a.main {
		if slices.Contains(b.main, n) {
			shared++
		}
	}
	return float64(shared) >= mainIngredientsOverlap*float64(len(a.main)+len(b.main)-shared)
}

// nearIndex finds near copies among recipes already kept.
type nearIndex map[string][]nearKey

func (x nearIndex) has(k nearKey) bool {
	return slices.ContainsFunc(x[k.title], func(o nearKey) bool { return nearCopies(k, o) })
}

func (x nearIndex) add(k nearKey) { x[k.title] = append(x[k.title], k) }

// withoutNearCopies keeps the first of each set of near copies, in order.
// Callers pass ranked items, so the best-ranked copy stays.
func withoutNearCopies(items []Result) []Result {
	seen := nearIndex{}
	out := items[:0:0]
	for _, it := range items {
		k := nearKeyOf(it.Content)
		if seen.has(k) {
			continue
		}
		seen.add(k)
		out = append(out, it)
	}
	return out
}

// freshOnly is a row's items for the top of Shared Recipes, given what
// earlier rows showed: only the ones no earlier row showed, so a recipe
// appears in the first row it qualifies for and Top Rated never repeats
// Chef’s Picks (decisions 733, 764). A row left with nothing new is left out;
// See All still lists everything.
func freshOnly(items []Result, shown nearIndex) []Result {
	var fresh []Result
	for _, it := range items {
		if !shown.has(nearKeyOf(it.Content)) {
			fresh = append(fresh, it)
		}
	}
	return fresh
}
