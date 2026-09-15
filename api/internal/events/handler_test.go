package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/ratelimit"
)

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer grants the listed permissions per (household, user); unknown
// pairs are ErrNotFound, like households.Service.Authorize.
type fakeAuthorizer map[string][]households.Permission

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	perms, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	m := households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
	if !slices.Contains(perms, perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

const userOutsider = "66e5a1f2c3b4a5d6e7f80c21"

type eventTestServer struct {
	router *chi.Mux
	store  *memoryStore
}

func newEventTestServer(t *testing.T, rateLimit func(http.Handler) http.Handler) *eventTestServer {
	t.Helper()
	store := &memoryStore{}
	svc, _ := newTestService(store)
	authz := fakeAuthorizer{
		hhA + "/" + userA:  {households.PermHouseholdView},
		hhA + "/" + userA2: {households.PermHouseholdView},
	}
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: authz, Tokens: fakeTokens{}, RateLimit: rateLimit}).Mount)
	return &eventTestServer{router: r, store: store}
}

func (s *eventTestServer) post(t *testing.T, householdID, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/households/"+householdID+"/events", strings.NewReader(body))
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	var body httpx.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != code {
		t.Fatalf("error = %+v (%v), want code %q", body, err, code)
	}
}

func batchJSON(n int) string {
	items := make([]string, n)
	at := testNow.Add(-time.Minute).Format(time.RFC3339)
	for i := range items {
		items[i] = fmt.Sprintf(`{"type":"recipe.cooked","recipeId":%q,"occurredAt":%q}`, recipeA, at)
	}
	return `{"events":[` + strings.Join(items, ",") + `]}`
}

func TestIngestHandler(t *testing.T) {
	srv := newEventTestServer(t, nil)
	at := testNow.Add(-time.Minute).Format(time.RFC3339)
	body := `{"events":[
		{"clientEventId":"e-1","type":"recipe.cooked","recipeId":"` + recipeA + `","occurredAt":"` + at + `","payload":{"servings":2}},
		{"clientEventId":"e-1","type":"recipe.cooked","recipeId":"` + recipeA + `","occurredAt":"` + at + `","payload":{"servings":2}},
		{"type":"recipe.rated","recipeId":"` + recipeA + `","occurredAt":"` + at + `","payload":{"score":5}}
	]}`
	rec := srv.post(t, hhA, body, userA)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp IngestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Accepted != 1 || resp.Duplicates != 1 || len(resp.Rejected) != 1 || resp.Rejected[0].Index != 2 {
		t.Fatalf("response = %s", rec.Body.String())
	}
	if e := srv.store.all()[0]; e.UserID != userA || e.HouseholdID != hhA || e.Source != SourceClient {
		t.Errorf("stored = %+v", e)
	}

	// A fully accepted batch still lists rejected as an empty array.
	if rec := srv.post(t, hhA, batchJSON(1), userA2); !strings.Contains(rec.Body.String(), `"rejected":[]`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestIngestHandlerErrors(t *testing.T) {
	srv := newEventTestServer(t, nil)
	tests := []struct {
		name        string
		householdID string
		body        string
		userID      string
		status      int
		code        string
	}{
		{"unauthenticated", hhA, batchJSON(1), "", 401, "unauthenticated"},
		{"non-member", hhA, batchJSON(1), userOutsider, 404, "not_found"},
		{"other household", hhB, batchJSON(1), userA, 404, "not_found"},
		{"malformed household", "nope", batchJSON(1), userA, 404, "not_found"},
		{"empty body", hhA, ``, userA, 400, "invalid_request"},
		{"malformed", hhA, `{"events":[`, userA, 400, "invalid_request"},
		{"unknown field", hhA, `{"events":[],"userId":"x"}`, userA, 400, "invalid_request"},
		{"unknown event field", hhA, `{"events":[{"type":"recipe.cooked","householdId":"x"}]}`, userA, 400, "invalid_request"},
		{"no events", hhA, `{"events":[]}`, userA, 400, "validation_failed"},
		{"missing events", hhA, `{}`, userA, 400, "validation_failed"},
		{"too many events", hhA, batchJSON(MaxBatchSize + 1), userA, 400, "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, srv.post(t, tt.householdID, tt.body, tt.userID), tt.status, tt.code)
		})
	}
	if n := len(srv.store.all()); n != 0 {
		t.Errorf("stored %d events from failed requests", n)
	}
}

func TestIngestHandlerRateLimitsPerUser(t *testing.T) {
	limiter := ratelimit.New(ratelimit.Options{Burst: 1, Every: time.Hour})
	srv := newEventTestServer(t, limiter.MiddlewareBy(RateLimitKey))

	if rec := srv.post(t, hhA, batchJSON(1), userA); rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, body %s", rec.Code, rec.Body.String())
	}
	wantError(t, srv.post(t, hhA, batchJSON(1), userA), 429, "rate_limited")
	// Another member from the same address has their own budget.
	if rec := srv.post(t, hhA, batchJSON(1), userA2); rec.Code != http.StatusOK {
		t.Errorf("other user status = %d, want 200", rec.Code)
	}
	// Unauthenticated requests are rejected before they consume anyone's budget.
	wantError(t, srv.post(t, hhA, batchJSON(1), ""), 401, "unauthenticated")
}
