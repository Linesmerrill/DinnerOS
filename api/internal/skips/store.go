package skips

import "context"

// Store persists a household's skipped ingredients.
//
// Every method is scoped by householdID; implementations filter on it and
// never trust a HouseholdID on values passed in except in PutSkip, where it is
// the scope. Missing records are ErrNotFound, and a second skip for the same
// (householdId, ingredientKey) is ErrDuplicate (wrapped).
type Store interface {
	// ListSkips returns the household's skips, newest first.
	ListSkips(ctx context.Context, householdID string) ([]Skip, error)
	// CountSkips returns how many skips the household has.
	CountSkips(ctx context.Context, householdID string) (int, error)
	// PutSkip creates the skip for s.IngredientKey or replaces the stored
	// one's scope, week, and name, keeping its ID, CreatedBy, and CreatedAt.
	// created is false when it replaced an existing skip.
	PutSkip(ctx context.Context, s Skip) (stored Skip, created bool, err error)
	// DeleteSkip removes one skip. A malformed ID is ErrNotFound.
	DeleteSkip(ctx context.Context, householdID, id string) error
}
