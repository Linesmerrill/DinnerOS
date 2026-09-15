package planning

import (
	"context"
	"time"
)

// Store persists week plans.
//
// Every method is scoped by householdID. Entry writes are single atomic
// updates of the plan document, so concurrent edits by different members of
// the same week never overwrite each other. Writes to entries fail with
// ErrFinalized when the plan is finalized. Returned slices are nil rather than
// empty.
type Store interface {
	// GetPlan returns the household's plan for week, or ErrNotFound.
	GetPlan(ctx context.Context, householdID string, week Week) (Plan, error)
	// ListSummaries returns summaries of the stored plans from..to inclusive,
	// in week order. Weeks without a stored plan are omitted.
	ListSummaries(ctx context.Context, householdID string, from, to Week) ([]Summary, error)
	// AddEntry appends e, creating the plan as a draft if needed. The store
	// assigns e.ID and returns it. It fails with ErrPlanFull when the plan
	// already has maxEntries entries.
	AddEntry(ctx context.Context, householdID string, week Week, e Entry, maxEntries int, now time.Time) (Plan, string, error)
	// AddEntries appends entries in one atomic update, creating the plan as a
	// draft if needed. It fails with ErrPlanFull, adding nothing, when the
	// plan can't take all of them within maxEntries. The store assigns IDs
	// and returns them in the order of entries.
	AddEntries(ctx context.Context, householdID string, week Week, entries []Entry, maxEntries int, now time.Time) (Plan, []string, error)
	// ListPlans returns the stored plans from..to inclusive, in week order.
	// Weeks without a stored plan are omitted.
	ListPlans(ctx context.Context, householdID string, from, to Week) ([]Plan, error)
	// EarliestWeek returns the earliest week whose stored plan has at least
	// one entry. ok is false when there is none.
	EarliestWeek(ctx context.Context, householdID string) (week Week, ok bool, err error)
	// UpdateEntry applies changes to one entry. ErrNotFound when the plan or
	// entry does not exist.
	UpdateEntry(ctx context.Context, householdID string, week Week, entryID string, changes EntryChanges, now time.Time) (Plan, error)
	// DeleteEntry removes one entry. ErrNotFound when the plan or entry does
	// not exist.
	DeleteEntry(ctx context.Context, householdID string, week Week, entryID string, now time.Time) (Plan, error)
	// SetStatus sets the plan's status, creating the plan if needed.
	SetStatus(ctx context.Context, householdID string, week Week, status Status, now time.Time) (Plan, error)
}
