package recipes

import (
	"context"
	"time"
)

// Store persists recipes, the ingredient catalog, and import review items.
//
// Every recipe and review method is scoped by householdID; implementations
// filter on it and never trust a HouseholdID field on the values passed in.
// Implementations return ErrNotFound for missing records and ErrDuplicate
// (wrapped) when a unique constraint is violated. Slices in returned values
// are nil rather than empty.
type Store interface {
	// FindIngredients returns catalog ingredients that carry any of refs or
	// whose Key is any of keys.
	FindIngredients(ctx context.Context, refs []SourceRef, keys []string) ([]Ingredient, error)
	// GetIngredients returns the catalog ingredients with the given IDs,
	// skipping IDs that do not exist.
	GetIngredients(ctx context.Context, ids []string) ([]Ingredient, error)
	// UpsertIngredients inserts each ingredient by Key or, when the key
	// exists, adds its SourceRefs and sets UpdatedAt. Name, category, and
	// image are only written on insert. It returns how many were inserted.
	UpsertIngredients(ctx context.Context, ingredients []Ingredient) (inserted int, err error)

	// FindRecipesBySourceIDs returns the household's recipes from source whose
	// SourceRecipeID or any SourceAlias is in ids, ordered by ID.
	FindRecipesBySourceIDs(ctx context.Context, householdID, source string, ids []string) ([]Recipe, error)
	// SaveRecipes inserts recipes with an empty ID and replaces the rest, in
	// as few round trips as practical.
	SaveRecipes(ctx context.Context, householdID string, recipes []Recipe) error
	// GetRecipe returns one of the household's recipes.
	GetRecipe(ctx context.Context, householdID, id string) (Recipe, error)
	// ListRecipes returns summaries matching f in f.Sort order.
	ListRecipes(ctx context.Context, householdID string, f ListFilter) ([]RecipeSummary, error)

	// SaveReviewItems records review items. Items already recorded for the
	// household (same reviewKey) are left unchanged.
	SaveReviewItems(ctx context.Context, householdID string, items []ReviewItem, now time.Time) error
}
