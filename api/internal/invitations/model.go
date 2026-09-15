// Package invitations owns HouseholdInvitation: hashed tokens and invite
// codes, expiry, revocation, acceptance, and the EmailProvider seam.
//
// An invitation carries two secrets. The token (32 random bytes) goes only
// into the emailed link. The code (10 characters) is shown to the inviting
// admin once and printed in the email, so it can be shared by other means.
// Only SHA-256 hashes of both are stored.
//
// Accepting does not require the signed-in user's email to match the invited
// address: Sign in with Apple may hide the real address behind a private relay
// address, and people often sign in with a different account than the one
// that received the email. Possession of the secret is the proof.
package invitations

import (
	"errors"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// TTL is how long an invitation stays valid.
const TTL = 7 * 24 * time.Hour

// Errors returned by the store and service.
var (
	// ErrNotFound means the invitation does not exist or is not in a state
	// the operation applies to.
	ErrNotFound = errors.New("invitations: not found")
	// ErrDuplicate means a unique constraint was violated.
	ErrDuplicate = errors.New("invitations: duplicate")
	// ErrInvalid means a presented token or code does not identify a usable
	// invitation: unknown, expired, revoked, or already used. Callers are
	// never told which.
	ErrInvalid = errors.New("invitations: invitation is invalid")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalidInput(msg string) error { return &ValidationError{Message: msg} }

// Invitation invites an email address to join a household with a role.
// Zero AcceptedAt/RevokedAt mean "not yet".
type Invitation struct {
	ID          string
	HouseholdID string
	// Email is normalized (trimmed, lowercase).
	Email      string
	Role       households.Role
	TokenHash  string
	CodeHash   string
	ExpiresAt  time.Time
	AcceptedAt time.Time
	AcceptedBy string
	RevokedAt  time.Time
	CreatedBy  string
	CreatedAt  time.Time
}

// Pending reports whether the invitation can still be accepted at now.
func (i Invitation) Pending(now time.Time) bool {
	return i.unusableReason(now) == ""
}

// unusableReason explains, for logs only, why the invitation cannot be
// accepted. It is empty for a pending invitation.
func (i Invitation) unusableReason(now time.Time) string {
	switch {
	case !i.RevokedAt.IsZero():
		return "revoked"
	case !i.AcceptedAt.IsZero():
		return "already accepted"
	case !now.Before(i.ExpiresAt):
		return "expired"
	}
	return ""
}
