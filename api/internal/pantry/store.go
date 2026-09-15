package pantry

import (
	"context"
	"time"
)

// Store persists pantry items.
//
// Every method is scoped by householdID; implementations filter on it and
// never trust a HouseholdID on values passed in except in InsertItem and
// UpdateItem, where it is the scope. Missing records are ErrNotFound, and a
// second item with the same (householdId, key) is ErrDuplicate (wrapped).
// Returned items are in no particular order.
type Store interface {
	// ListItems returns the household's items matching f.
	ListItems(ctx context.Context, householdID string, f ListFilter) ([]Item, error)
	// GetItem returns one item. A malformed ID is ErrNotFound.
	GetItem(ctx context.Context, householdID, id string) (Item, error)
	// GetItems returns the items with the given IDs, skipping missing and
	// malformed IDs.
	GetItems(ctx context.Context, householdID string, ids []string) ([]Item, error)
	// FindItemsByKeys returns the household's items whose Key is any of keys.
	FindItemsByKeys(ctx context.Context, householdID string, keys []string) ([]Item, error)
	// CountItems returns how many items the household has.
	CountItems(ctx context.Context, householdID string) (int, error)
	// InsertItem stores a new item, assigning its ID and Version 1.
	InsertItem(ctx context.Context, item Item) (Item, error)
	// UpdateItem replaces the item with item.ID, but only if its stored
	// Version still equals item.Version, and returns it with the next
	// Version. A missing item is ErrNotFound; a newer stored version is
	// ErrConflict. Key and CreatedAt are never changed; every other field,
	// including Tracking, History, and Rate, is replaced.
	UpdateItem(ctx context.Context, item Item) (Item, error)
	// DeleteItem removes one item.
	DeleteItem(ctx context.Context, householdID, id string) error
	// SetStatus sets the status of the items with the given IDs (missing and
	// malformed IDs are ignored) as set by a person (StatusSource person,
	// StatusSetAt at), bumping each Version. Marking items out clears their
	// quantity and unit but keeps Tracking.
	SetStatus(ctx context.Context, householdID string, ids []string, status Status, updatedBy string, at time.Time) error
}
