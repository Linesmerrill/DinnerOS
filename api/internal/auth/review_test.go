package auth

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

const (
	testReviewEmail    = "review@example.test"
	testReviewPassword = "plain-test-password-0123"
)

type fakeSeeder struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeSeeder) ReviewAccountSignedIn(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, userID)
	return f.err
}

type reviewTestServer struct {
	router *chi.Mux
	users  *fakeUsers
	seeder *fakeSeeder
	tokens *TokenService
	clock  *fakeClock
	logs   *bytes.Buffer
}

func newReviewTestServer(t *testing.T, enabled bool) *reviewTestServer {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	clock := newFakeClock()
	tokens, err := NewTokenService(&memorySessionStore{}, TokenOptions{SigningKey: testSigningKey, Now: clock.Now, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	fu := &fakeUsers{users: map[string]users.User{}}
	seeder := &fakeSeeder{}
	opts := HandlerOptions{
		Service: NewService(ServiceOptions{Users: fu, Tokens: tokens, Logger: logger}),
		Tokens:  tokens, Logger: logger,
	}
	if enabled {
		opts.Review = NewReviewSignIn(ReviewOptions{Email: testReviewEmail, Password: testReviewPassword, Seeder: seeder, Now: clock.Now})
	}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(opts).Mount)
	return &reviewTestServer{router: r, users: fu, seeder: seeder, tokens: tokens, clock: clock, logs: &logs}
}

func (s *reviewTestServer) signIn(t *testing.T, ip, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/review-sign-in", strings.NewReader(body))
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func (s *reviewTestServer) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func wantReviewMessage(t *testing.T, rec *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	wantError(t, rec, status, code)
	if got := decodeBody[httpx.ErrorResponse](t, rec).Error.Message; got != message {
		t.Errorf("message = %q, want %q", got, message)
	}
}

func TestAuthOptionsSaysWhetherReviewSignInIsOn(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		srv := newReviewTestServer(t, enabled)
		rec := srv.get(t, "/api/v1/auth/options")
		if rec.Code != http.StatusOK {
			t.Fatalf("enabled=%v: status = %d", enabled, rec.Code)
		}
		if got := decodeBody[OptionsResponse](t, rec); got.ReviewSignIn != enabled {
			t.Errorf("enabled=%v: reviewSignIn = %v", enabled, got.ReviewSignIn)
		}
	}
}

func TestReviewSignInIsNotMountedWhenOff(t *testing.T) {
	srv := newReviewTestServer(t, false)
	if rec := srv.signIn(t, "10.0.0.1", testReviewEmail, testReviewPassword); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want the route to be absent", rec.Code)
	}
	if NewReviewSignIn(ReviewOptions{Email: testReviewEmail}) != nil || NewReviewSignIn(ReviewOptions{Password: testReviewPassword}) != nil {
		t.Error("a half-configured review account should be off")
	}
}

func TestReviewSignInRightEmailAndPassword(t *testing.T) {
	srv := newReviewTestServer(t, true)
	// The email is matched without case or surrounding spaces.
	rec := srv.signIn(t, "10.0.0.1", "  Review@Example.TEST ", testReviewPassword)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[SessionResponse](t, rec)
	if !body.IsNewUser || body.User.DisplayName != "App Review" || body.User.PrimaryEmail != testReviewEmail {
		t.Errorf("body = %+v", body)
	}
	if userID, err := srv.tokens.ValidateAccessToken(body.AccessToken); err != nil || userID != body.User.ID {
		t.Errorf("access token sub = %q, %v; want %q", userID, err, body.User.ID)
	}
	ids, _ := srv.users.ListIdentities(context.Background(), body.User.ID)
	if len(ids) != 1 || ids[0].Provider != users.ProviderReview || ids[0].Subject != ReviewSubject(testReviewEmail) || strings.Contains(ids[0].Subject, "review@") {
		t.Errorf("identities = %+v; want one review identity keyed by a subject derived from the email", ids)
	}

	again := decodeBody[SessionResponse](t, srv.signIn(t, "10.0.0.1", testReviewEmail, testReviewPassword))
	if again.IsNewUser || again.User.ID != body.User.ID {
		t.Errorf("second sign-in = %+v; want the same user", again)
	}
	if len(srv.seeder.calls) != 2 || srv.seeder.calls[0] != body.User.ID {
		t.Errorf("seeder calls = %v; want one per sign-in for %s", srv.seeder.calls, body.User.ID)
	}
	for _, secret := range []string{testReviewPassword, testReviewEmail, body.AccessToken, body.RefreshToken} {
		if strings.Contains(srv.logs.String(), secret) {
			t.Errorf("logs contain %q: %s", secret, srv.logs.String())
		}
	}
}

func TestReviewSignInWrongEmailOrPasswordGetsOneMessage(t *testing.T) {
	srv := newReviewTestServer(t, true)
	for name, try := range map[string][2]string{
		"other email":     {"someone@example.test", testReviewPassword},
		"wrong password":  {testReviewEmail, testReviewPassword + "x"},
		"password's case": {testReviewEmail, strings.ToUpper(testReviewPassword)},
		"too long":        {testReviewEmail, strings.Repeat("a", 300)},
	} {
		rec := srv.signIn(t, "10.0.0."+fmt.Sprint(len(name)), try[0], try[1])
		wantReviewMessage(t, rec, http.StatusUnauthorized, "sign_in_failed", "That email and password didn’t work.")
	}
	if len(srv.users.users) != 0 || len(srv.seeder.calls) != 0 {
		t.Errorf("a failed sign-in made a user (%d) or seeded (%d)", len(srv.users.users), len(srv.seeder.calls))
	}
	if strings.Contains(srv.logs.String(), testReviewPassword) || strings.Contains(srv.logs.String(), "someone@") {
		t.Errorf("logs name what was typed: %s", srv.logs.String())
	}
	wantReviewMessage(t, srv.signIn(t, "10.0.1.1", "", ""), http.StatusBadRequest, "validation_failed", "Enter your email and password.")
}

func TestReviewSignInLocksOutAfterRepeatedFailures(t *testing.T) {
	srv := newReviewTestServer(t, true)
	ip := "10.0.0.9"
	for i := range reviewLockAfter {
		// Past the rate limit's burst, a minute between tries keeps it from
		// answering first.
		srv.clock.Advance(time.Minute)
		wantError(t, srv.signIn(t, ip, testReviewEmail, "wrong-"+fmt.Sprint(i)), http.StatusUnauthorized, "sign_in_failed")
	}
	srv.clock.Advance(time.Minute)
	// Locked: even the right password is turned away, without being checked.
	wantReviewMessage(t, srv.signIn(t, ip, testReviewEmail, testReviewPassword), http.StatusTooManyRequests, "rate_limited",
		"Too many tries. Wait 15 minutes and try again.")
	// Another address isn't locked by this one.
	if rec := srv.signIn(t, "10.0.0.10", testReviewEmail, testReviewPassword); rec.Code != http.StatusOK {
		t.Fatalf("other address: status = %d", rec.Code)
	}
	srv.clock.Advance(reviewLockFor)
	if rec := srv.signIn(t, ip, testReviewEmail, testReviewPassword); rec.Code != http.StatusOK {
		t.Fatalf("after the lock: status = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestReviewSignInLocksEveryoneAfterManyFailures(t *testing.T) {
	srv := newReviewTestServer(t, true)
	for i := range reviewGlobalLockAfter {
		wantError(t, srv.signIn(t, fmt.Sprintf("10.1.%d.%d", i/200, i%200), testReviewEmail, "wrong"), http.StatusUnauthorized, "sign_in_failed")
	}
	wantError(t, srv.signIn(t, "10.2.0.1", testReviewEmail, testReviewPassword), http.StatusTooManyRequests, "rate_limited")
}

func TestReviewSignInRateLimitsOneAddress(t *testing.T) {
	srv := newReviewTestServer(t, true)
	for range reviewRateBurst {
		if rec := srv.signIn(t, "10.0.0.7", testReviewEmail, testReviewPassword); rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	wantError(t, srv.signIn(t, "10.0.0.7", testReviewEmail, testReviewPassword), http.StatusTooManyRequests, "rate_limited")
}

func TestReviewSignInFailsWhenSetupFails(t *testing.T) {
	srv := newReviewTestServer(t, true)
	srv.seeder.err = errors.New("mongo down")
	wantError(t, srv.signIn(t, "10.0.0.1", testReviewEmail, testReviewPassword), http.StatusInternalServerError, "internal")
	srv.seeder.err = nil
	if rec := srv.signIn(t, "10.0.0.1", testReviewEmail, testReviewPassword); rec.Code != http.StatusOK {
		t.Fatalf("retry: status = %d", rec.Code)
	}
}
