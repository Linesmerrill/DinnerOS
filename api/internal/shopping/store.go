package shopping

import (
	"context"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// Store persists shopping settings, saved products, and handoffs. Every
// method is scoped by householdID. Missing records are ErrNotFound; unique
// key violations are ErrDuplicate (wrapped).
type Store interface {
	// GetSettings returns the household's settings, or ErrNotFound when they
	// were never set.
	GetSettings(ctx context.Context, householdID string) (Settings, error)
	// PutSettings creates or replaces the household's settings.
	PutSettings(ctx context.Context, s Settings) (Settings, error)

	// ListPreferences returns the household's saved products for a provider,
	// ordered by ingredient name.
	ListPreferences(ctx context.Context, householdID string, provider providers.Key) ([]Preference, error)
	// GetPreference returns the saved product for one ingredient.
	GetPreference(ctx context.Context, householdID string, provider providers.Key, ingredientKey string) (Preference, error)
	// CountPreferences counts the household's saved products for a provider.
	CountPreferences(ctx context.Context, householdID string, provider providers.Key) (int, error)
	// UpsertPreference saves p by (householdId, provider, ingredientKey),
	// keeping an existing one's ID, CreatedBy, and CreatedAt, and reports
	// whether it was created.
	UpsertPreference(ctx context.Context, p Preference) (Preference, bool, error)
	// DeletePreference removes the saved product for one ingredient.
	DeletePreference(ctx context.Context, householdID string, provider providers.Key, ingredientKey string) error

	// InsertHandoff stores h, assigning its ID, as the week's active handoff
	// for its provider at revision 1. Another active one for the same week and
	// provider is ErrDuplicate.
	InsertHandoff(ctx context.Context, h Handoff) (Handoff, error)
	// ActiveHandoff returns the household's active handoff for a week and
	// provider, or ErrNotFound.
	ActiveHandoff(ctx context.Context, householdID, week string, provider providers.Key) (Handoff, error)
	// UpdateHandoffSend replaces h's lines, exclusions, links, and store, marks
	// it active, and bumps its revision, provided it is still at revision.
	// Otherwise it returns ErrConflict. Activating it beside another active
	// handoff is ErrDuplicate.
	UpdateHandoffSend(ctx context.Context, h Handoff, revision int64) (Handoff, error)
	// RemovePendingLines removes an active handoff's pending lines for one
	// ingredient and reports whether any were removed.
	RemovePendingLines(ctx context.Context, householdID, handoffID, ingredientKey string, at time.Time) (bool, error)
	// CloseHandoff makes a handoff inactive with a reason, unless it is
	// already closed.
	CloseHandoff(ctx context.Context, householdID, handoffID string, reason CloseReason, at time.Time) error
	// CloseWeekHandoffs closes every handoff of the week that isn't closed,
	// for every provider, and returns how many it closed.
	CloseWeekHandoffs(ctx context.Context, householdID, week string, reason CloseReason, at time.Time) (int, error)
	// ReopenHandoff makes a closed handoff active again. Another active one
	// for the same week and provider is ErrDuplicate.
	ReopenHandoff(ctx context.Context, householdID, handoffID string, at time.Time) error
	// GetHandoff returns one of the household's handoffs.
	GetHandoff(ctx context.Context, householdID, id string) (Handoff, error)
	// ListHandoffs returns the household's handoffs, newest first.
	ListHandoffs(ctx context.Context, householdID string, f HandoffFilter) ([]Handoff, error)
	// ClaimLine reserves a line that isn't confirmed for the caller at now,
	// unless another claim newer than staleBefore holds it. It reports
	// whether the claim was taken.
	ClaimLine(ctx context.Context, householdID, handoffID, lineID string, now, staleBefore time.Time) (bool, error)
	// ReleaseLine drops a line's claim.
	ReleaseLine(ctx context.Context, householdID, handoffID, lineID string) error
	// ConfirmLine marks a line confirmed with its purchase, and drops its
	// claim.
	ConfirmLine(ctx context.Context, householdID, handoffID string, line HandoffLine, at time.Time) error
	// SkipLine marks a pending line skipped and reports whether it was
	// pending.
	SkipLine(ctx context.Context, householdID, handoffID, lineID, userID string, at time.Time) (bool, error)

	// GetOrderedWeek returns the household's ordered marker for the week, or
	// ErrNotFound when the week isn't marked.
	GetOrderedWeek(ctx context.Context, householdID, week string) (OrderedWeek, error)
	// MarkWeekOrdered records that a week's groceries were ordered. Marking a
	// week that is already marked keeps the first marker, so a double tap
	// doesn't rewrite who ordered and when.
	MarkWeekOrdered(ctx context.Context, w OrderedWeek) (OrderedWeek, error)
	// UnmarkWeekOrdered removes the week's marker, or returns ErrNotFound.
	UnmarkWeekOrdered(ctx context.Context, householdID, week string) error

	// ListStoreRequests returns the household's store requests, newest first.
	ListStoreRequests(ctx context.Context, householdID string) ([]StoreRequest, error)
	// GetStoreRequestByKey returns the household's request for one store.
	GetStoreRequestByKey(ctx context.Context, householdID, key string) (StoreRequest, error)
	// CountStoreRequests counts the stores the household has asked for.
	CountStoreRequests(ctx context.Context, householdID string) (int, error)
	// CountStoreRequestsByKey counts requests per store key across every
	// household: DinnerOS's own demand signal.
	CountStoreRequestsByKey(ctx context.Context) (map[string]int, error)
	// UpsertStoreRequest saves r by (householdId, key), keeping an existing
	// one's ID, RequestedBy, and RequestedAt, and reports whether it was
	// created.
	UpsertStoreRequest(ctx context.Context, r StoreRequest) (StoreRequest, bool, error)
	// DeleteStoreRequest removes one of the household's store requests.
	DeleteStoreRequest(ctx context.Context, householdID, id string) error
}
