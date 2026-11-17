package companions

import (
	"context"
	"time"
)

// Store persists companions. Every method is scoped by householdID; values
// passed in carry their own HouseholdID only where it is the scope. Missing
// records are ErrNotFound.
type Store interface {
	// List returns the household's companions, oldest first. Hidden ones
	// (deleted companions made from add-on recipes) only with hidden.
	List(ctx context.Context, householdID string, hidden bool) ([]Companion, error)
	// Get returns one visible companion.
	Get(ctx context.Context, householdID, id string) (Companion, error)
	// Insert stores a new companion with a new ID.
	Insert(ctx context.Context, c Companion) (Companion, error)
	// EnsureLinked stores c unless the household already has a companion,
	// hidden or not, linked to c.LinkedRecipe. It reports whether it stored
	// one. Two callers racing store one.
	EnsureLinked(ctx context.Context, c Companion) (bool, error)
	// Update replaces what a member edits on a visible companion (not its
	// link, history or creation) and returns it.
	Update(ctx context.Context, c Companion) (Companion, error)
	// Hide hides a visible companion.
	Hide(ctx context.Context, householdID, id string, at time.Time) error
	// Delete removes a companion.
	Delete(ctx context.Context, householdID, id string) error
	// MarkListed records that the companion went on week's list at at,
	// unless it is already recorded for a later week.
	MarkListed(ctx context.Context, householdID, id, week string, at time.Time) error
}
