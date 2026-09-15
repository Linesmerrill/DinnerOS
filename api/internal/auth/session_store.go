package auth

import (
	"context"
	"errors"
	"time"
)

// ErrSessionNotFound is returned by SessionStore when no session matches.
var ErrSessionNotFound = errors.New("auth: session not found")

// Session is one refresh token. Sessions descended from the same sign-in share
// a FamilyID; only the newest session in a family is neither rotated nor
// revoked.
type Session struct {
	ID         string
	UserID     string
	FamilyID   string
	TokenHash  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastUsedAt time.Time
	RotatedAt  *time.Time
	RevokedAt  *time.Time
}

// SessionStore persists refresh-token sessions.
type SessionStore interface {
	// CreateSession inserts s and returns it with ID set.
	CreateSession(ctx context.Context, s Session) (Session, error)
	// FindSessionByTokenHash returns the session for a token hash, or
	// ErrSessionNotFound.
	FindSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	// MarkRotated atomically sets rotatedAt and lastUsedAt, but only if the
	// session is neither rotated nor revoked. It reports whether it did.
	MarkRotated(ctx context.Context, id string, at time.Time) (bool, error)
	// RevokeFamily sets revokedAt on every not-yet-revoked session in a family.
	RevokeFamily(ctx context.Context, familyID string, at time.Time) error
}
