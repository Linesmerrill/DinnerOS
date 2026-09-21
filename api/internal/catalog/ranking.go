package catalog

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// Discovery ranking weights. They are small integers on purpose: this is the
// same taste profile Autopilot plans a week from, read through the same
// recommendations.AttributesOf, so discovery agrees with the week it
// proposes instead of being a second, differently-tuned recommender.
//
// Discovery is not week planning, though, and the differences are deliberate.
// There is no variety, novelty, or repetition term, because nothing here is
// already in the household's library; there is no day, season, or calendar
// term, because a browse surface is not about one evening. What is left is
// the taste profile's hard restrictions as a filter and its likes and
// dislikes as a score.
const (
	likedCuisine    = 3.0
	likedRegion     = 1.5
	likedTag        = 2.0
	likedProtein    = 3.0
	dislikedPenalty = -4.0
	overTimePenalty = -1.5
	unknownTime     = -0.25
	maxReasons      = 3
)

// rank filters entries the household's restrictions rule out and orders the
// rest by taste. Ties break by name, then by ID, so the same household and
// the same catalog always produce the same page — including for a household
// with no profile at all, where every score is 0 and the order is
// alphabetical.
func rank(entries []Recipe, p recommendations.Profile, bands autopilot.TimeBands) []Result {
	out := make([]Result, 0, len(entries))
	for _, e := range entries {
		attrs := recommendations.AttributesOf(e.Content, nil, bands)
		if excluded(e, attrs, p.Restrictions) {
			continue
		}
		score, reasons := taste(attrs, p)
		out = append(out, Result{Recipe: e, Score: score, Reasons: reasons})
	}
	slices.SortStableFunc(out, func(a, b Result) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		if c := cmp.Compare(fold(a.Content.Name), fold(b.Content.Name)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

// excluded reports whether a hard restriction rules the recipe out. A
// restriction is never traded off against a like: Autopilot treats these as
// absolute, and a browse surface that offered a household the allergen it
// told us to avoid would be worse than one that offered nothing.
func excluded(e Recipe, a recommendations.RecipeAttributes, r recommendations.Restrictions) bool {
	if r.NoSpicy && a.Spicy {
		return true
	}
	for _, allergen := range r.Allergens {
		if containsFold(a.Allergens, allergen) {
			return true
		}
	}
	for _, diet := range r.Diets {
		if !containsFold(a.Diets, diet) {
			return true
		}
	}
	for _, c := range r.ExcludedCuisines {
		want := recommendations.CanonicalCuisine(c)
		if slices.Contains(a.Cuisines, want) || slices.Contains(a.CuisineRegions, want) {
			return true
		}
	}
	for _, t := range r.ExcludedTags {
		if slices.Contains(a.Tags, recommendations.CanonicalTag(t)) {
			return true
		}
	}
	for _, protein := range r.ExcludedProteins {
		if containsFold(a.Proteins, protein) {
			return true
		}
	}
	for _, name := range r.ExcludedIngredients {
		key := ingredients.NormalizeName(name)
		if key == "" {
			continue
		}
		for _, line := range e.Content.Ingredients {
			if ingredients.NormalizeName(line.Name) == key {
				return true
			}
		}
	}
	return false
}

// taste scores a recipe against the household's likes and dislikes and
// explains the top few reasons in the household's own words.
func taste(a recommendations.RecipeAttributes, p recommendations.Profile) (float64, []string) {
	var score float64
	type reason struct {
		weight float64
		text   string
	}
	var reasons []reason
	add := func(w float64, format string, args ...any) {
		score += w
		if w > 0 {
			reasons = append(reasons, reason{weight: w, text: fmt.Sprintf(format, args...)})
		}
	}

	for _, c := range p.Taste.Likes.Cuisines {
		want := recommendations.CanonicalCuisine(c)
		switch {
		case slices.Contains(a.Cuisines, want):
			add(likedCuisine, "You like %s", label(c))
		case slices.Contains(a.CuisineRegions, want):
			add(likedRegion, "Close to %s, which you like", label(c))
		}
	}
	for _, t := range p.Taste.Likes.Tags {
		if slices.Contains(a.Tags, recommendations.CanonicalTag(t)) {
			add(likedTag, "You like %s", label(t))
		}
	}
	for _, protein := range p.Taste.Likes.Proteins {
		if containsFold(a.Proteins, protein) {
			add(likedProtein, "You like %s", label(protein))
		}
	}
	for _, c := range p.Taste.Dislikes.Cuisines {
		if slices.Contains(a.Cuisines, recommendations.CanonicalCuisine(c)) {
			add(dislikedPenalty, "")
		}
	}
	for _, t := range p.Taste.Dislikes.Tags {
		if slices.Contains(a.Tags, recommendations.CanonicalTag(t)) {
			add(dislikedPenalty, "")
		}
	}
	for _, protein := range p.Taste.Dislikes.Proteins {
		if containsFold(a.Proteins, protein) {
			add(dislikedPenalty, "")
		}
	}

	switch limit := p.Schedule.WeeknightMaxMinutes; {
	case a.CookMinutes <= 0:
		// An unknown cook time is a mild penalty, not a filter: the recipe
		// may still be what the household wants, but it can't be promoted
		// over one we can actually vouch for.
		score += unknownTime
	case limit > 0 && a.CookMinutes > limit:
		score += overTimePenalty
	}

	slices.SortStableFunc(reasons, func(x, y reason) int { return cmp.Compare(y.weight, x.weight) })
	out := make([]string, 0, min(len(reasons), maxReasons))
	for _, r := range reasons {
		if len(out) == maxReasons {
			break
		}
		if !slices.Contains(out, r.text) {
			out = append(out, r.text)
		}
	}
	if len(out) == 0 {
		return score, nil
	}
	return score, out
}

// label turns a stored value back into something to show ("north american" →
// "North American", "chicken" → "chicken"). Known cuisines have a display
// label; anything else is shown as the household typed it.
func label(value string) string {
	if l := recommendations.CuisineLabel(recommendations.CanonicalCuisine(value)); l != "" {
		return l
	}
	return strings.TrimSpace(value)
}

func containsFold(values []string, want string) bool {
	want = fold(want)
	return slices.ContainsFunc(values, func(v string) bool { return fold(v) == want })
}
