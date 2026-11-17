package plus

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The development-only route gives the caller's household Plus once; a
// second call changes nothing, and it needs a signed-in member.
func TestDevCompGivesPlus(t *testing.T) {
	comps := &memoryComps{m: map[string]Comp{}}
	s := NewService(newMemoryStore(), true).WithComps(comps)
	r := chi.NewRouter()
	r.Route("/api/v1", NewDevCompHandler(comps, fakeAuthorizer{}, fakeTokens{}, nil).Mount)
	post := func(token, household string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/households/"+household+"/plus/dev-comp", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	if ok, _ := s.HasPlus(t.Context(), hh); ok {
		t.Fatal("has Plus before the comp")
	}
	if rec := post("token-u1", hh); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"granted":true`) {
		t.Fatalf("grant = %d %s", rec.Code, rec.Body)
	}
	if ok, _ := s.HasPlus(t.Context(), hh); !ok {
		t.Error("no Plus after the comp")
	}
	if c := comps.m[hh]; c.Reason != CompReasonQA {
		t.Errorf("reason = %q", c.Reason)
	}
	if rec := post("token-u1", hh); !strings.Contains(rec.Body.String(), `"granted":false`) {
		t.Errorf("second grant = %s", rec.Body)
	}
	if rec := post("", hh); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d", rec.Code)
	}
	if rec := post("token-u2", hh); rec.Code != http.StatusNotFound {
		t.Errorf("not a member = %d", rec.Code)
	}
}
