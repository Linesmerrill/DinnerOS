package users

import (
	"context"
	"time"
)

// Store persists users and their identities.
//
// Implementations return ErrNotFound for missing records and ErrDuplicate
// (wrapped) when a unique constraint is violated.
type Store interface {
	// CreateUser inserts u and returns it with ID set.
	CreateUser(ctx context.Context, u User) (User, error)
	// DeleteUser removes a user. It is used only to clean up a user created
	// during a lost sign-up race.
	DeleteUser(ctx context.Context, id string) error
	// GetUser returns the user with the given ID.
	GetUser(ctx context.Context, id string) (User, error)

	// CreateIdentity inserts identity and returns it with ID set. A second
	// identity with the same (provider, subject) fails with ErrDuplicate.
	CreateIdentity(ctx context.Context, identity AuthIdentity) (AuthIdentity, error)
	// FindIdentity returns the identity for (provider, subject).
	FindIdentity(ctx context.Context, provider Provider, subject string) (AuthIdentity, error)
	// TouchIdentity records a sign-in: it sets lastUsedAt and, when email is
	// non-empty, the verified email.
	TouchIdentity(ctx context.Context, id string, email string, at time.Time) error
	// ListIdentities returns every identity for a user, oldest first.
	ListIdentities(ctx context.Context, userID string) ([]AuthIdentity, error)
}
