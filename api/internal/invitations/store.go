package invitations

import (
	"context"
	"time"
)

// Store persists invitations.
//
// Implementations return ErrNotFound for missing records (including malformed
// IDs) and ErrDuplicate (wrapped) when a unique constraint is violated. At most
// one invitation per (household, email) may be pending (neither accepted nor
// revoked, regardless of expiry); a second fails with ErrDuplicate.
type Store interface {
	// Create inserts inv and returns it with ID set.
	Create(ctx context.Context, inv Invitation) (Invitation, error)
	// Get returns an invitation of the given household.
	Get(ctx context.Context, householdID, id string) (Invitation, error)
	// FindByTokenHash returns the invitation whose token hashes to hash.
	FindByTokenHash(ctx context.Context, hash string) (Invitation, error)
	// FindByCodeHash returns the invitation whose code hashes to hash.
	FindByCodeHash(ctx context.Context, hash string) (Invitation, error)
	// ListPending returns the household's unaccepted, unrevoked invitations
	// that have not expired at now, newest first.
	ListPending(ctx context.Context, householdID string, now time.Time) ([]Invitation, error)
	// RevokePending revokes every unaccepted, unrevoked invitation (expired
	// or not) for the household and email.
	RevokePending(ctx context.Context, householdID, email string, at time.Time) error
	// Revoke revokes the invitation if it is neither accepted nor revoked.
	// Revoking an invitation in any other state is a no-op.
	Revoke(ctx context.Context, householdID, id string, at time.Time) error
	// MarkAccepted records userID's acceptance only if the invitation is
	// still pending and unexpired at `at`; otherwise it returns ErrNotFound.
	// This conditional update is what stops two acceptances from both
	// succeeding.
	MarkAccepted(ctx context.Context, id, userID string, at time.Time) error
	// UnmarkAccepted reverses MarkAccepted by userID, so the invitation can be
	// retried after joining the household failed.
	UnmarkAccepted(ctx context.Context, id, userID string) error
}
