package liveactivity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newTestRouter(store Store) chi.Router {
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Store: store, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount)
	return r
}

func do(r chi.Router, method, path, body, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

const activityPath = "/api/v1/households/" + hhAda + "/meal-kit/hellofresh/imports/" + jobAda + "/live-activity"

func TestRegisteringAnActivityTokenStoresItOnTheJob(t *testing.T) {
	store := newMemoryStore()
	r := newTestRouter(store)
	rec := do(r, http.MethodPut, activityPath, `{"token":"`+token1+`","environment":"production"}`, userAda)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), token1) {
		t.Error("the token was echoed back")
	}
	reg, err := store.Get(context.Background(), jobAda)
	if err != nil || reg.Token != token1 || reg.Environment != "production" {
		t.Fatalf("stored = %+v, %v", reg, err)
	}

	// A rotated token replaces the old one.
	if rec := do(r, http.MethodPut, activityPath, `{"token":"`+token2+`","environment":"production"}`, userAda); rec.Code != http.StatusNoContent {
		t.Fatalf("rotate = %d", rec.Code)
	}
	if reg, _ := store.Get(context.Background(), jobAda); reg.Token != token2 {
		t.Errorf("after rotation token = %q", reg.Token)
	}

	if rec := do(r, http.MethodDelete, activityPath, "", userAda); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	if _, err := store.Get(context.Background(), jobAda); err != ErrNotFound {
		t.Errorf("token kept after DELETE: %v", err)
	}
}

func TestActivityTokenRoutesRefuseBadInputAndOtherHouseholds(t *testing.T) {
	r := newTestRouter(newMemoryStore())
	good := `{"token":"` + token1 + `","environment":"sandbox"}`
	cases := []struct {
		name, method, path, body, user string
		want                           int
	}{
		{"unauthenticated", http.MethodPut, activityPath, good, "", http.StatusUnauthorized},
		{"no import permission", http.MethodPut, "/api/v1/households/" + hhBob + "/meal-kit/hellofresh/imports/" + jobAda + "/live-activity", good, userBob, http.StatusForbidden},
		{"not a member", http.MethodPut, activityPath, good, userBob, http.StatusNotFound},
		{"unknown service", http.MethodPut, "/api/v1/households/" + hhAda + "/meal-kit/other/imports/" + jobAda + "/live-activity", good, userAda, http.StatusNotFound},
		{"not hex", http.MethodPut, activityPath, `{"token":"NOT-A-TOKEN","environment":"sandbox"}`, userAda, http.StatusBadRequest},
		{"bad environment", http.MethodPut, activityPath, `{"token":"` + token1 + `","environment":"staging"}`, userAda, http.StatusBadRequest},
		{"unknown field", http.MethodPut, activityPath, `{"token":"` + token1 + `","environment":"sandbox","email":"x"}`, userAda, http.StatusBadRequest},
		{"finished or unknown job", http.MethodPut, "/api/v1/households/" + hhAda + "/meal-kit/hellofresh/imports/66e5a1f2c3b4a5d6e7f80bff/live-activity", good, userAda, http.StatusNotFound},
	}
	for _, tc := range cases {
		if rec := do(r, tc.method, tc.path, tc.body, tc.user); rec.Code != tc.want {
			t.Errorf("%s: %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body)
		}
	}
}
