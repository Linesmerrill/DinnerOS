package siwa

import (
	"context"
	"log/slog"
	"time"
)

// Options configures a Service.
type Options struct {
	// Client is nil when no Sign in with Apple key is configured; the
	// Service then does nothing.
	Client *Client
	Store  Store
	// SealKey derives the key tokens are sealed with (the server's token
	// signing key). Required when Client is set.
	SealKey []byte
	Logger  *slog.Logger
	Now     func() time.Time
}

// Service captures refresh tokens at sign-in and revokes them at deletion.
type Service struct {
	client *Client
	store  Store
	box    *box
	logger *slog.Logger
	now    func() time.Time
}

// NewService returns a Service. A Client without a usable SealKey is an
// error, so revocation is never half on.
func NewService(opts Options) (*Service, error) {
	s := &Service{client: opts.Client, store: opts.Store, logger: opts.Logger, now: opts.Now}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.client != nil {
		b, err := newBox(opts.SealKey)
		if err != nil {
			return nil, err
		}
		s.box = b
	}
	return s, nil
}

// Enabled reports whether a key is configured.
func (s *Service) Enabled() bool { return s != nil && s.client != nil }

// SaveFromCode exchanges the authorization code from a sign-in and keeps the
// refresh token for the user. Failures are logged and dropped: sign-in has
// already succeeded.
func (s *Service) SaveFromCode(ctx context.Context, userID, code string) {
	if !s.Enabled() || code == "" {
		return
	}
	token, err := s.client.ExchangeCode(ctx, code)
	if err != nil {
		s.logger.WarnContext(ctx, "apple code exchange failed", "userId", userID, "error", err)
		return
	}
	sealed, err := s.box.seal(token)
	if err == nil {
		err = s.store.Put(ctx, userID, sealed, s.now())
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "apple refresh token not saved", "userId", userID, "error", err)
		return
	}
	s.logger.InfoContext(ctx, "apple refresh token saved", "userId", userID)
}

// RevokeForDeletion revokes the user's Sign in with Apple authorization
// before their account is deleted: the stored refresh token, and the one a
// fresh code from the delete request exchanges for. It never fails; Apple's
// answers are logged. The stored token itself is deleted with the account
// (MongoStore.PurgeUser).
func (s *Service) RevokeForDeletion(ctx context.Context, userID, code string) {
	if !s.Enabled() {
		return
	}
	var tokens []string
	if sealed, err := s.store.Get(ctx, userID); err != nil {
		s.logger.ErrorContext(ctx, "apple refresh token not loaded", "userId", userID, "error", err)
	} else if sealed != "" {
		if token, err := s.box.open(sealed); err != nil {
			s.logger.WarnContext(ctx, "apple refresh token unreadable", "userId", userID)
		} else {
			tokens = append(tokens, token)
		}
	}
	if code != "" {
		if token, err := s.client.ExchangeCode(ctx, code); err != nil {
			s.logger.WarnContext(ctx, "apple code exchange failed at deletion", "userId", userID, "error", err)
		} else if len(tokens) == 0 || tokens[0] != token {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		s.logger.InfoContext(ctx, "no apple token to revoke", "userId", userID, "freshCode", code != "")
		return
	}
	revoked := 0
	for _, token := range tokens {
		if err := s.client.RevokeRefreshToken(ctx, token); err != nil {
			s.logger.WarnContext(ctx, "apple token revoke failed", "userId", userID, "error", err)
			continue
		}
		revoked++
	}
	s.logger.InfoContext(ctx, "apple sign-in revoked", "userId", userID, "revoked", revoked, "tokens", len(tokens))
}
