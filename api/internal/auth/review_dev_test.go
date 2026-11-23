package auth

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

const testDevPassword = "jordan-avery-password"

// newDevAccountServer is a development server: developer sign-in on, and the
// review account set up only when withReview is true.
func newDevAccountServer(t *testing.T, devLogin, withReview bool, burst int) *reviewTestServer {
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
	reviewOpts := ReviewOptions{Seeder: seeder, Now: clock.Now, DevAccounts: devLogin, RateBurst: burst}
	if withReview {
		reviewOpts.Email, reviewOpts.Password = testReviewEmail, testReviewPassword
	}
	opts := HandlerOptions{
		Service: NewService(ServiceOptions{Users: fu, Tokens: tokens, Logger: logger}),
		Tokens:  tokens, Logger: logger, DevLoginEnabled: devLogin,
		Review: NewReviewSignIn(reviewOpts),
	}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(opts).Mount)
	return &reviewTestServer{router: r, users: fu, seeder: seeder, tokens: tokens, clock: clock, logs: &logs}
}

func (s *reviewTestServer) register(t *testing.T, email, password, name string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q,"displayName":%q}`, email, password, name)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/dev/email-account", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func TestDevEmailAccountSignsInAsItsOwnPersonWithEmail(t *testing.T) {
	srv := newDevAccountServer(t, true, true, 0)
	if rec := srv.register(t, "Jordan.Avery@Example.TEST", testDevPassword, " Jordan Avery "); rec.Code != http.StatusNoContent {
		t.Fatalf("register: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec := srv.signIn(t, "10.0.0.1", "jordan.avery@example.test", testDevPassword)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in: status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[SessionResponse](t, rec)
	if body.User.DisplayName != "Jordan Avery" || body.User.PrimaryEmail != "jordan.avery@example.test" {
		t.Errorf("user = %+v; want Jordan Avery with the email", body.User)
	}
	ids, _ := srv.users.ListIdentities(context.Background(), body.User.ID)
	if len(ids) != 1 || ids[0].Provider != users.ProviderReview || ids[0].Subject != ReviewSubject("jordan.avery@example.test") {
		t.Errorf("identities = %+v; want one email identity", ids)
	}
	// Set up like the review account: a household with recipes.
	if len(srv.seeder.calls) != 1 || srv.seeder.calls[0] != body.User.ID {
		t.Errorf("seeder calls = %v", srv.seeder.calls)
	}
	// The review account still works beside it, as itself.
	review := decodeBody[SessionResponse](t, srv.signIn(t, "10.0.0.1", testReviewEmail, testReviewPassword))
	if review.User.ID == body.User.ID || review.User.DisplayName != "App Review" {
		t.Errorf("review account = %+v", review.User)
	}
	// A wrong password gets the one message.
	wantReviewMessage(t, srv.signIn(t, "10.0.0.2", "jordan.avery@example.test", "wrong-password"),
		http.StatusUnauthorized, "sign_in_failed", "That email and password didn’t work.")
	if strings.Contains(srv.logs.String(), testDevPassword) {
		t.Errorf("logs contain the password: %s", srv.logs.String())
	}
}

func TestDevEmailAccountsWorkWithoutTheReviewAccount(t *testing.T) {
	srv := newDevAccountServer(t, true, false, 0)
	if got := decodeBody[OptionsResponse](t, srv.get(t, "/api/v1/auth/options")); !got.ReviewSignIn {
		t.Error("email sign-in should show on a development server")
	}
	if rec := srv.register(t, "sam.lee@example.test", testDevPassword, "Sam Lee"); rec.Code != http.StatusNoContent {
		t.Fatalf("register: status = %d", rec.Code)
	}
	if rec := srv.signIn(t, "10.0.0.1", "sam.lee@example.test", testDevPassword); rec.Code != http.StatusOK {
		t.Fatalf("sign-in: status = %d, body %s", rec.Code, rec.Body.String())
	}
	// With no review account set, an empty password can't match it.
	wantError(t, srv.signIn(t, "10.0.0.1", "nobody@example.test", "x"), http.StatusUnauthorized, "sign_in_failed")
}

func TestDevEmailAccountRouteIsOnlyOnADevelopmentServer(t *testing.T) {
	// Developer sign-in off (production): no route, and the review account alone.
	srv := newDevAccountServer(t, false, true, 0)
	if rec := srv.register(t, "jordan.avery@example.test", testDevPassword, "Jordan Avery"); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want the route to be absent", rec.Code)
	}
	if NewReviewSignIn(ReviewOptions{}) != nil {
		t.Error("no review account and no development accounts should be off")
	}
}

func TestDevEmailAccountRegistrationIsChecked(t *testing.T) {
	srv := newDevAccountServer(t, true, true, 0)
	for name, try := range map[string][3]string{
		"real domain":   {"jordan@gmail.com", testDevPassword, "Jordan Avery"},
		"not an email":  {"jordan", testDevPassword, "Jordan Avery"},
		"short":         {"jordan@example.test", "short", "Jordan Avery"},
		"no name":       {"jordan@example.test", testDevPassword, "  "},
		"lookalike tld": {"jordan@example.testing", testDevPassword, "Jordan Avery"},
	} {
		if rec := srv.register(t, try[0], try[1], try[2]); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	if rec := srv.register(t, testReviewEmail, testDevPassword, "Someone"); rec.Code != http.StatusConflict {
		t.Errorf("the review account's email: status = %d, want 409", rec.Code)
	}
	// The review account's password is unchanged.
	if rec := srv.signIn(t, "10.0.0.1", testReviewEmail, testReviewPassword); rec.Code != http.StatusOK {
		t.Errorf("review sign-in: status = %d", rec.Code)
	}
}

func TestDevEmailAccountsTakeTheConfiguredBurst(t *testing.T) {
	// Ten simulators on one Mac all sign in from localhost.
	srv := newDevAccountServer(t, true, false, 20)
	srv.register(t, "maya.rivera@example.test", testDevPassword, "Maya Rivera")
	for i := range 10 {
		if rec := srv.signIn(t, "127.0.0.1", "maya.rivera@example.test", testDevPassword); rec.Code != http.StatusOK {
			t.Fatalf("sign-in %d: status = %d", i, rec.Code)
		}
	}
}

func TestProviderNames(t *testing.T) {
	for p, want := range map[users.Provider]string{
		users.ProviderApple: "Apple", users.ProviderGoogle: "Google", users.ProviderReview: "Email", users.ProviderDev: "Developer",
	} {
		if got := providerName(p); got != want {
			t.Errorf("providerName(%q) = %q, want %q", p, got, want)
		}
	}
}
