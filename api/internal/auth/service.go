package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// ErrProviderNotConfigured means sign-in with a provider was requested but the
// server has no verifier for it (for example GOOGLE_CLIENT_ID is unset).
var ErrProviderNotConfigured = errors.New("auth: provider not configured")

// UserService is what auth needs from the users module.
type UserService interface {
	FindOrCreateByIdentity(ctx context.Context, v users.VerifiedIdentity) (users.User, bool, error)
	GetUser(ctx context.Context, id string) (users.User, error)
	ListIdentities(ctx context.Context, userID string) ([]users.AuthIdentity, error)
}

// SignInResult is returned by every sign-in path.
type SignInResult struct {
	Tokens    TokenPair
	User      users.User
	IsNewUser bool
}

// Service implements sign-in, refresh, logout, and the current-user lookup.
type Service struct {
	verifiers map[users.Provider]IdentityVerifier
	users     UserService
	tokens    *TokenService
	logger    *slog.Logger
}

// ServiceOptions configures a Service. Verifiers maps each enabled provider to
// its verifier; a missing provider yields ErrProviderNotConfigured.
type ServiceOptions struct {
	Verifiers map[users.Provider]IdentityVerifier
	Users     UserService
	Tokens    *TokenService
	Logger    *slog.Logger
}

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{verifiers: opts.Verifiers, users: opts.Users, tokens: opts.Tokens, logger: logger}
}

// SignInWithProvider verifies a provider token, finds or creates the user, and
// starts a new session. displayName is used only when a user is created.
func (s *Service) SignInWithProvider(ctx context.Context, provider users.Provider, token, nonce, displayName string) (SignInResult, error) {
	verifier, ok := s.verifiers[provider]
	if !ok || verifier == nil {
		return SignInResult{}, ErrProviderNotConfigured
	}
	identity, err := verifier.Verify(ctx, token, nonce)
	if err != nil {
		return SignInResult{}, err
	}
	if identity.DisplayName == "" {
		identity.DisplayName = displayName
	}
	return s.signIn(ctx, identity)
}

// SignInDev signs in with an unverified development identity. Callers must
// only expose this when development login is explicitly enabled.
func (s *Service) SignInDev(ctx context.Context, subject, email, displayName string) (SignInResult, error) {
	return s.signIn(ctx, users.VerifiedIdentity{
		Provider:      users.ProviderDev,
		Subject:       subject,
		Email:         email,
		EmailVerified: email != "",
		DisplayName:   displayName,
	})
}

func (s *Service) signIn(ctx context.Context, identity users.VerifiedIdentity) (SignInResult, error) {
	user, created, err := s.users.FindOrCreateByIdentity(ctx, identity)
	if err != nil {
		return SignInResult{}, fmt.Errorf("auth: find or create user: %w", err)
	}
	pair, err := s.tokens.IssueForNewSignIn(ctx, user.ID)
	if err != nil {
		return SignInResult{}, err
	}
	s.logger.InfoContext(ctx, "user signed in",
		"provider", string(identity.Provider), "userId", user.ID, "isNewUser", created)
	return SignInResult{Tokens: pair, User: user, IsNewUser: created}, nil
}

// Refresh rotates a refresh token. See TokenService.Refresh.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	pair, _, err := s.tokens.Refresh(ctx, refreshToken)
	return pair, err
}

// Logout revokes the sign-in a refresh token belongs to. It is idempotent.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.tokens.Revoke(ctx, refreshToken)
}

// Me returns the user and their login methods.
func (s *Service) Me(ctx context.Context, userID string) (users.User, []users.AuthIdentity, error) {
	user, err := s.users.GetUser(ctx, userID)
	if err != nil {
		return users.User{}, nil, err
	}
	identities, err := s.users.ListIdentities(ctx, userID)
	if err != nil {
		return users.User{}, nil, err
	}
	return user, identities, nil
}
