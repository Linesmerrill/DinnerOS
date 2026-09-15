package households

import (
	"context"
	"time"
)

// Store persists households and memberships.
//
// Implementations return ErrNotFound for missing records (including malformed
// IDs) and ErrDuplicate (wrapped) when a unique constraint is violated.
type Store interface {
	// CreateHousehold inserts h and returns it with ID set. The household's
	// admin count starts at 1: it is always created together with its first
	// admin membership.
	CreateHousehold(ctx context.Context, h Household) (Household, error)
	// DeleteHousehold removes a household. It is used only to clean up after
	// a failed creation.
	DeleteHousehold(ctx context.Context, id string) error
	// GetHousehold returns the household with the given ID.
	GetHousehold(ctx context.Context, id string) (Household, error)
	// ListHouseholds returns the households with the given IDs, in any order.
	// Missing IDs are skipped.
	ListHouseholds(ctx context.Context, ids []string) ([]Household, error)
	// UpdateHousehold applies patch, sets updatedAt, and returns the result.
	UpdateHousehold(ctx context.Context, id string, patch HouseholdPatch, at time.Time) (Household, error)

	// DecrementAdminCount atomically lowers the household's admin count when
	// it is above 1, and returns ErrLastAdmin otherwise. Callers decrement
	// before demoting or removing an admin, and increment again if that
	// change then fails.
	DecrementAdminCount(ctx context.Context, householdID string) error
	// IncrementAdminCount raises the household's admin count by one.
	IncrementAdminCount(ctx context.Context, householdID string) error

	// CreateMembership inserts m and returns it with ID set. A second
	// membership for the same (household, user) fails with ErrDuplicate.
	CreateMembership(ctx context.Context, m Membership) (Membership, error)
	// GetMembership returns the user's membership in the household.
	GetMembership(ctx context.Context, householdID, userID string) (Membership, error)
	// ListMembershipsByHousehold returns a household's memberships, oldest first.
	ListMembershipsByHousehold(ctx context.Context, householdID string) ([]Membership, error)
	// ListMembershipsByUser returns a user's memberships, oldest first.
	ListMembershipsByUser(ctx context.Context, userID string) ([]Membership, error)
	// UpdateMembershipRole changes the role from `from` to `to` only if the
	// membership still has role `from`; otherwise it returns ErrNotFound.
	UpdateMembershipRole(ctx context.Context, householdID, userID string, from, to Role, at time.Time) (Membership, error)
	// DeleteMembership removes the membership only if it still has role;
	// otherwise it returns ErrNotFound.
	DeleteMembership(ctx context.Context, householdID, userID string, role Role) error
}
