package shopping

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// The week's cost and the savings history are behind Plus; the shopping
// settings and sending a list are not.
func TestIntegrationCostIsBehindPlus(t *testing.T) {
	f := newFixture(t)
	blocked := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, r, http.StatusPaymentRequired, "plus_required", "This is part of DinnerOS Plus.")
		})
	}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{}, RequirePlus: blocked,
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
		Households: fakeHouseholds{testUser: {testHousehold}},
	}).Mount)
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+testHousehold+"/shopping"+path, nil)
		req.Header.Set("Authorization", "Bearer token-"+testUser)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{"/weeks/2026-W38/cost", "/savings"} {
		if rec := get(path); rec.Code != http.StatusPaymentRequired || !strings.Contains(rec.Body.String(), "plus_required") {
			t.Errorf("%s = %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if rec := get("/settings"); rec.Code != http.StatusOK {
		t.Errorf("settings = %d %s", rec.Code, rec.Body.String())
	}
}
