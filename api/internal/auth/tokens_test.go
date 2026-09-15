package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// memorySessionStore is an in-memory SessionStore for unit tests.
type memorySessionStore struct {
	mu       sync.Mutex
	next     int
	sessions []Session

	// beforeMarkRotated, when set, runs once before MarkRotated so tests can
	// simulate a concurrent refresh with the same token.
	beforeMarkRotated func()
}

func (m *memorySessionStore) CreateSession(_ context.Context, s Session) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.sessions {
		if existing.TokenHash == s.TokenHash {
			return Session{}, errors.New("duplicate token hash")
		}
	}
	m.next++
	s.ID = fmt.Sprintf("%024x", m.next)
	m.sessions = append(m.sessions, s)
	return s, nil
}

func (m *memorySessionStore) FindSessionByTokenHash(_ context.Context, hash string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.TokenHash == hash {
			return s, nil
		}
	}
	return Session{}, ErrSessionNotFound
}

func (m *memorySessionStore) MarkRotated(_ context.Context, id string, at time.Time) (bool, error) {
	if hook := m.beforeMarkRotated; hook != nil {
		m.beforeMarkRotated = nil
		hook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.sessions {
		s := &m.sessions[i]
		if s.ID == id {
			if s.RotatedAt != nil || s.RevokedAt != nil {
				return false, nil
			}
			s.RotatedAt, s.LastUsedAt = &at, at
			return true, nil
		}
	}
	return false, nil
}

func (m *memorySessionStore) RevokeFamily(_ context.Context, familyID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.sessions {
		if m.sessions[i].FamilyID == familyID && m.sessions[i].RevokedAt == nil {
			m.sessions[i].RevokedAt = &at
		}
	}
	return nil
}

func (m *memorySessionStore) byHash(token string) Session {
	s, _ := m.FindSessionByTokenHash(context.Background(), hashToken(token))
	return s
}

var testSigningKey = []byte("0123456789abcdef0123456789abcdef-test-key")

const testUserID = "66e5a1f2c3b4a5d6e7f80912"

func newTestTokens(t *testing.T) (*TokenService, *memorySessionStore, *fakeClock) {
	t.Helper()
	store := &memorySessionStore{}
	clock := newFakeClock()
	svc, err := NewTokenService(store, TokenOptions{SigningKey: testSigningKey, Now: clock.Now})
	if err != nil {
		t.Fatalf("NewTokenService() error = %v", err)
	}
	return svc, store, clock
}

func TestNewTokenServiceRejectsShortKey(t *testing.T) {
	if _, err := NewTokenService(&memorySessionStore{}, TokenOptions{SigningKey: make([]byte, 31)}); err == nil {
		t.Fatal("NewTokenService(31-byte key) error = nil")
	}
}

func TestIssueAndValidateAccessToken(t *testing.T) {
	svc, store, clock := newTestTokens(t)
	pair, err := svc.IssueForNewSignIn(context.Background(), testUserID)
	if err != nil {
		t.Fatalf("IssueForNewSignIn() error = %v", err)
	}

	userID, err := svc.ValidateAccessToken(pair.AccessToken)
	if err != nil || userID != testUserID {
		t.Fatalf("ValidateAccessToken() = %q, %v", userID, err)
	}

	var claims jwt.RegisteredClaims
	tok, _, err := jwt.NewParser().ParseUnverified(pair.AccessToken, &claims)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Method.Alg() != "HS256" || claims.Issuer != AccessTokenIssuer || len(claims.Audience) != 1 ||
		claims.Audience[0] != AccessTokenAudience || claims.ID == "" || claims.Subject != testUserID {
		t.Errorf("claims = %+v alg=%s", claims, tok.Method.Alg())
	}
	if got := claims.ExpiresAt.Sub(claims.IssuedAt.Time); got != 15*time.Minute {
		t.Errorf("access lifetime = %v, want 15m", got)
	}
	if !pair.AccessTokenExpiresAt.Equal(claims.ExpiresAt.Time) {
		t.Errorf("AccessTokenExpiresAt = %v, want %v", pair.AccessTokenExpiresAt, claims.ExpiresAt.Time)
	}

	raw, err := base64.RawURLEncoding.DecodeString(pair.RefreshToken)
	if err != nil || len(raw) != 32 {
		t.Errorf("refresh token decodes to %d bytes (err %v), want 32", len(raw), err)
	}
	if !pair.RefreshTokenExpiresAt.Equal(clock.Now().Add(60 * 24 * time.Hour)) {
		t.Errorf("RefreshTokenExpiresAt = %v", pair.RefreshTokenExpiresAt)
	}
	if len(store.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(store.sessions))
	}
	s := store.sessions[0]
	if s.TokenHash == pair.RefreshToken || s.TokenHash != hashToken(pair.RefreshToken) || s.UserID != testUserID || s.FamilyID == "" {
		t.Errorf("stored session = %+v; must hold only the hash", s)
	}
}

func TestValidateAccessTokenRejections(t *testing.T) {
	svc, _, clock := newTestTokens(t)
	pair, err := svc.IssueForNewSignIn(context.Background(), testUserID)
	if err != nil {
		t.Fatal(err)
	}

	sign := func(method jwt.SigningMethod, key any, mutate func(jwt.MapClaims)) string {
		c := jwt.MapClaims{
			"iss": AccessTokenIssuer, "aud": AccessTokenAudience, "sub": testUserID,
			"iat": clock.Now().Unix(), "exp": clock.Now().Add(time.Minute).Unix(), "jti": "x",
		}
		if mutate != nil {
			mutate(c)
		}
		s, err := jwt.NewWithClaims(method, c).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	tests := []struct {
		name  string
		token string
		want  error
	}{
		{"control: valid hand-built token", sign(jwt.SigningMethodHS256, testSigningKey, nil), nil},
		{"wrong signing key", sign(jwt.SigningMethodHS256, []byte("another-key-another-key-another-key!"), nil), ErrInvalidAccessToken},
		{"HS512 rejected", sign(jwt.SigningMethodHS512, testSigningKey, nil), ErrInvalidAccessToken},
		{"alg none rejected", sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, nil), ErrInvalidAccessToken},
		{"RS256 rejected", sign(jwt.SigningMethodRS256, testKeyA(), nil), ErrInvalidAccessToken},
		{"wrong audience", sign(jwt.SigningMethodHS256, testSigningKey, func(c jwt.MapClaims) { c["aud"] = "other" }), ErrInvalidAccessToken},
		{"wrong issuer", sign(jwt.SigningMethodHS256, testSigningKey, func(c jwt.MapClaims) { c["iss"] = "https://appleid.apple.com" }), ErrInvalidAccessToken},
		{"missing exp", sign(jwt.SigningMethodHS256, testSigningKey, func(c jwt.MapClaims) { delete(c, "exp") }), ErrInvalidAccessToken},
		{"missing sub", sign(jwt.SigningMethodHS256, testSigningKey, func(c jwt.MapClaims) { delete(c, "sub") }), ErrInvalidAccessToken},
		{"expired", sign(jwt.SigningMethodHS256, testSigningKey, func(c jwt.MapClaims) { c["exp"] = clock.Now().Add(-time.Hour).Unix() }), ErrAccessTokenExpired},
		{"tampered payload", pair.AccessToken[:strings.LastIndexByte(pair.AccessToken, '.')] + ".c2lnbmF0dXJl", ErrInvalidAccessToken},
		{"garbage", "garbage", ErrInvalidAccessToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ValidateAccessToken(tt.token)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("issued token expires after 15 minutes", func(t *testing.T) {
		clock.Advance(16 * time.Minute)
		if _, err := svc.ValidateAccessToken(pair.AccessToken); !errors.Is(err, ErrAccessTokenExpired) {
			t.Fatalf("error = %v, want ErrAccessTokenExpired", err)
		}
	})
}

func TestRefreshRotation(t *testing.T) {
	svc, store, clock := newTestTokens(t)
	ctx := context.Background()
	first, err := svc.IssueForNewSignIn(ctx, testUserID)
	if err != nil {
		t.Fatal(err)
	}

	clock.Advance(time.Hour)
	second, userID, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if userID != testUserID || second.RefreshToken == first.RefreshToken || second.AccessToken == first.AccessToken {
		t.Errorf("Refresh() returned userID=%q or reused tokens", userID)
	}
	if !second.RefreshTokenExpiresAt.Equal(clock.Now().Add(60 * 24 * time.Hour)) {
		t.Errorf("RefreshTokenExpiresAt = %v, want sliding expiry from now", second.RefreshTokenExpiresAt)
	}

	old, current := store.byHash(first.RefreshToken), store.byHash(second.RefreshToken)
	if old.RotatedAt == nil || !old.RotatedAt.Equal(clock.Now()) || old.RevokedAt != nil {
		t.Errorf("old session = %+v, want rotated", old)
	}
	if current.FamilyID != old.FamilyID || current.RotatedAt != nil || current.RevokedAt != nil {
		t.Errorf("new session = %+v, want same family and active", current)
	}

	if _, _, err := svc.Refresh(ctx, second.RefreshToken); err != nil {
		t.Errorf("refreshing the newest token: %v", err)
	}
}

func TestRefreshReuseRevokesFamily(t *testing.T) {
	svc, store, _ := newTestTokens(t)
	ctx := context.Background()
	first, _ := svc.IssueForNewSignIn(ctx, testUserID)
	other, _ := svc.IssueForNewSignIn(ctx, testUserID) // a second device: separate family
	second, _, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}

	// An attacker replays the rotated token.
	if _, _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("replay error = %v, want ErrRefreshTokenReused", err)
	}
	// The legitimate newest token in the family is now revoked too.
	if _, _, err := svc.Refresh(ctx, second.RefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("newest token after reuse error = %v, want ErrRefreshTokenReused", err)
	}
	if s := store.byHash(second.RefreshToken); s.RevokedAt == nil {
		t.Error("newest session not revoked")
	}
	// Other sign-ins are untouched.
	if _, _, err := svc.Refresh(ctx, other.RefreshToken); err != nil {
		t.Errorf("other family refresh error = %v", err)
	}
}

func TestRefreshConcurrentUseIsReuse(t *testing.T) {
	svc, store, _ := newTestTokens(t)
	ctx := context.Background()
	pair, _ := svc.IssueForNewSignIn(ctx, testUserID)

	// Another request rotates the same session between our read and write.
	store.beforeMarkRotated = func() {
		s := store.byHash(pair.RefreshToken)
		if _, err := store.MarkRotated(ctx, s.ID, testNow); err != nil {
			t.Error(err)
		}
	}
	if _, _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("error = %v, want ErrRefreshTokenReused", err)
	}
	if s := store.byHash(pair.RefreshToken); s.RevokedAt == nil {
		t.Error("family not revoked")
	}
}

func TestRefreshInvalidAndExpired(t *testing.T) {
	svc, _, clock := newTestTokens(t)
	ctx := context.Background()
	pair, _ := svc.IssueForNewSignIn(ctx, testUserID)

	for _, token := range []string{"", "unknown-token"} {
		if _, _, err := svc.Refresh(ctx, token); !errors.Is(err, ErrInvalidRefreshToken) {
			t.Errorf("Refresh(%q) error = %v, want ErrInvalidRefreshToken", token, err)
		}
	}

	clock.Advance(60*24*time.Hour + time.Second)
	if _, _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("expired Refresh() error = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestRevoke(t *testing.T) {
	svc, store, _ := newTestTokens(t)
	ctx := context.Background()
	first, _ := svc.IssueForNewSignIn(ctx, testUserID)
	second, _, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}

	for i := range 2 { // idempotent
		if err := svc.Revoke(ctx, second.RefreshToken); err != nil {
			t.Fatalf("Revoke() #%d error = %v", i+1, err)
		}
	}
	if err := svc.Revoke(ctx, "unknown"); err != nil {
		t.Errorf("Revoke(unknown) error = %v", err)
	}
	if err := svc.Revoke(ctx, ""); err != nil {
		t.Errorf("Revoke(empty) error = %v", err)
	}
	for _, s := range store.sessions {
		if s.RevokedAt == nil {
			t.Errorf("session %s not revoked", s.ID)
		}
	}
	if _, _, err := svc.Refresh(ctx, second.RefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Errorf("refresh after logout error = %v, want rejection", err)
	}
}
