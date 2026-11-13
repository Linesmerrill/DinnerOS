package plus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

const hh = "66e5a1f2c3b4a5d6e7f80a01"

type memoryStore struct {
	mu sync.Mutex
	m  map[string]Entitlement
}

func newMemoryStore() *memoryStore { return &memoryStore{m: map[string]Entitlement{}} }

func (s *memoryStore) Get(_ context.Context, id string) (Entitlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[id]
	if !ok {
		return Entitlement{}, ErrNotFound
	}
	return e, nil
}

func (s *memoryStore) Put(_ context.Context, e Entitlement) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.m[e.HouseholdID]; ok && old.LastEventAt.After(e.LastEventAt) {
		return false, nil
	}
	s.m[e.HouseholdID] = e
	return true, nil
}

func (s *memoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}

func ms(t time.Time) *int64 { v := t.UnixMilli(); return &v }

func event(id, typ string, at, expires time.Time) Event {
	return Event{
		ID: id, Type: typ, AppUserID: hh, ProductID: "dinneros_plus_monthly", PeriodType: "NORMAL",
		EntitlementIDs: []string{"plus"}, ExpirationAtMs: ms(expires), EventTimestampMs: at.UnixMilli(),
		Environment: "SANDBOX",
	}
}

// A trial, a renewal, turning off renewal, and the end: Plus follows each
// event, and an older or repeated event never undoes a newer one.
func TestPlusFollowsTheSubscription(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s := NewService(newMemoryStore(), true)
	s.now = func() time.Time { return now }

	if st, _ := s.Status(ctx, hh); !st.Enabled || st.Active {
		t.Fatalf("before = %+v", st)
	}
	if ok, _ := s.HasPlus(ctx, hh); ok {
		t.Error("has Plus before subscribing")
	}

	trial := event("e1", "INITIAL_PURCHASE", now, now.Add(14*24*time.Hour))
	trial.PeriodType = "TRIAL"
	if out, err := s.Apply(ctx, trial); err != nil || out != OutcomeApplied {
		t.Fatalf("trial = %v, %v", out, err)
	}
	st, _ := s.Status(ctx, hh)
	if !st.Active || !st.Trial || !st.WillRenew || !st.ExpiresAt.Equal(now.Add(14*24*time.Hour)) {
		t.Errorf("in trial = %+v", st)
	}
	// The same event delivered twice changes nothing.
	if out, _ := s.Apply(ctx, trial); out != OutcomeStale {
		t.Errorf("repeat = %v", out)
	}

	renewed := event("e2", "RENEWAL", now.Add(14*24*time.Hour), now.Add(44*24*time.Hour))
	if out, _ := s.Apply(ctx, renewed); out != OutcomeApplied {
		t.Fatalf("renewal = %v", out)
	}
	// A cancellation that arrives late, from before the renewal, is ignored.
	late := event("e0", "CANCELLATION", now.Add(time.Hour), now.Add(14*24*time.Hour))
	if out, _ := s.Apply(ctx, late); out != OutcomeStale {
		t.Errorf("late cancellation = %v", out)
	}
	if st, _ := s.Status(ctx, hh); st.Trial || !st.WillRenew {
		t.Errorf("after renewal = %+v", st)
	}

	// Renewal turned off: still Plus until it runs out.
	cancel := event("e3", "CANCELLATION", now.Add(20*24*time.Hour), now.Add(44*24*time.Hour))
	if _, err := s.Apply(ctx, cancel); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(ctx, hh); !st.Active || st.WillRenew {
		t.Errorf("cancelled = %+v", st)
	}
	s.now = func() time.Time { return now.Add(45 * 24 * time.Hour) }
	if st, _ := s.Status(ctx, hh); st.Active {
		t.Errorf("past expiry = %+v", st)
	}

	// EXPIRATION ends it whatever the date says.
	s.now = func() time.Time { return now.Add(21 * 24 * time.Hour) }
	if _, err := s.Apply(ctx, event("e4", "EXPIRATION", now.Add(21*24*time.Hour), now.Add(44*24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.HasPlus(ctx, hh); ok {
		t.Error("still Plus after EXPIRATION")
	}
}

func TestPlusIgnoresWhatIsntAboutAHousehold(t *testing.T) {
	ctx := t.Context()
	s := NewService(newMemoryStore(), true)
	now := time.Now()
	for name, ev := range map[string]Event{
		"test": {ID: "t", Type: "TEST", AppUserID: hh},
		"another entitlement": func() Event {
			e := event("x", "INITIAL_PURCHASE", now, now.Add(time.Hour))
			e.EntitlementIDs = []string{"pro"}
			return e
		}(),
		"an unknown type": event("u", "SOMETHING_NEW", now, now.Add(time.Hour)),
	} {
		if out, err := s.Apply(ctx, ev); err != nil || out != OutcomeIgnored {
			t.Errorf("%s = %v, %v", name, out, err)
		}
	}
	anon := event("a", "INITIAL_PURCHASE", now, now.Add(time.Hour))
	anon.AppUserID = "$RCAnonymousID:abc"
	if out, _ := s.Apply(ctx, anon); out != OutcomeNoHousehold {
		t.Errorf("anonymous = %v", out)
	}
	// A household named only in the aliases is found.
	anon.Aliases = []string{"$RCAnonymousID:abc", hh}
	if out, _ := s.Apply(ctx, anon); out != OutcomeApplied {
		t.Errorf("alias = %v", out)
	}
}

// While Plus is switched off, everything is open and no paywall shows.
func TestPlusSwitchedOffOpensEverything(t *testing.T) {
	s := NewService(newMemoryStore(), false)
	st, _ := s.Status(t.Context(), hh)
	ok, _ := s.HasPlus(t.Context(), hh)
	if st.Enabled || !ok {
		t.Errorf("off = %+v, %v", st, ok)
	}
}

type fakeAuthorizer struct{}

func (fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, _ households.Permission) (households.Membership, error) {
	if householdID != hh || userID != "u1" {
		return households.Membership{}, households.ErrNotFound
	}
	return households.Membership{HouseholdID: hh, UserID: userID, Role: households.RoleMember}, nil
}

type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok {
		return id, nil
	}
	return "", errors.New("bad token")
}

func TestPlusRoutes(t *testing.T) {
	s := NewService(newMemoryStore(), true)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: s, Authorizer: fakeAuthorizer{}, Tokens: fakeTokens{}, WebhookSecret: "whsec-test",
	}).Mount)
	do := func(method, path, auth, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	now := time.Now()
	raw, _ := json.Marshal(map[string]any{"api_version": "1.0", "event": event("w1", "INITIAL_PURCHASE", now, now.Add(time.Hour))})
	if rec := do(http.MethodPost, "/webhooks/revenuecat", "Bearer wrong", string(raw)); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong secret = %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/webhooks/revenuecat", "", string(raw)); rec.Code != http.StatusUnauthorized {
		t.Errorf("no secret = %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/webhooks/revenuecat", "whsec-test", `{"nope":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("not an event = %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/webhooks/revenuecat", "Bearer whsec-test", string(raw)); rec.Code != http.StatusOK {
		t.Fatalf("webhook = %d %s", rec.Code, rec.Body.String())
	}

	rec := do(http.MethodGet, "/households/"+hh+"/plus", "Bearer token-u1", "")
	var st StatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != http.StatusOK || !st.Enabled || !st.Active || st.ExpiresAt == nil {
		t.Errorf("status = %d %+v", rec.Code, st)
	}
	if rec := do(http.MethodGet, "/households/"+hh+"/plus", "Bearer token-u2", ""); rec.Code == http.StatusOK {
		t.Error("an outsider read the household's Plus")
	}

	// No secret configured: the webhook isn't there at all.
	off := chi.NewRouter()
	off.Route("/api/v1", NewHandler(HandlerOptions{Service: s, Authorizer: fakeAuthorizer{}, Tokens: fakeTokens{}}).Mount)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/revenuecat", strings.NewReader(string(raw)))
	rec = httptest.NewRecorder()
	off.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("webhook without a secret = %d", rec.Code)
	}
}
