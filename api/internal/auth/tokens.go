package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Access token claims and lifetimes.
const (
	AccessTokenIssuer   = "dinneros"
	AccessTokenAudience = "dinneros-api"
	DefaultAccessTTL    = 15 * time.Minute
	DefaultRefreshTTL   = 60 * 24 * time.Hour
	MinSigningKeyBytes  = 32
	refreshTokenBytes   = 32
	accessTokenLeeway   = 30 * time.Second
)

// Token errors.
var (
	// ErrInvalidAccessToken means the bearer token is malformed, forged, or
	// otherwise unacceptable.
	ErrInvalidAccessToken = errors.New("auth: invalid access token")
	// ErrAccessTokenExpired means the bearer token was valid but has expired.
	ErrAccessTokenExpired = errors.New("auth: access token expired")
	// ErrInvalidRefreshToken means the refresh token is unknown or expired.
	ErrInvalidRefreshToken = errors.New("auth: invalid refresh token")
	// ErrRefreshTokenReused means an already-rotated or revoked refresh token
	// was presented; the whole token family has been revoked.
	ErrRefreshTokenReused = errors.New("auth: refresh token reuse detected")
)

// TokenPair is issued at sign-in and on every refresh.
type TokenPair struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

// TokenOptions configures a TokenService.
type TokenOptions struct {
	SigningKey []byte        // required, at least 32 bytes
	AccessTTL  time.Duration // default 15m
	RefreshTTL time.Duration // default 60d, sliding
	Now        func() time.Time
	Logger     *slog.Logger
}

// TokenService issues and validates DinnerOS access tokens and manages
// refresh-token sessions with rotation and reuse detection.
type TokenService struct {
	key        []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
	sessions   SessionStore
	logger     *slog.Logger
	parser     *jwt.Parser
}

// NewTokenService returns a TokenService that stores sessions in store.
func NewTokenService(store SessionStore, opts TokenOptions) (*TokenService, error) {
	if len(opts.SigningKey) < MinSigningKeyBytes {
		return nil, fmt.Errorf("auth: signing key must be at least %d bytes", MinSigningKeyBytes)
	}
	s := &TokenService{
		key:        append([]byte(nil), opts.SigningKey...),
		accessTTL:  opts.AccessTTL,
		refreshTTL: opts.RefreshTTL,
		now:        opts.Now,
		sessions:   store,
		logger:     opts.Logger,
	}
	if s.accessTTL <= 0 {
		s.accessTTL = DefaultAccessTTL
	}
	if s.refreshTTL <= 0 {
		s.refreshTTL = DefaultRefreshTTL
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	s.parser = jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(AccessTokenAudience),
		jwt.WithIssuer(AccessTokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(accessTokenLeeway),
		jwt.WithTimeFunc(func() time.Time { return s.now() }),
	)
	return s, nil
}

// IssueForNewSignIn starts a new token family for userID.
func (s *TokenService) IssueForNewSignIn(ctx context.Context, userID string) (TokenPair, error) {
	familyID, err := randomHex(12)
	if err != nil {
		return TokenPair{}, err
	}
	return s.issue(ctx, userID, familyID)
}

func (s *TokenService) issue(ctx context.Context, userID, familyID string) (TokenPair, error) {
	now := s.now().UTC()
	access, accessExp, err := s.signAccessToken(userID, now)
	if err != nil {
		return TokenPair{}, err
	}

	raw := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return TokenPair{}, fmt.Errorf("auth: generate refresh token: %w", err)
	}
	refresh := base64.RawURLEncoding.EncodeToString(raw)
	refreshExp := now.Add(s.refreshTTL)

	if _, err := s.sessions.CreateSession(ctx, Session{
		UserID:     userID,
		FamilyID:   familyID,
		TokenHash:  hashToken(refresh),
		ExpiresAt:  refreshExp,
		CreatedAt:  now,
		LastUsedAt: now,
	}); err != nil {
		return TokenPair{}, fmt.Errorf("auth: create session: %w", err)
	}

	return TokenPair{
		AccessToken:           access,
		AccessTokenExpiresAt:  accessExp,
		RefreshToken:          refresh,
		RefreshTokenExpiresAt: refreshExp,
	}, nil
}

// accessClaims are the claims in a DinnerOS access token.
type accessClaims = jwt.RegisteredClaims

func (s *TokenService) signAccessToken(userID string, now time.Time) (string, time.Time, error) {
	jti, err := randomHex(16)
	if err != nil {
		return "", time.Time{}, err
	}
	// JWT times have one-second resolution; report the value actually signed.
	exp := now.Add(s.accessTTL).Truncate(time.Second)
	claims := accessClaims{
		Issuer:    AccessTokenIssuer,
		Subject:   userID,
		Audience:  jwt.ClaimStrings{AccessTokenAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
		ID:        jti,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, exp, nil
}

// ValidateAccessToken verifies a bearer token and returns its user ID. Errors
// are ErrAccessTokenExpired or ErrInvalidAccessToken (wrapped).
func (s *TokenService) ValidateAccessToken(token string) (string, error) {
	var claims accessClaims
	_, err := s.parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) {
		return s.key, nil
	})
	switch {
	case errors.Is(err, jwt.ErrTokenExpired) && !errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return "", ErrAccessTokenExpired
	case err != nil:
		return "", fmt.Errorf("%w: %w", ErrInvalidAccessToken, err)
	case claims.Subject == "" || claims.IssuedAt == nil:
		return "", fmt.Errorf("%w: missing sub or iat", ErrInvalidAccessToken)
	}
	return claims.Subject, nil
}

// Refresh exchanges a refresh token for a new token pair in the same family.
//
// The presented session is marked rotated. Presenting a token that was already
// rotated or revoked is treated as theft: the whole family is revoked and
// ErrRefreshTokenReused is returned.
func (s *TokenService) Refresh(ctx context.Context, refreshToken string) (TokenPair, string, error) {
	if refreshToken == "" {
		return TokenPair{}, "", ErrInvalidRefreshToken
	}
	now := s.now().UTC()

	session, err := s.sessions.FindSessionByTokenHash(ctx, hashToken(refreshToken))
	if errors.Is(err, ErrSessionNotFound) {
		return TokenPair{}, "", ErrInvalidRefreshToken
	}
	if err != nil {
		return TokenPair{}, "", fmt.Errorf("auth: find session: %w", err)
	}

	if session.RotatedAt != nil || session.RevokedAt != nil {
		return TokenPair{}, "", s.revokeReusedFamily(ctx, session, now)
	}
	if !now.Before(session.ExpiresAt) {
		return TokenPair{}, "", ErrInvalidRefreshToken
	}

	rotated, err := s.sessions.MarkRotated(ctx, session.ID, now)
	if err != nil {
		return TokenPair{}, "", fmt.Errorf("auth: rotate session: %w", err)
	}
	if !rotated {
		// Another request rotated or revoked this session between our read and
		// write: the same token was used twice.
		return TokenPair{}, "", s.revokeReusedFamily(ctx, session, now)
	}

	pair, err := s.issue(ctx, session.UserID, session.FamilyID)
	if err != nil {
		return TokenPair{}, "", err
	}
	return pair, session.UserID, nil
}

func (s *TokenService) revokeReusedFamily(ctx context.Context, session Session, now time.Time) error {
	if err := s.sessions.RevokeFamily(ctx, session.FamilyID, now); err != nil {
		return fmt.Errorf("auth: revoke family after reuse: %w", err)
	}
	s.logger.WarnContext(ctx, "refresh token reuse detected; session family revoked",
		"userId", session.UserID, "familyId", session.FamilyID)
	return ErrRefreshTokenReused
}

// Revoke ends the sign-in a refresh token belongs to by revoking its whole
// token family. It is idempotent: unknown, expired, or already revoked tokens
// succeed silently.
func (s *TokenService) Revoke(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	session, err := s.sessions.FindSessionByTokenHash(ctx, hashToken(refreshToken))
	if errors.Is(err, ErrSessionNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: find session: %w", err)
	}
	if err := s.sessions.RevokeFamily(ctx, session.FamilyID, s.now().UTC()); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// hashToken returns the hex SHA-256 of a refresh token. Only this hash is
// stored, so a database leak does not expose usable tokens.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: random: %w", err)
	}
	return hex.EncodeToString(b), nil
}
