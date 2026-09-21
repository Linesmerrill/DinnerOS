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
	// SearchIngredients returns at most limit catalog ingredients whose Key
	// matches keyPattern, a case-sensitive regular expression, ordered by Key.
	SearchIngredients(ctx context.Context, keyPattern string, limit int) ([]Ingredient, error)

	// FindRecipesBySourceIDs returns the household's recipes from source whose
	// SourceRecipeID or any SourceAlias is in ids, ordered by ID.
	FindRecipesBySourceIDs(ctx context.Context, householdID, source string, ids []string) ([]Recipe, error)
	// SaveRecipes inserts recipes with an empty ID and replaces the rest, in
	// as few round trips as practical.
	SaveRecipes(ctx context.Context, householdID string, recipes []Recipe) error
	// GetRecipe returns one of the household's recipes.
	GetRecipe(ctx context.Context, householdID, id string) (Recipe, error)
	// GetRecipes returns the household's recipes with the given IDs, ordered
	// by ID, skipping IDs that are malformed, missing, or another household's.
	GetRecipes(ctx context.Context, householdID string, ids []string) ([]Recipe, error)
	// ListRecipesAfter returns up to limit of the household's full recipes
	// whose ID sorts after afterID (from the first when afterID is empty),
	// ordered by ID, for paging through a whole household.
	ListRecipesAfter(ctx context.Context, householdID, afterID string, limit int) ([]Recipe, error)
	// LibraryCatalogKeys returns, for each of keys the household already has a
	// recipe for, that recipe's ID. Keys the household does not have are
	// absent from the map.
	LibraryCatalogKeys(ctx context.Context, householdID string, keys []string) (map[string]string, error)
	// SetRecipeShared sets a recipe's catalog opt-in and UpdatedAt. It returns
	// the stored recipe as it now is.
	SetRecipeShared(ctx context.Context, householdID, id string, shared bool, now time.Time) (Recipe, error)
	// ExistingRecipeIDs returns those of ids that are the household's
	// recipes, in any order. Malformed IDs are skipped.
	ExistingRecipeIDs(ctx context.Context, householdID string, ids []string) ([]string, error)
	// ListRecipes returns summaries matching f in f.Sort order.
	ListRecipes(ctx context.Context, householdID string, f ListFilter) ([]RecipeSummary, error)
	// ListCatalog returns up to limit of the household's recipes ordered by
	// ID, without steps, nutrition, or descriptions. Ingredient lines are
	// included (without categories).
	ListCatalog(ctx context.Context, householdID string, limit int) ([]Recipe, error)
	// ListMenuCatalog returns up to limit of the household's recipes ordered
	// by ID for menu cards: everything but steps, descriptions, source
	// identity, and ingredient amounts. Ingredient lines carry only names.
	ListMenuCatalog(ctx context.Context, householdID string, limit int) ([]Recipe, error)
	// FindIngredientUse returns one IngredientUse per household recipe that
	// has a line with any of ingredientIDs, ordered by recipe ID. Each use
	// lists which of those IDs the recipe has, sorted and without repeats.
	// Malformed IDs are skipped.
	FindIngredientUse(ctx context.Context, householdID string, ingredientIDs []string) ([]IngredientUse, error)

	// SaveReviewItems records review items. Items already recorded for the
	// household (same reviewKey) are left unchanged.
	SaveReviewItems(ctx context.Context, householdID string, items []ReviewItem, now time.Time) error
	// ListReviewItems returns up to limit of the household's review items
	// with the given status, oldest first. An empty status returns every
	// item. Records carry no RecipeID; the service resolves it.
	ListReviewItems(ctx context.Context, householdID, status string, limit int) ([]ReviewRecord, error)
}
