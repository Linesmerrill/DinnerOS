package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

var (
	testKeyA = sync.OnceValue(func() *rsa.PrivateKey { return mustRSAKey() })
	testKeyB = sync.OnceValue(func() *rsa.PrivateKey { return mustRSAKey() })
)

func mustRSAKey() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
}

var testNow = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// fakeClock is a mutable clock safe for concurrent reads.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: testNow} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// testIdP serves a JWKS document whose keys tests can rotate.
type testIdP struct {
	server  *httptest.Server
	fetches atomic.Int32

	mu     sync.Mutex
	keys   map[string]*rsa.PrivateKey
	status int
}

func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	p := &testIdP{keys: map[string]*rsa.PrivateKey{"key-a": testKeyA()}, status: http.StatusOK}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.fetches.Add(1)
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.status != http.StatusOK {
			w.WriteHeader(p.status)
			return
		}
		doc := map[string]any{"keys": []map[string]string{}}
		entries := []map[string]string{}
		for kid, key := range p.keys {
			entries = append(entries, map[string]string{
				"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			})
		}
		// An unrelated EC key must be ignored, not break parsing.
		entries = append(entries, map[string]string{"kty": "EC", "kid": "ec", "crv": "P-256"})
		doc["keys"] = entries
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *testIdP) setKeys(keys map[string]*rsa.PrivateKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = keys
}

func (p *testIdP) setStatus(status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = status
}

func (p *testIdP) jwks(clock *fakeClock) *JWKSClient {
	return NewJWKSClient(p.server.URL, JWKSOptions{HTTPClient: p.server.Client(), Now: clock.Now})
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	s, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func hashNonce(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

const (
	testBundleID = "com.linesmerrill.dinneros"
	testClientID = "123-abc.apps.googleusercontent.com"
	testNonce    = "raw-nonce-0f9e8d7c"
)

func appleClaims(mutate func(jwt.MapClaims)) jwt.MapClaims {
	c := jwt.MapClaims{
		"iss":            AppleIssuer,
		"aud":            testBundleID,
		"sub":            "001234.abcdef.0123",
		"iat":            testNow.Add(-time.Minute).Unix(),
		"exp":            testNow.Add(10 * time.Minute).Unix(),
		"nonce":          hashNonce(testNonce),
		"email":          "Ada@PrivateRelay.AppleID.com",
		"email_verified": "true",
	}
	if mutate != nil {
		mutate(c)
	}
	return c
}

func TestAppleVerifier(t *testing.T) {
	idp := newTestIdP(t)
	clock := newFakeClock()
	verifier := NewAppleVerifier(testBundleID, VerifierOptions{Keys: idp.jwks(clock), Now: clock.Now})

	hsToken := func() string {
		// Algorithm confusion: an HMAC token keyed with the RSA public key.
		pub, _ := x509.MarshalPKIXPublicKey(&testKeyA().PublicKey)
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, appleClaims(nil))
		tok.Header["kid"] = "key-a"
		s, err := tok.SignedString(pub)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	noneToken := func() string {
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, appleClaims(nil))
		tok.Header["kid"] = "key-a"
		s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	tests := []struct {
		name      string
		token     string
		nonce     string
		wantErr   error
		wantEmail string
	}{
		{name: "valid with string email_verified", token: signRS256(t, testKeyA(), "key-a", appleClaims(nil)), nonce: testNonce, wantEmail: "ada@privaterelay.appleid.com"},
		{name: "valid with bool email_verified", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["email_verified"] = true })), nonce: testNonce, wantEmail: "ada@privaterelay.appleid.com"},
		{name: "email_verified string false drops email", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["email_verified"] = "false" })), nonce: testNonce},
		{name: "email_verified bool false drops email", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["email_verified"] = false })), nonce: testNonce},
		{name: "missing email_verified drops email", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { delete(c, "email_verified") })), nonce: testNonce},
		{name: "exp within clock skew accepted", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["exp"] = testNow.Add(-30 * time.Second).Unix() })), nonce: testNonce, wantEmail: "ada@privaterelay.appleid.com"},
		{name: "wrong audience", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["aud"] = "com.example.other" })), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "wrong issuer", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["iss"] = "https://accounts.google.com" })), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "expired", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["exp"] = testNow.Add(-5 * time.Minute).Unix() })), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "missing exp", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { delete(c, "exp") })), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "issued in the future", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["iat"] = testNow.Add(5 * time.Minute).Unix() })), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "missing iat", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { delete(c, "iat") })), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "missing sub", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { delete(c, "sub") })), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "nonce mismatch", token: signRS256(t, testKeyA(), "key-a", appleClaims(nil)), nonce: "some-other-nonce", wantErr: ErrInvalidIdentityToken},
		{name: "raw nonce in claim is not accepted", token: signRS256(t, testKeyA(), "key-a", appleClaims(func(c jwt.MapClaims) { c["nonce"] = testNonce })), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "nonce required", token: signRS256(t, testKeyA(), "key-a", appleClaims(nil)), nonce: "", wantErr: ErrInvalidIdentityToken},
		{name: "bad signature", token: signRS256(t, testKeyB(), "key-a", appleClaims(nil)), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "missing kid", token: signRS256(t, testKeyA(), "", appleClaims(nil)), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "alg confusion HS256 rejected", token: hsToken(), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "alg none rejected", token: noneToken(), nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "garbage", token: "not.a.jwt", nonce: testNonce, wantErr: ErrInvalidIdentityToken},
		{name: "empty", token: "", nonce: testNonce, wantErr: ErrInvalidIdentityToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := verifier.Verify(context.Background(), tt.token, tt.nonce)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			if got.Provider != users.ProviderApple || got.Subject != "001234.abcdef.0123" {
				t.Errorf("identity = %+v", got)
			}
			if got.Email != tt.wantEmail || got.EmailVerified != (tt.wantEmail != "") {
				t.Errorf("email = %q verified=%v, want %q", got.Email, got.EmailVerified, tt.wantEmail)
			}
		})
	}
}

func googleClaims(mutate func(jwt.MapClaims)) jwt.MapClaims {
	c := jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"azp":            testClientID,
		"sub":            "110169484474386276334",
		"iat":            testNow.Add(-time.Minute).Unix(),
		"exp":            testNow.Add(time.Hour).Unix(),
		"email":          "ada@gmail.com",
		"email_verified": true,
		"name":           "Ada Lovelace",
	}
	if mutate != nil {
		mutate(c)
	}
	return c
}

func TestGoogleVerifier(t *testing.T) {
	idp := newTestIdP(t)
	clock := newFakeClock()
	verifier := NewGoogleVerifier(testClientID, VerifierOptions{Keys: idp.jwks(clock), Now: clock.Now})

	tests := []struct {
		name      string
		claims    jwt.MapClaims
		nonce     string
		wantErr   bool
		wantEmail string
	}{
		{name: "valid https issuer", claims: googleClaims(nil), wantEmail: "ada@gmail.com"},
		{name: "valid bare issuer", claims: googleClaims(func(c jwt.MapClaims) { c["iss"] = "accounts.google.com" }), wantEmail: "ada@gmail.com"},
		{name: "email_verified string true", claims: googleClaims(func(c jwt.MapClaims) { c["email_verified"] = "true" }), wantEmail: "ada@gmail.com"},
		{name: "unverified email dropped", claims: googleClaims(func(c jwt.MapClaims) { c["email_verified"] = false })},
		{name: "matching nonce", claims: googleClaims(func(c jwt.MapClaims) { c["nonce"] = testNonce }), nonce: testNonce, wantEmail: "ada@gmail.com"},
		{name: "nonce mismatch", claims: googleClaims(func(c jwt.MapClaims) { c["nonce"] = "other" }), nonce: testNonce, wantErr: true},
		{name: "nonce supplied but absent from token", claims: googleClaims(nil), nonce: testNonce, wantErr: true},
		{name: "wrong issuer", claims: googleClaims(func(c jwt.MapClaims) { c["iss"] = "https://accounts.google.com.evil.example" }), wantErr: true},
		{name: "apple issuer rejected", claims: googleClaims(func(c jwt.MapClaims) { c["iss"] = AppleIssuer }), wantErr: true},
		{name: "wrong audience", claims: googleClaims(func(c jwt.MapClaims) { c["aud"] = "other.apps.googleusercontent.com" }), wantErr: true},
		{name: "expired", claims: googleClaims(func(c jwt.MapClaims) { c["exp"] = testNow.Add(-time.Hour).Unix() }), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := verifier.Verify(context.Background(), signRS256(t, testKeyA(), "key-a", tt.claims), tt.nonce)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidIdentityToken) {
					t.Fatalf("Verify() error = %v, want ErrInvalidIdentityToken", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			if got.Provider != users.ProviderGoogle || got.Subject != "110169484474386276334" || got.DisplayName != "Ada Lovelace" {
				t.Errorf("identity = %+v", got)
			}
			if got.Email != tt.wantEmail || got.EmailVerified != (tt.wantEmail != "") {
				t.Errorf("email = %q verified=%v, want %q", got.Email, got.EmailVerified, tt.wantEmail)
			}
		})
	}
}

func TestJWKSUnknownKidRefetchIsRateLimited(t *testing.T) {
	idp := newTestIdP(t)
	clock := newFakeClock()
	verifier := NewAppleVerifier(testBundleID, VerifierOptions{Keys: idp.jwks(clock), Now: clock.Now})
	ctx := context.Background()

	if _, err := verifier.Verify(ctx, signRS256(t, testKeyA(), "key-a", appleClaims(nil)), testNonce); err != nil {
		t.Fatalf("initial Verify() error = %v", err)
	}
	if got := idp.fetches.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}

	// Cached: no refetch for a known kid.
	if _, err := verifier.Verify(ctx, signRS256(t, testKeyA(), "key-a", appleClaims(nil)), testNonce); err != nil {
		t.Fatal(err)
	}
	if got := idp.fetches.Load(); got != 1 {
		t.Fatalf("fetches after cached verify = %d, want 1", got)
	}

	// Provider rotates to key B. The unknown kid triggers a refetch once the
	// minimum refetch interval has passed.
	idp.setKeys(map[string]*rsa.PrivateKey{"key-b": testKeyB()})
	clock.Advance(2 * time.Minute)
	if _, err := verifier.Verify(ctx, signRS256(t, testKeyB(), "key-b", appleClaims(nil)), testNonce); err != nil {
		t.Fatalf("Verify(rotated key) error = %v", err)
	}
	if got := idp.fetches.Load(); got != 2 {
		t.Fatalf("fetches after rotation = %d, want 2", got)
	}

	// A burst of bogus kids within the interval does not hit the provider.
	for range 20 {
		_, err := verifier.Verify(ctx, signRS256(t, testKeyA(), "bogus", appleClaims(nil)), testNonce)
		if !errors.Is(err, ErrInvalidIdentityToken) || !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("Verify(bogus kid) error = %v, want invalid token / unknown key", err)
		}
	}
	if got := idp.fetches.Load(); got != 2 {
		t.Fatalf("fetches after bogus kids = %d, want 2 (rate limited)", got)
	}

	clock.Advance(2 * time.Minute)
	_, _ = verifier.Verify(ctx, signRS256(t, testKeyA(), "bogus", appleClaims(nil)), testNonce)
	if got := idp.fetches.Load(); got != 3 {
		t.Fatalf("fetches after interval = %d, want 3", got)
	}
}

func TestJWKSCacheTTLAndOutage(t *testing.T) {
	idp := newTestIdP(t)
	clock := newFakeClock()
	keys := idp.jwks(clock)
	ctx := context.Background()

	if _, err := keys.Key(ctx, "key-a"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(59 * time.Minute)
	if _, err := keys.Key(ctx, "key-a"); err != nil || idp.fetches.Load() != 1 {
		t.Fatalf("within TTL: err=%v fetches=%d, want cached", err, idp.fetches.Load())
	}

	// After the TTL the cache refreshes; if the provider is down the stale key
	// is still served.
	idp.setStatus(http.StatusInternalServerError)
	clock.Advance(2 * time.Minute)
	if _, err := keys.Key(ctx, "key-a"); err != nil {
		t.Fatalf("stale key during outage: %v", err)
	}
	if got := idp.fetches.Load(); got != 2 {
		t.Fatalf("fetches = %d, want 2", got)
	}
}

func TestJWKSProviderUnavailable(t *testing.T) {
	idp := newTestIdP(t)
	idp.setStatus(http.StatusServiceUnavailable)
	clock := newFakeClock()
	verifier := NewAppleVerifier(testBundleID, VerifierOptions{Keys: idp.jwks(clock), Now: clock.Now})

	for range 2 { // the second call is inside the refetch interval
		_, err := verifier.Verify(context.Background(), signRS256(t, testKeyA(), "key-a", appleClaims(nil)), testNonce)
		if !errors.Is(err, ErrKeysUnavailable) || errors.Is(err, ErrInvalidIdentityToken) {
			t.Fatalf("Verify() error = %v, want ErrKeysUnavailable only", err)
		}
	}
	if got := idp.fetches.Load(); got != 1 {
		t.Errorf("fetches = %d, want 1", got)
	}
}

func TestFlexibleBool(t *testing.T) {
	tests := map[string]bool{`true`: true, `false`: false, `"true"`: true, `"TRUE"`: true, `"false"`: false, `null`: false, `"yes"`: false}
	for input, want := range tests {
		var b flexibleBool
		if err := json.Unmarshal([]byte(input), &b); err != nil {
			t.Errorf("unmarshal %s: %v", input, err)
			continue
		}
		if bool(b) != want {
			t.Errorf("unmarshal %s = %v, want %v", input, b, want)
		}
	}
	var b flexibleBool
	if err := json.Unmarshal([]byte(`1`), &b); err == nil {
		t.Error("unmarshal 1: want error")
	}
}
