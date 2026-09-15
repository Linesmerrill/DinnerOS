package recommendations

import "context"

// Store persists profiles, week contexts, proposals, and recipe overrides.
//
// Every method is scoped by household. Saves use optimistic concurrency: a
// value with Version 0 is inserted (ErrConflict when one already exists), and
// any other value replaces the stored one only while its version still equals
// Version (ErrConflict otherwise). A save stores and returns Version+1.
type Store interface {
	// GetProfile returns the household's profile, or ErrNotFound.
	GetProfile(ctx context.Context, householdID string) (Profile, error)
	SaveProfile(ctx context.Context, p Profile) (Profile, error)

	// GetWeekContext returns a week's context, or ErrNotFound.
	GetWeekContext(ctx context.Context, householdID, week string) (WeekContext, error)
	SaveWeekContext(ctx context.Context, c WeekContext) (WeekContext, error)
	// DeleteWeekContext removes a week's context and returns it, or
	// ErrNotFound.
	DeleteWeekContext(ctx context.Context, householdID, week string) (WeekContext, error)

	// GetProposal returns the week's proposal, or ErrNotFound.
	GetProposal(ctx context.Context, householdID, week string) (Proposal, error)
	// SaveProposal writes the week's proposal. Generating again saves a new
	// proposal (new ID) over the old one with the old one's Version.
	SaveProposal(ctx context.Context, p Proposal) (Proposal, error)

	// ListOverrides returns the household's recipe overrides, ordered by
	// recipe ID.
	ListOverrides(ctx context.Context, householdID string) ([]RecipeOverride, error)
	// GetOverride returns a recipe's override, or ErrNotFound.
	GetOverride(ctx context.Context, householdID, recipeID string) (RecipeOverride, error)
	// SaveOverride upserts an override; one with no methods is deleted.
	SaveOverride(ctx context.Context, o RecipeOverride) error
}
