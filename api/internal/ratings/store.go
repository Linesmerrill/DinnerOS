package ratings

import "context"

// Store persists ratings. Every method is scoped by householdID.
// Implementations return nil rather than empty slices.
type Store interface {
	// Upsert saves r as the user's rating of the recipe. An existing rating
	// keeps its ID and CreatedAt; score, comment, tags, and UpdatedAt are
	// replaced. It returns the saved rating and the rating it replaced, if any.
	Upsert(ctx context.Context, r Rating) (saved Rating, previous *Rating, err error)
	// Delete removes the user's rating of the recipe and returns it, or
	// ErrNotFound when there is none.
	Delete(ctx context.Context, householdID, recipeID, userID string) (Rating, error)
	// ListForRecipe returns every rating of the recipe, most recently updated
	// first.
	ListForRecipe(ctx context.Context, householdID, recipeID string) ([]Rating, error)
	// Summaries returns the count and score sum of each recipe's ratings,
	// with userID's own rating as Mine. Recipes without ratings are absent.
	Summaries(ctx context.Context, householdID, userID string, recipeIDs []string) (map[string]Summary, error)
}
