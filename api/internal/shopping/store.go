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

	// InsertHandoff stores h, assigning its ID.
	InsertHandoff(ctx context.Context, h Handoff) (Handoff, error)
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
}
