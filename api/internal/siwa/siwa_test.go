package siwa

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testTeamID   = "6VTPDG2HNK"
	testKeyID    = "SIWAKEY123"
	testClientID = "com.linesmerrill.dinneros"
	userAda      = "aaaaaaaaaaaaaaaaaaaaaaaa"
)

var testSealKey = []byte("0123456789abcdef0123456789abcdef")

// fakeApple stands in for appleid.apple.com: /auth/token trades known codes
// for refresh tokens and /auth/revoke records what it revoked. Every request
// must carry a client secret signed by key.
type fakeApple struct {
	t   *testing.T
	key *ecdsa.PrivateKey

	mu         sync.Mutex
	codes      map[string]string // code -> refresh token
	tokenFail  int               // status to answer /auth/token with
	revokeFail int               // status to answer /auth/revoke with
	exchanged  []string
	revoked    []string
	secrets    []string
}

func newFakeApple(t *testing.T, key *ecdsa.PrivateKey) (*fakeApple, *httptest.Server) {
	f := &fakeApple{t: t, key: key, codes: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeApple) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("client_id") != testClientID {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
		return
	}
	secret := r.PostForm.Get("client_secret")
	f.secrets = append(f.secrets, secret)
	if _, err := jwt.Parse(secret, func(*jwt.Token) (any, error) { return &f.key.PublicKey, nil },
		jwt.WithValidMethods([]string{"ES256"})); err != nil {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
		return
	}
	switch r.URL.Path {
	case "/auth/token":
		if f.tokenFail != 0 {
			http.Error(w, `{"error":"invalid_grant","refresh_token":"leak-me"}`, f.tokenFail)
			return
		}
		code := r.PostForm.Get("code")
		token, ok := f.codes[code]
		if r.PostForm.Get("grant_type") != "authorization_code" || !ok {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		delete(f.codes, code) // codes are single use
		f.exchanged = append(f.exchanged, code)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_in":3600,"refresh_token":"` + token + `","id_token":"x"}`))
	case "/auth/revoke":
		if f.revokeFail != 0 {
			http.Error(w, `{"error":"invalid_request"}`, f.revokeFail)
			return
		}
		if r.PostForm.Get("token_type_hint") != "refresh_token" {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		f.revoked = append(f.revoked, r.PostForm.Get("token"))
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func generateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func newTestClient(key *ecdsa.PrivateKey, baseURL string, now func() time.Time) *Client {
	return NewClient(Credentials{TeamID: testTeamID, KeyID: testKeyID, ClientID: testClientID, PrivateKey: key},
		ClientOptions{BaseURL: baseURL, Now: now})
}

// memStore is an in-memory Store.
type memStore struct {
	mu     sync.Mutex
	tokens map[string]string
	getErr error
}

func (m *memStore) Put(_ context.Context, userID, sealed string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[userID] = sealed
	return nil
}

func (m *memStore) Get(_ context.Context, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tokens[userID], m.getErr
}

func (m *memStore) PurgeUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, userID)
	return nil
}

type harness struct {
	apple *fakeApple
	store *memStore
	svc   *Service
	logs  *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	key := generateKey(t)
	apple, srv := newFakeApple(t, key)
	store := &memStore{tokens: map[string]string{}}
	var logs bytes.Buffer
	svc, err := NewService(Options{
		Client: newTestClient(key, srv.URL, nil), Store: store, SealKey: testSealKey,
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{apple: apple, store: store, svc: svc, logs: &logs}
}

// assertNoSecrets fails if any secret value reached the logs.
func (h *harness) assertNoSecrets(t *testing.T, secrets ...string) {
	t.Helper()
	secrets = append(secrets, h.apple.secrets...)
	for _, s := range secrets {
		if s != "" && strings.Contains(h.logs.String(), s) {
			t.Errorf("logs contain %q: %s", s, h.logs.String())
		}
	}
}

func TestClientSecretShape(t *testing.T) {
	key := generateKey(t)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	secret, err := newTestClient(key, "", func() time.Time { return now }).ClientSecret()
	if err != nil {
		t.Fatal(err)
	}
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(secret, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{"ES256"}), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("secret does not verify with the key: %v", err)
	}
	if token.Header["alg"] != "ES256" || token.Header["kid"] != testKeyID {
		t.Errorf("header = %v", token.Header)
	}
	if claims.Issuer != testTeamID || claims.Subject != testClientID ||
		len(claims.Audience) != 1 || claims.Audience[0] != "https://appleid.apple.com" {
		t.Errorf("claims = %+v", claims)
	}
	if !claims.IssuedAt.Equal(now) {
		t.Errorf("iat = %v", claims.IssuedAt)
	}
	life := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if life <= 0 || life > 15*time.Minute {
		t.Errorf("lifetime = %v, want a few minutes (Apple allows at most 6 months)", life)
	}

	// Another key's signature doesn't verify.
	other := generateKey(t)
	if _, err := jwt.Parse(secret, func(*jwt.Token) (any, error) { return &other.PublicKey, nil }); err == nil {
		t.Error("secret verified with the wrong key")
	}
}

func TestSaveFromCodeStoresTheSealedRefreshToken(t *testing.T) {
	h := newHarness(t)
	h.apple.codes["code-1"] = "apple-refresh-1"

	h.svc.SaveFromCode(context.Background(), userAda, "code-1")

	sealed := h.store.tokens[userAda]
	if sealed == "" || strings.Contains(sealed, "apple-refresh-1") {
		t.Fatalf("stored = %q, want the token sealed", sealed)
	}
	if got, err := h.svc.box.open(sealed); err != nil || got != "apple-refresh-1" {
		t.Errorf("open = %q, %v", got, err)
	}
	h.assertNoSecrets(t, "code-1", "apple-refresh-1", sealed)
}

func TestSaveFromCodeFailureIsLoggedAndDropped(t *testing.T) {
	h := newHarness(t)
	h.apple.tokenFail = http.StatusBadRequest

	h.svc.SaveFromCode(context.Background(), userAda, "code-1")

	if len(h.store.tokens) != 0 {
		t.Errorf("stored %v", h.store.tokens)
	}
	if !strings.Contains(h.logs.String(), "invalid_grant") {
		t.Errorf("logs = %s, want Apple's error code", h.logs.String())
	}
	h.assertNoSecrets(t, "code-1", "leak-me")
}

func TestRevokeWithStoredToken(t *testing.T) {
	h := newHarness(t)
	h.apple.codes["code-1"] = "apple-refresh-1"
	h.svc.SaveFromCode(context.Background(), userAda, "code-1")

	h.svc.RevokeForDeletion(context.Background(), userAda, "")

	if len(h.apple.revoked) != 1 || h.apple.revoked[0] != "apple-refresh-1" {
		t.Errorf("revoked = %v", h.apple.revoked)
	}
	h.assertNoSecrets(t, "code-1", "apple-refresh-1")
}

func TestRevokeWithFreshCode(t *testing.T) {
	// Signed in before codes were sent: nothing stored, the app sends a code.
	h := newHarness(t)
	h.apple.codes["fresh"] = "apple-refresh-2"

	h.svc.RevokeForDeletion(context.Background(), userAda, "fresh")

	if len(h.apple.exchanged) != 1 || len(h.apple.revoked) != 1 || h.apple.revoked[0] != "apple-refresh-2" {
		t.Errorf("exchanged %v, revoked %v", h.apple.exchanged, h.apple.revoked)
	}
	h.assertNoSecrets(t, "fresh", "apple-refresh-2")
}

func TestRevokeWithStoredTokenAndFreshCode(t *testing.T) {
	h := newHarness(t)
	h.apple.codes["code-1"] = "apple-refresh-1"
	h.apple.codes["fresh"] = "apple-refresh-2"
	h.svc.SaveFromCode(context.Background(), userAda, "code-1")

	h.svc.RevokeForDeletion(context.Background(), userAda, "fresh")

	if strings.Join(h.apple.revoked, ",") != "apple-refresh-1,apple-refresh-2" {
		t.Errorf("revoked = %v", h.apple.revoked)
	}
}

func TestRevokeFailuresNeverPanicOrStop(t *testing.T) {
	h := newHarness(t)
	h.apple.codes["code-1"] = "apple-refresh-1"
	h.svc.SaveFromCode(context.Background(), userAda, "code-1")
	h.apple.revokeFail = http.StatusServiceUnavailable

	h.svc.RevokeForDeletion(context.Background(), userAda, "unknown-code")

	if !strings.Contains(h.logs.String(), "apple token revoke failed") || !strings.Contains(h.logs.String(), "apple code exchange failed at deletion") {
		t.Errorf("logs = %s", h.logs.String())
	}
	h.assertNoSecrets(t, "code-1", "apple-refresh-1", "unknown-code")

	// A store that can't be read, or a token sealed under another key, is
	// logged and skipped.
	h.store.getErr = errors.New("db down")
	h.svc.RevokeForDeletion(context.Background(), userAda, "")
	h.store.getErr = nil
	h.store.tokens[userAda] = "garbage"
	h.svc.RevokeForDeletion(context.Background(), userAda, "")
	if !strings.Contains(h.logs.String(), "apple refresh token unreadable") {
		t.Errorf("logs = %s", h.logs.String())
	}
}

func TestRevokeWithNothingToRevoke(t *testing.T) {
	h := newHarness(t)
	h.svc.RevokeForDeletion(context.Background(), userAda, "")
	if len(h.apple.revoked) != 0 || len(h.apple.secrets) != 0 {
		t.Errorf("called Apple: revoked %v", h.apple.revoked)
	}
}

func TestNotConfiguredIsANoOp(t *testing.T) {
	store := &memStore{tokens: map[string]string{userAda: "sealed"}}
	svc, err := NewService(Options{Store: store})
	if err != nil || svc.Enabled() {
		t.Fatalf("enabled = %v, %v", svc.Enabled(), err)
	}
	svc.SaveFromCode(context.Background(), "bbbbbbbbbbbbbbbbbbbbbbbb", "code")
	svc.RevokeForDeletion(context.Background(), userAda, "code")
	if len(store.tokens) != 1 {
		t.Errorf("store changed: %v", store.tokens)
	}
	var nilSvc *Service
	if nilSvc.Enabled() {
		t.Error("nil service is enabled")
	}
}

func TestNewServiceNeedsASealKey(t *testing.T) {
	if _, err := NewService(Options{Client: newTestClient(generateKey(t), "", nil), Store: &memStore{}}); err == nil {
		t.Error("NewService() with a client and no seal key succeeded")
	}
}

func TestAppleErrorKeepsOnlyACode(t *testing.T) {
	for in, want := range map[string]string{"invalid_grant": "invalid_grant", "eyJhbGciOi.x": "", "": "", strings.Repeat("a", 41): ""} {
		if got := safeCode(in); got != want {
			t.Errorf("safeCode(%q) = %q, want %q", in, got, want)
		}
	}
}
