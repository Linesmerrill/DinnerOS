// Package ratings owns household members' ratings of recipes: a 1–5 score, an
// optional short comment, and tags from a fixed allowlist.
//
// Each member has at most one rating per recipe in a household; rating again
// replaces it. Ratings are personal, so any member who can view the household
// (household.view) may rate, change, or remove their own rating, and see the
// ratings of other members. The recipes module reads aggregates through
// Service.Summaries to show householdRating and myRating on recipes.
//
// Every change records a recipe.rated or recipe.unrated event (package events)
// on a best-effort basis.
package ratings
