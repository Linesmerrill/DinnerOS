package substitutes

import (
	"context"
	"time"
)

// Store persists the curated specialty ingredients (global) and each
// household's options and choices. Household methods are scoped by
// householdID. Missing records are ErrNotFound; unique key violations are
// ErrDuplicate (wrapped).
type Store interface {
	// ListSpecialties returns the curated specialty ingredients ordered by
	// ID, without retired ones unless includeRetired.
	ListSpecialties(ctx context.Context, includeRetired bool) ([]Specialty, error)
	// GetSpecialty returns one by ID, retired or not.
	GetSpecialty(ctx context.Context, id string) (Specialty, error)
	// UpsertSpecialty inserts s by ID or replaces the stored one's content,
	// keeping its CreatedAt, and clears Retired.
	UpsertSpecialty(ctx context.Context, s Specialty) error
	// RetireSpecialties marks the specialties with ids retired.
	RetireSpecialties(ctx context.Context, ids []string, at time.Time) error

	// ListOptions returns the household's options, for one specialty or all
	// when specialtyID is "", oldest first.
	ListOptions(ctx context.Context, householdID, specialtyID string) ([]Option, error)
	// GetOption returns one of the household's options.
	GetOption(ctx context.Context, householdID, id string) (Option, error)
	// CountOptions counts the household's options for a specialty.
	CountOptions(ctx context.Context, householdID, specialtyID string) (int, error)
	// InsertOption stores o, assigning its ID.
	InsertOption(ctx context.Context, o Option) (Option, error)
	// ReplaceOption replaces the content of the household's option o.ID,
	// keeping its specialty, CreatedBy, and CreatedAt.
	ReplaceOption(ctx context.Context, o Option) (Option, error)
	// DeleteOption deletes one of the household's options.
	DeleteOption(ctx context.Context, householdID, id string) error

	// ListChoices returns the household's choices ordered by specialty ID.
	ListChoices(ctx context.Context, householdID string) ([]Choice, error)
	// PutChoice creates or replaces the household's choice for c.SpecialtyID.
	PutChoice(ctx context.Context, c Choice) (Choice, error)
	// InsertChoice stores c only when the household has no choice for its
	// specialty, and reports whether it did.
	InsertChoice(ctx context.Context, c Choice) (bool, error)
	// DeleteChoice removes the household's choice for a specialty; removing
	// one that doesn't exist is not an error.
	DeleteChoice(ctx context.Context, householdID, specialtyID string) error
	// DeleteChoicesForOption removes the household's choices of optionID.
	DeleteChoicesForOption(ctx context.Context, householdID, optionID string) error
}
