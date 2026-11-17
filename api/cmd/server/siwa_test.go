package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/account"
	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/siwa"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// appleStandIn answers Apple's token and revoke endpoints: every code is
// worth "refresh-for-<code>", and revoked tokens are recorded.
type appleStandIn struct {
	mu      sync.Mutex
	revoked []string
	down    bool
}

func (a *appleStandIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.down {
		http.Error(w, `{"error":"server_error"}`, http.StatusServiceUnavailable)
		return
	}
	_ = r.ParseForm()
	switch r.URL.Path {
	case "/auth/token":
		_, _ = w.Write([]byte(`{"refresh_token":"refresh-for-` + r.PostForm.Get("code") + `"}`))
	case "/auth/revoke":
		a.revoked = append(a.revoked, r.PostForm.Get("token"))
	}
}

func TestIntegrationAccountDeletionRevokesSignInWithApple(t *testing.T) {
	client := mongotest.Client(t)
	db := client.Database()
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, indexSets()...); err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	apple := &appleStandIn{}
	appleSrv := httptest.NewServer(apple)
	defer appleSrv.Close()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	svc, err := siwa.NewService(siwa.Options{
		Client: siwa.NewClient(siwa.Credentials{TeamID: "6VTPDG2HNK", KeyID: "SIWAKEY123", ClientID: config.DefaultAppleBundleID, PrivateKey: key},
			siwa.ClientOptions{BaseURL: appleSrv.URL}),
		Store: siwa.NewMongoStore(db), SealKey: []byte("0123456789abcdef0123456789abcdef"), Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	userStore := users.NewMongoStore(db)
	householdService := households.NewService(households.ServiceOptions{Store: households.NewMongoStore(db), Users: userStore})
	tokens, err := auth.NewTokenService(auth.NewMongoSessionStore(db), auth.TokenOptions{SigningKey: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	account.NewHandler(account.HandlerOptions{
		Service: newAccountService(db, householdService, logger), Tokens: tokens, Logger: logger, Apple: svc,
	}).Mount(r)

	newUser := func(name string) (string, string) {
		now := time.Now().UTC()
		u, err := userStore.CreateUser(ctx, users.User{DisplayName: name, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		pair, err := tokens.IssueForNewSignIn(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return u.ID, pair.AccessToken
	}
	deleteMe := func(access, body string) int {
		req := httptest.NewRequest(http.MethodDelete, "/me", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+access)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	stored := func(userID string) int64 {
		id, _ := bson.ObjectIDFromHex(userID)
		n, err := db.Collection(siwa.Collection).CountDocuments(ctx, bson.D{{Key: "userId", Value: id}})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Signed in with a code: the stored token is revoked and deleted.
	ada, adaAccess := newUser("Ada")
	svc.SaveFromCode(ctx, ada, "ada-code")
	if stored(ada) != 1 {
		t.Fatal("no token stored at sign-in")
	}
	if code := deleteMe(adaAccess, ""); code != http.StatusNoContent {
		t.Fatalf("delete status = %d", code)
	}
	if strings.Join(apple.revoked, ",") != "refresh-for-ada-code" || stored(ada) != 0 {
		t.Errorf("revoked %v, stored %d", apple.revoked, stored(ada))
	}

	// Signed in before codes were sent: the fresh code is exchanged and revoked.
	bob, bobAccess := newUser("Bob")
	if code := deleteMe(bobAccess, `{"appleAuthorizationCode":"bob-fresh"}`); code != http.StatusNoContent {
		t.Fatalf("delete status = %d", code)
	}
	if apple.revoked[len(apple.revoked)-1] != "refresh-for-bob-fresh" {
		t.Errorf("revoked %v", apple.revoked)
	}
	if _, err := userStore.GetUser(ctx, bob); err == nil {
		t.Error("Bob still exists")
	}

	// Apple down: the account is deleted anyway.
	cy, cyAccess := newUser("Cy")
	svc.SaveFromCode(ctx, cy, "cy-code")
	apple.mu.Lock()
	apple.down = true
	apple.mu.Unlock()
	if code := deleteMe(cyAccess, `{"appleAuthorizationCode":"cy-fresh"}`); code != http.StatusNoContent {
		t.Fatalf("delete status with Apple down = %d", code)
	}
	if _, err := userStore.GetUser(ctx, cy); err == nil || stored(cy) != 0 {
		t.Errorf("Cy left behind: %v, stored %d", err, stored(cy))
	}

	for _, secret := range []string{"ada-code", "refresh-for", "bob-fresh", "cy-fresh"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("logs contain %q", secret)
		}
	}
}

func TestIntegrationAppleRevocationIsOffWithoutAKey(t *testing.T) {
	var logs bytes.Buffer
	svc, err := newAppleRevocation(config.Config{}, mongotest.Client(t).Database(), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil || svc.Enabled() {
		t.Fatalf("enabled = %v, %v", svc.Enabled(), err)
	}
	if !strings.Contains(logs.String(), "not revoked") {
		t.Errorf("logs = %s", logs.String())
	}
}
