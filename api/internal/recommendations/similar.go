package recommendations

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// "Try something similar" re-picks one planned meal. The candidates come from
// the same machinery as an Autopilot swap — the baseline provider ranks the
// day with the rest of the week fixed, so restrictions, never-include rules,
// variety, and the cook-time mix are honored exactly once — and this file
// only reorders that ranking towards meals that resemble the one being
// replaced, and says why each one resembles it.
//
// Similarity is a weighted sum of five facets of the recipe attributes
// Autopilot already derives (AttributesOf), each scored 0..1:
//
//	protein   0.30  how much the protein sets overlap (Jaccard)
//	cuisine   0.25  1 for a shared cuisine, 0.5 for a shared region only
//	category  0.20  1 for a shared meal category (pasta, curry, tacos)
//	tags      0.15  how much the tag sets overlap (Jaccard)
//	time      0.10  1 for the same cook-time band, less as the minutes differ
//
// A facet neither recipe says anything about is unknown rather than
// different, and scores neutralFacet, so a thin library isn't punished for
// missing data.
const (
	weightProtein  = 0.30
	weightCuisine  = 0.25
	weightCategory = 0.20
	weightTags     = 0.15
	weightTime     = 0.10

	// neutralFacet is what a facet neither recipe describes scores.
	neutralFacet = 0.5
	// MinSimilarity is how alike a candidate must be to be offered as
	// "something similar". Below it, it is only offered when nothing better
	// exists, with the notSimilar message.
	MinSimilarity = 0.35
	// similarityShare is how much of the ordering is similarity; the rest is
	// the provider's own fit for the day.
	similarityShare = 0.65
	// timeSpreadMinutes is the cook-time difference at which two meals in
	// different bands stop resembling each other at all.
	timeSpreadMinutes = 45
)

// Similarity is how much a candidate resembles the meal being replaced, with
// the facets that matched.
type Similarity struct {
	// Score is 0..1; MinSimilarity and above reads as "similar".
	Score float64
	// Reasons name the facets that matched, strongest first ("Also Thai",
	// "30 min").
	Reasons []Reason
}

// Similarity reason codes.
const (
	ReasonSimilarProtein  = "similarProtein"
	ReasonSimilarCuisine  = "similarCuisine"
	ReasonSimilarCategory = "similarCategory"
	ReasonSimilarTag      = "similarTag"
	ReasonSimilarTime     = "similarTime"
)

// similarity compares a candidate with the meal being replaced. Both sets of
// attributes come from AttributesOf, so overrides are already applied.
func similarity(target, candidate RecipeAttributes) Similarity {
	var out Similarity
	score := 0.0

	protein, sharedProteins := overlap(target.Proteins, candidate.Proteins)
	score += weightProtein * protein
	cuisine, sharedCuisine, viaRegion := cuisineSimilarity(target, candidate)
	score += weightCuisine * cuisine
	category, sharedCategory := categorySimilarity(target, candidate)
	score += weightCategory * category
	tags, sharedTags := overlap(target.Tags, candidate.Tags)
	score += weightTags * tags
	score += weightTime * timeSimilarity(target, candidate)
	out.Score = score

	if sharedCuisine != "" {
		label := cuisineLabel(sharedCuisine)
		if label == "" {
			label = titleCase(sharedCuisine)
		}
		text := "Also " + label
		if viaRegion {
			text = "Also " + label
		}
		out.Reasons = append(out.Reasons, Reason{Code: ReasonSimilarCuisine, Text: text})
	}
	if sharedCategory != "" {
		out.Reasons = append(out.Reasons, Reason{Code: ReasonSimilarCategory, Text: "Also " + strings.ToLower(optionLabel(MealCategoryOptions, sharedCategory))})
	}
	if len(sharedProteins) > 0 {
		out.Reasons = append(out.Reasons, Reason{Code: ReasonSimilarProtein, Text: "Also " + strings.ToLower(optionLabel(ProteinOptions, sharedProteins[0]))})
	}
	if candidate.CookMinutes > 0 {
		out.Reasons = append(out.Reasons, Reason{Code: ReasonSimilarTime, Text: fmt.Sprintf("%d min", candidate.CookMinutes)})
	}
	if len(sharedTags) > 0 && len(out.Reasons) < maxSimilarReasons {
		out.Reasons = append(out.Reasons, Reason{Code: ReasonSimilarTag, Text: "Also " + sharedTags[0]})
	}
	if len(out.Reasons) > maxSimilarReasons {
		out.Reasons = out.Reasons[:maxSimilarReasons]
	}
	return out
}

// maxSimilarReasons bounds the reasons shown under a candidate.
const maxSimilarReasons = 3

// overlap is the Jaccard overlap of two value sets, with the values both
// share, and neutralFacet when neither has any.
func overlap(a, b []string) (float64, []string) {
	if len(a) == 0 && len(b) == 0 {
		return neutralFacet, nil
	}
	if len(a) == 0 || len(b) == 0 {
		return 0, nil
	}
	var shared []string
	for _, v := range a {
		if slices.Contains(b, v) && !slices.Contains(shared, v) {
			shared = append(shared, v)
		}
	}
	union := len(a)
	for _, v := range b {
		if !slices.Contains(a, v) {
			union++
		}
	}
	return float64(len(shared)) / float64(union), shared
}

// cuisineSimilarity scores a shared cuisine, or half a point for a shared
// broader region ("italian" and "greek" are both southern european). It
// returns the shared value to explain and whether it is a region.
func cuisineSimilarity(target, candidate RecipeAttributes) (float64, string, bool) {
	if len(target.Cuisines) == 0 && len(candidate.Cuisines) == 0 {
		return neutralFacet, "", false
	}
	for _, c := range target.Cuisines {
		if slices.Contains(candidate.Cuisines, c) {
			return 1, c, false
		}
	}
	for _, r := range append(slices.Clone(target.Cuisines), target.CuisineRegions...) {
		if slices.Contains(candidate.CuisineRegions, r) || slices.Contains(candidate.Cuisines, r) {
			return 0.5, r, true
		}
	}
	return 0, "", false
}

// categorySimilarity scores a shared meal category (two pastas, two curries).
func categorySimilarity(target, candidate RecipeAttributes) (float64, string) {
	mine, theirs := suitedCategories(target), suitedCategories(candidate)
	if len(mine) == 0 && len(theirs) == 0 {
		return neutralFacet, ""
	}
	for _, c := range mine {
		if slices.Contains(theirs, c) {
			return 1, c
		}
	}
	return 0, ""
}

// suitedCategories are the meal categories a recipe is in.
func suitedCategories(a RecipeAttributes) []string {
	var out []string
	for _, c := range a.MealCategories {
		if c.Suits {
			out = append(out, c.Category)
		}
	}
	return out
}

// timeSimilarity scores how close two cook times are: the same band is a full
// point, and otherwise the minutes are compared. An unknown time is neutral.
func timeSimilarity(target, candidate RecipeAttributes) float64 {
	if target.CookMinutes == 0 || candidate.CookMinutes == 0 {
		if target.TimeBand != "" && target.TimeBand == candidate.TimeBand {
			return 1
		}
		return neutralFacet
	}
	if target.TimeBand != "" && target.TimeBand == candidate.TimeBand {
		return 1
	}
	diff := target.CookMinutes - candidate.CookMinutes
	if diff < 0 {
		diff = -diff
	}
	if diff >= timeSpreadMinutes {
		return 0
	}
	return 1 - float64(diff)/timeSpreadMinutes
}

// titleCase capitalizes each word of a canonical value ("north american").
func titleCase(v string) string {
	words := strings.Fields(v)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// rankFit turns a candidate's position in the provider's ranking into a 0..1
// fit, so the provider's unbounded scores can be mixed with similarity.
func rankFit(i, n int) float64 {
	if n <= 1 {
		return 1
	}
	return 1 - float64(i)/float64(n-1)
}

// similarAttributes are a recipe's attributes with its meal categories filled
// in, which plain attributes() leaves to Service.RecipeAttributes.
func similarAttributes(r recipes.Recipe, override *RecipeOverride, bands autopilot.TimeBands) RecipeAttributes {
	a := attributes(r, override, bands)
	for _, c := range recipeMealCategories(r, override) {
		a.MealCategories = append(a.MealCategories, MealCategoryAttribute{Category: c, Suits: true, Source: SourceHeuristic})
	}
	return a
}
