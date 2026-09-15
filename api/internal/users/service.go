package users

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Service implements user and identity use cases.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService returns a Service backed by store.
func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

// FindOrCreateByIdentity returns the user that owns the verified identity,
// creating the user and identity on first sign-in. created reports whether a
// new user was created.
//
// Users are looked up only by (provider, subject). An existing account with
// the same email is deliberately not reused: email is not a stable identity
// and auto-merging would let one provider's account take over another's.
func (s *Service) FindOrCreateByIdentity(ctx context.Context, v VerifiedIdentity) (user User, created bool, err error) {
	if v.Provider == "" || strings.TrimSpace(v.Subject) == "" {
		return User{}, false, errors.New("users: identity provider and subject are required")
	}
	if !v.EmailVerified {
		v.Email = "" // never persist an email the provider did not verify
	}
	now := s.now().UTC()

	identity, err := s.store.FindIdentity(ctx, v.Provider, v.Subject)
	switch {
	case err == nil:
		return s.signInExisting(ctx, identity, v.Email, now)
	case !errors.Is(err, ErrNotFound):
		return User{}, false, fmt.Errorf("find identity: %w", err)
	}

	user, err = s.store.CreateUser(ctx, User{
		DisplayName:  strings.TrimSpace(v.DisplayName),
		PrimaryEmail: v.Email,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return User{}, false, fmt.Errorf("create user: %w", err)
	}

	_, err = s.store.CreateIdentity(ctx, AuthIdentity{
		UserID:        user.ID,
		Provider:      v.Provider,
		Subject:       v.Subject,
		Email:         v.Email,
		EmailVerified: v.Email != "",
		CreatedAt:     now,
		LastUsedAt:    now,
	})
	if errors.Is(err, ErrDuplicate) {
		// A concurrent first sign-in for the same identity won the race. Remove
		// the orphaned user and sign in to the winner's account instead.
		if delErr := s.store.DeleteUser(ctx, user.ID); delErr != nil {
			return User{}, false, fmt.Errorf("delete orphaned user: %w", delErr)
		}
		identity, err = s.store.FindIdentity(ctx, v.Provider, v.Subject)
		if err != nil {
			return User{}, false, fmt.Errorf("find identity after race: %w", err)
		}
		return s.signInExisting(ctx, identity, v.Email, now)
	}
	if err != nil {
		return User{}, false, fmt.Errorf("create identity: %w", err)
	}
	return user, true, nil
}

func (s *Service) signInExisting(ctx context.Context, identity AuthIdentity, email string, now time.Time) (User, bool, error) {
	if err := s.store.TouchIdentity(ctx, identity.ID, email, now); err != nil {
		return User{}, false, fmt.Errorf("touch identity: %w", err)
	}
	user, err := s.store.GetUser(ctx, identity.UserID)
	if err != nil {
		return User{}, false, fmt.Errorf("get user: %w", err)
	}
	return user, false, nil
}

// GetUser returns the user with the given ID, or ErrNotFound.
func (s *Service) GetUser(ctx context.Context, id string) (User, error) {
	return s.store.GetUser(ctx, id)
}

// ListIdentities returns the login methods attached to a user.
func (s *Service) ListIdentities(ctx context.Context, userID string) ([]AuthIdentity, error) {
	return s.store.ListIdentities(ctx, userID)
}
