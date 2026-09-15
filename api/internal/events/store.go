package events

import (
	"context"
	"time"
)

// Store appends and reads events. It never updates or deletes them.
type Store interface {
	// Insert stores events, assigning IDs. An event whose (householdId,
	// userId, clientEventId) is already stored is skipped and counted in
	// duplicates; the other events are still stored.
	Insert(ctx context.Context, events []Event) (inserted, duplicates int, err error)
	// List returns a household's events matching q, oldest first.
	List(ctx context.Context, q Query) ([]Event, error)
}

// Query selects a household's events.
type Query struct {
	HouseholdID string
	// RecipeID, when set, keeps only events about that recipe.
	RecipeID string
	// Types, when set, keeps only these types.
	Types []Type
	// Since, when set, keeps events that occurred at or after it.
	Since time.Time
	// Limit caps the result; 0 means DefaultListLimit.
	Limit int
}

// DefaultListLimit is the List limit when Query.Limit is 0.
const DefaultListLimit = 1000
