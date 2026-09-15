package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/ratelimit"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// fakeVerifier accepts tokens registered in identities.
type fakeVerifier struct {
	mu         sync.Mutex
	identities map[string]users.VerifiedIdentity
	lastNonce  string
}

func (f *fakeVerifier) Verify(_ context.Context, token, nonce string) (users.VerifiedIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastNonce = nonce
	switch token {
	case "provider-down":
		return users.VerifiedIdentity{}, fmt.Errorf("%w: status 503", ErrKeysUnavailable)
	}
	identity, ok := f.identities[token]
	if !ok {
		return users.VerifiedIdentity{}, fmt.Errorf("%w: token is expired", ErrInvalidIdentityToken)
	}
	return identity, nil
}

// fakeUsers is an in-memory UserService.
type fakeUsers struct {
	mu         sync.Mutex
	users      map[string]users.User
	identities []users.AuthIdentity
}

func (f *fakeUsers) FindOrCreateByIdentity(_ context.Context, v users.VerifiedIdentity) (users.User, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, identity := range f.identities {
		if identity.Provider == v.Provider && identity.Subject == v.Subject {
			return f.users[identity.UserID], false, nil
		}
	}
	id := fmt.Sprintf("%024x", len(f.users)+1)
	u := users.User{ID: id, DisplayName: v.DisplayName, PrimaryEmail: v.Email, CreatedAt: testNow, UpdatedAt: testNow}
	f.users[id] = u
	f.identities = append(f.identities, users.AuthIdentity{UserID: id, Provider: v.Provider, Subject: v.Subject, Email: v.Email, EmailVerified: v.EmailVerified})
	return u, true, nil
}

func (f *fakeUsers) GetUser(_ context.Context, id string) (users.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return users.User{}, users.ErrNotFound
	}
	return u, nil
}

func (f *fakeUsers) ListIdentities(_ context.Context, userID string) ([]users.AuthIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []users.AuthIdentity
	for _, identity := range f.identities {
		if identity.UserID == userID {
			out = append(out, identity)
		}
	}
	return out, nil
}

type authTestServer struct {
	router *chi.Mux
	apple  *fakeVerifier
	tokens *TokenService
	clock  *fakeClock
	logs   *bytes.Buffer
}

type serverConfig struct {
	devLogin       bool
	withoutGoogle  bool
	rateLimitBurst int
}

func newAuthTestServer(t *testing.T, cfg serverConfig) *authTestServer {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	clock := newFakeClock()
	tokens, err := NewTokenService(&memorySessionStore{}, TokenOptions{SigningKey: testSigningKey, Now: clock.Now, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	apple := &fakeVerifier{identities: map[string]users.VerifiedIdentity{
		"apple-token": {Provider: users.ProviderApple, Subject: "apple-sub", Email: "ada@privaterelay.appleid.com", EmailVerified: true},
	}}
	google := &fakeVerifier{identities: map[string]users.VerifiedIdentity{
		"google-token": {Provider: users.ProviderGoogle, Subject: "google-sub", Email: "ada@gmail.com", EmailVerified: true, DisplayName: "Ada G"},
	}}
	verifiers := map[users.Provider]IdentityVerifier{users.ProviderApple: apple, users.ProviderGoogle: google}
	if cfg.withoutGoogle {
		delete(verifiers, users.ProviderGoogle)
	}
	svc := NewService(ServiceOptions{
		Verifiers: verifiers,
		Users:     &fakeUsers{users: map[string]users.User{}},
		Tokens:    tokens,
		Logger:    logger,
	})
	opts := HandlerOptions{Service: svc, Tokens: tokens, Logger: logger, DevLoginEnabled: cfg.devLogin}
	if cfg.rateLimitBurst > 0 {
		opts.RateLimit = ratelimit.New(ratelimit.Options{Burst: cfg.rateLimitBurst, Now: clock.Now}).Middleware
	}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(opts).Mount)
	return &authTestServer{router: r, apple: apple, tokens: tokens, clock: clock, logs: &logs}
}

func (s *authTestServer) do(t *testing.T, method, path, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	if got := decodeBody[httpx.ErrorResponse](t, rec).Error.Code; got != code {
		t.Fatalf("error code = %q, want %q", got, code)
	}
}

func TestAppleSignInHandler(t *testing.T) {
	srv := newAuthTestServer(t, serverConfig{})

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/apple", `{"identityToken":"apple-token","nonce":"n-123","fullName":" Ada Lovelace "}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[SessionResponse](t, rec)
	if !body.IsNewUser || body.User.DisplayName != "Ada Lovelace" || body.User.PrimaryEmail != "ada@privaterelay.appleid.com" || body.User.ID == "" {
		t.Errorf("body = %+v", body)
	}
	if body.AccessToken == "" || body.RefreshToken == "" || body.AccessTokenExpiresAt.IsZero() || body.RefreshTokenExpiresAt.IsZero() {
		t.Errorf("token fields missing: %+v", body.TokenPairResponse)
	}
	if srv.apple.lastNonce != "n-123" {
		t.Errorf("verifier nonce = %q, want n-123", srv.apple.lastNonce)
	}
	if userID, err := srv.tokens.ValidateAccessToken(body.AccessToken); err != nil || userID != body.User.ID {
		t.Errorf("access token sub = %q, %v; want %q", userID, err, body.User.ID)
	}

	again := decodeBody[SessionResponse](t, srv.do(t, http.MethodPost, "/api/v1/auth/apple", `{"identityToken":"apple-token","nonce":"n-456"}`, ""))
	if again.IsNewUser || again.User.ID != body.User.ID {
		t.Errorf("second sign-in = %+v, want existing user", again)
	}

	// No secret material in logs.
	for _, secret := range []string{"apple-token", "n-123", body.AccessToken, body.RefreshToken} {
		if strings.Contains(srv.logs.String(), secret) {
			t.Errorf("logs contain secret %q: %s", secret, srv.logs.String())
		}
	}
}

func TestSignInHandlerErrors(t *testing.T) {
	tests := []struct {
		name   string
		cfg    serverConfig
		path   string
		body   string
		status int
		code   string
	}{
		{"apple empty body", serverConfig{}, "/api/v1/auth/apple", ``, 400, "invalid_request"},
		{"apple malformed", serverConfig{}, "/api/v1/auth/apple", `{"identityToken":`, 400, "invalid_request"},
		{"apple unknown field", serverConfig{}, "/api/v1/auth/apple", `{"identityToken":"apple-token","nonce":"n","email":"x@y.z"}`, 400, "invalid_request"},
		{"apple missing token", serverConfig{}, "/api/v1/auth/apple", `{"nonce":"n"}`, 400, "validation_failed"},
		{"apple missing nonce", serverConfig{}, "/api/v1/auth/apple", `{"identityToken":"apple-token"}`, 400, "validation_failed"},
		{"apple long name", serverConfig{}, "/api/v1/auth/apple", `{"identityToken":"apple-token","nonce":"n","fullName":"` + strings.Repeat("a", 101) + `"}`, 400, "validation_failed"},
		{"apple invalid token", serverConfig{}, "/api/v1/auth/apple", `{"identityToken":"forged","nonce":"n"}`, 401, "unauthenticated"},
		{"apple provider down", serverConfig{}, "/api/v1/auth/apple", `{"identityToken":"provider-down","nonce":"n"}`, 503, "provider_unavailable"},
		{"google missing token", serverConfig{}, "/api/v1/auth/google", `{}`, 400, "validation_failed"},
		{"google invalid token", serverConfig{}, "/api/v1/auth/google", `{"idToken":"forged"}`, 401, "unauthenticated"},
		{"google not configured", serverConfig{withoutGoogle: true}, "/api/v1/auth/google", `{"idToken":"google-token"}`, 503, "provider_unavailable"},
		{"refresh missing token", serverConfig{}, "/api/v1/auth/refresh", `{}`, 400, "validation_failed"},
		{"refresh unknown token", serverConfig{}, "/api/v1/auth/refresh", `{"refreshToken":"nope"}`, 401, "unauthenticated"},
		{"logout missing token", serverConfig{}, "/api/v1/auth/logout", `{}`, 400, "validation_failed"},
		{"dev missing subject", serverConfig{devLogin: true}, "/api/v1/auth/dev", `{"email":"a@b.co"}`, 400, "validation_failed"},
		{"dev invalid email", serverConfig{devLogin: true}, "/api/v1/auth/dev", `{"subject":"s","email":"Ada <a@b.co>"}`, 400, "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthTestServer(t, tt.cfg)
			wantError(t, srv.do(t, http.MethodPost, tt.path, tt.body, ""), tt.status, tt.code)
		})
	}
}

func TestGoogleSignInHandler(t *testing.T) {
	srv := newAuthTestServer(t, serverConfig{})
	rec := srv.do(t, http.MethodPost, "/api/v1/auth/google", `{"idToken":"google-token"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[SessionResponse](t, rec)
	if !body.IsNewUser || body.User.DisplayName != "Ada G" || body.User.PrimaryEmail != "ada@gmail.com" {
		t.Errorf("body = %+v", body)
	}
}

func TestRefreshAndLogoutHandlers(t *testing.T) {
	srv := newAuthTestServer(t, serverConfig{})
	signIn := decodeBody[SessionResponse](t, srv.do(t, http.MethodPost, "/api/v1/auth/google", `{"idToken":"google-token"}`, ""))

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"`+signIn.RefreshToken+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body %s", rec.Code, rec.Body.String())
	}
	refreshed := decodeBody[TokenPairResponse](t, rec)
	if refreshed.RefreshToken == "" || refreshed.RefreshToken == signIn.RefreshToken || refreshed.AccessToken == "" {
		t.Fatalf("refreshed = %+v", refreshed)
	}
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if _, ok := raw["user"]; ok {
		t.Errorf("refresh response unexpectedly includes user: %s", rec.Body.String())
	}

	// Reusing the rotated token fails and revokes the family.
	wantError(t, srv.do(t, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"`+signIn.RefreshToken+`"}`, ""), 401, "unauthenticated")
	wantError(t, srv.do(t, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"`+refreshed.RefreshToken+`"}`, ""), 401, "unauthenticated")
	if !strings.Contains(srv.logs.String(), "refresh token reuse detected") {
		t.Errorf("reuse not logged: %s", srv.logs.String())
	}

	// Logout is idempotent and makes the refresh token unusable.
	session := decodeBody[SessionResponse](t, srv.do(t, http.MethodPost, "/api/v1/auth/google", `{"idToken":"google-token"}`, ""))
	for i := range 2 {
		rec := srv.do(t, http.MethodPost, "/api/v1/auth/logout", `{"refreshToken":"`+session.RefreshToken+`"}`, "")
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("logout #%d status = %d body %q, want 204", i+1, rec.Code, rec.Body.String())
		}
	}
	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/logout", `{"refreshToken":"never-issued"}`, ""); rec.Code != http.StatusNoContent {
		t.Errorf("logout unknown token status = %d, want 204", rec.Code)
	}
	wantError(t, srv.do(t, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"`+session.RefreshToken+`"}`, ""), 401, "unauthenticated")

	for _, secret := range []string{signIn.RefreshToken, refreshed.RefreshToken, session.RefreshToken} {
		if strings.Contains(srv.logs.String(), secret) {
			t.Errorf("logs contain refresh token")
		}
	}
}

func TestMeHandler(t *testing.T) {
	srv := newAuthTestServer(t, serverConfig{})
	session := decodeBody[SessionResponse](t, srv.do(t, http.MethodPost, "/api/v1/auth/apple", `{"identityToken":"apple-token","nonce":"n"}`, ""))

	rec := srv.do(t, http.MethodGet, "/api/v1/me", "", session.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	me := decodeBody[MeResponse](t, rec)
	if me.User.ID != session.User.ID || len(me.Identities) != 1 ||
		me.Identities[0] != (IdentityResponse{Provider: "apple", Email: "ada@privaterelay.appleid.com"}) {
		t.Errorf("me = %+v", me)
	}
	if strings.Contains(rec.Body.String(), "apple-sub") {
		t.Errorf("/me leaks provider subject: %s", rec.Body.String())
	}

	t.Run("missing bearer", func(t *testing.T) {
		rec := srv.do(t, http.MethodGet, "/api/v1/me", "", "")
		wantError(t, rec, 401, "unauthenticated")
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Error("missing WWW-Authenticate header")
		}
	})
	t.Run("wrong scheme", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Authorization", "Basic "+session.AccessToken)
		rec := httptest.NewRecorder()
		srv.router.ServeHTTP(rec, req)
		wantError(t, rec, 401, "unauthenticated")
	})
	t.Run("refresh token is not an access token", func(t *testing.T) {
		wantError(t, srv.do(t, http.MethodGet, "/api/v1/me", "", session.RefreshToken), 401, "unauthenticated")
	})
	t.Run("token for deleted user", func(t *testing.T) {
		pair, err := srv.tokens.IssueForNewSignIn(context.Background(), "ffffffffffffffffffffffff")
		if err != nil {
			t.Fatal(err)
		}
		wantError(t, srv.do(t, http.MethodGet, "/api/v1/me", "", pair.AccessToken), 401, "unauthenticated")
	})
	t.Run("expired", func(t *testing.T) {
		srv.clock.Advance(16 * time.Minute)
		wantError(t, srv.do(t, http.MethodGet, "/api/v1/me", "", session.AccessToken), 401, "token_expired")
	})
}

func TestDevSignInRoute(t *testing.T) {
	t.Run("absent when disabled", func(t *testing.T) {
		srv := newAuthTestServer(t, serverConfig{devLogin: false})
		rec := srv.do(t, http.MethodPost, "/api/v1/auth/dev", `{"subject":"dev-1"}`, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("signs in when enabled", func(t *testing.T) {
		srv := newAuthTestServer(t, serverConfig{devLogin: true})
		body := `{"subject":"dev-1","email":"Dev@Example.com","displayName":"Dev User"}`
		rec := srv.do(t, http.MethodPost, "/api/v1/auth/dev", body, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
		session := decodeBody[SessionResponse](t, rec)
		if !session.IsNewUser || session.User.PrimaryEmail != "dev@example.com" || session.User.DisplayName != "Dev User" {
			t.Errorf("session = %+v", session)
		}
		me := decodeBody[MeResponse](t, srv.do(t, http.MethodGet, "/api/v1/me", "", session.AccessToken))
		if len(me.Identities) != 1 || me.Identities[0].Provider != "dev" {
			t.Errorf("identities = %+v", me.Identities)
		}
	})
}

func TestAuthRoutesAreRateLimited(t *testing.T) {
	srv := newAuthTestServer(t, serverConfig{rateLimitBurst: 2})
	for i := range 2 {
		rec := srv.do(t, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"x"}`, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("request %d status = %d, want 401", i+1, rec.Code)
		}
	}
	wantError(t, srv.do(t, http.MethodPost, "/api/v1/auth/apple", `{"identityToken":"apple-token","nonce":"n"}`, ""), 429, "rate_limited")

	// /me is not an /auth route and is not limited.
	if rec := srv.do(t, http.MethodGet, "/api/v1/me", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("/me status = %d, want 401", rec.Code)
	}
}
