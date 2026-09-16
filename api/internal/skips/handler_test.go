package skips

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

const (
	hhAda = testHousehold
	hhBob = otherHousehold

	userAda    = "ada"    // may change hhAda's plan, so may skip
	userViewer = "viewer" // may view hhAda but not skip
	userBob    = "bob"    // may change hhBob's plan
	userCat    = "cat"    // no household
)

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer grants permissions per (household, user), mirroring
// households.Service.Authorize.
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

var testAuthorizer = fakeAuthorizer{
	hhAda + "/" + userAda:    {households.PermHouseholdView, households.PermPlanEdit},
	hhAda + "/" + userViewer: {households.PermHouseholdView},
	hhBob + "/" + userBob:    {households.PermHouseholdView, households.PermPlanEdit},
}

func newSkipTestRouter(t *testing.T) *chi.Mux {
	t.Helper()
	svc, _ := newTestService(t, nil)
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: svc, Authorizer: testAuthorizer, Tokens: fakeTokens{},
	}).Mount)
	return r
}

func do(t *testing.T, router http.Handler, method, path, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeSkip(t *testing.T, rec *httptest.ResponseRecorder) SkipResponse {
	t.Helper()
	var resp SkipResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
	return resp
}

func skipsPath(householdID string) string {
	return "/api/v1/households/" + householdID + "/grocery-skips"
}

func TestHandlerSkipsAndResumesAnIngredient(t *testing.T) {
	router := newSkipTestRouter(t)

	// Skip forever.
	rec := do(t, router, http.MethodPost, skipsPath(hhAda),
		`{"ingredientKey":"name:cilantro","name":"Cilantro","scope":"always"}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, want 201: %s", rec.Code, rec.Body)
	}
	created := decodeSkip(t, rec)
	if created.Scope != ScopeAlways || created.Week != nil {
		t.Errorf("created = %+v, want scope always with no week", created)
	}
	if created.Text != "Never buying this" {
		t.Errorf("created text = %q", created.Text)
	}

	// Changing the lifetime replaces it: 200, not a second skip.
	rec = do(t, router, http.MethodPost, skipsPath(hhAda),
		`{"ingredientKey":"name:cilantro","name":"Cilantro","scope":"week","week":"2026-W38"}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST replace = %d, want 200: %s", rec.Code, rec.Body)
	}
	replaced := decodeSkip(t, rec)
	if replaced.ID != created.ID {
		t.Errorf("replace changed the ID: %q → %q", created.ID, replaced.ID)
	}
	if replaced.Week == nil || *replaced.Week != "2026-W38" || replaced.Text != "Skipped this week" {
		t.Errorf("replaced = %+v, want the week skip", replaced)
	}

	rec = do(t, router, http.MethodGet, skipsPath(hhAda), "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body)
	}
	var list SkipListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding the list: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("GET returned %d skips, want 1", len(list.Items))
	}

	// Resume.
	rec = do(t, router, http.MethodDelete, skipsPath(hhAda)+"/"+created.ID, "", userAda)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204: %s", rec.Code, rec.Body)
	}
	rec = do(t, router, http.MethodDelete, skipsPath(hhAda)+"/"+created.ID, "", userAda)
	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE twice = %d, want 404", rec.Code)
	}
}

func TestHandlerRejectsInvalidRequests(t *testing.T) {
	router := newSkipTestRouter(t)
	cases := []struct{ name, body string }{
		{"no key", `{"name":"Cilantro","scope":"always"}`},
		{"unknown scope", `{"ingredientKey":"name:cilantro","name":"Cilantro","scope":"forever"}`},
		{"week scope with no week", `{"ingredientKey":"name:cilantro","name":"Cilantro","scope":"week"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, router, http.MethodPost, skipsPath(hhAda), tc.body, userAda)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST %s = %d, want 400: %s", tc.body, rec.Code, rec.Body)
			}
		})
	}
}

func TestHandlerAuthorizesLikeTheRestOfThePlan(t *testing.T) {
	router := newSkipTestRouter(t)
	const body = `{"ingredientKey":"name:cilantro","name":"Cilantro","scope":"always"}`

	if rec := do(t, router, http.MethodPost, skipsPath(hhAda), body, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST without a token = %d, want 401", rec.Code)
	}
	// A viewer may read the skips but not change them.
	if rec := do(t, router, http.MethodGet, skipsPath(hhAda), "", userViewer); rec.Code != http.StatusOK {
		t.Errorf("GET as a viewer = %d, want 200", rec.Code)
	}
	if rec := do(t, router, http.MethodPost, skipsPath(hhAda), body, userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("POST as a viewer = %d, want 403", rec.Code)
	}
	// A non-member never learns the household exists.
	if rec := do(t, router, http.MethodGet, skipsPath(hhAda), "", userBob); rec.Code != http.StatusNotFound {
		t.Errorf("GET as another household's member = %d, want 404", rec.Code)
	}
	if rec := do(t, router, http.MethodGet, skipsPath(hhAda), "", userCat); rec.Code != http.StatusNotFound {
		t.Errorf("GET with no household = %d, want 404", rec.Code)
	}
}

func TestHandlerKeepsSkipsInTheirHousehold(t *testing.T) {
	router := newSkipTestRouter(t)
	rec := do(t, router, http.MethodPost, skipsPath(hhAda),
		`{"ingredientKey":"name:cilantro","name":"Cilantro","scope":"always"}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	adas := decodeSkip(t, rec)

	// Bob's household sees none of it, and cannot resume Ada's ingredient.
	rec = do(t, router, http.MethodGet, skipsPath(hhBob), "", userBob)
	var list SkipListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("another household sees %d skips, want 0", len(list.Items))
	}
	if rec := do(t, router, http.MethodDelete, skipsPath(hhBob)+"/"+adas.ID, "", userBob); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE across households = %d, want 404", rec.Code)
	}
}
