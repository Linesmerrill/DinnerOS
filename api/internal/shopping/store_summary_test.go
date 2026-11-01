package shopping

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

func TestIntegrationStoreRequestSummary(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	for _, req := range []struct {
		household, user string
		in              StoreRequestInput
	}{
		{testHousehold, testUser, StoreRequestInput{Name: "Sprouts Farmers Market #412", Source: SourceNearby, Place: &StorePlace{Name: "Sprouts #412", Address: "1 Test St"}}},
		{secondHousehold, otherMember, StoreRequestInput{Key: "sprouts", Note: "closest to us"}},
		{testHousehold, testUser, StoreRequestInput{Name: "corner market #3"}},
	} {
		if _, _, err := f.svc.RequestStore(ctx, memberOfHousehold(req.household, req.user), req.in); err != nil {
			t.Fatal(err)
		}
	}
	tallies, err := f.svc.StoreRequestSummary(ctx)
	if err != nil || len(tallies) != 2 {
		t.Fatalf("tallies = %+v, %v", tallies, err)
	}
	sprouts, corner := tallies[0], tallies[1]
	if sprouts.Key != "sprouts" || sprouts.Households != 2 || sprouts.Custom || sprouts.Status() != StatusUnsupported {
		t.Errorf("sprouts = %+v", sprouts)
	}
	if sprouts.Sources[SourceNearby] != 1 || sprouts.Sources[SourceCatalog] != 1 || len(sprouts.Places) != 1 || len(sprouts.Notes) != 1 {
		t.Errorf("sprouts detail = %+v", sprouts)
	}
	if corner.Key != "corner-market" || !corner.Custom || corner.Households != 1 || corner.Sources[SourceTyped] != 1 {
		t.Errorf("corner = %+v", corner)
	}
}

func TestIntegrationStoreRequestSummaryIsForOperators(t *testing.T) {
	f := newFixture(t)
	if _, _, err := f.svc.RequestStore(f.ctx, f.actor, StoreRequestInput{Name: "corner market"}); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
		Households: fakeHouseholds{testUser: {testHousehold}},
		Operators:  []string{otherMember},
	}).Mount)
	get := func(user string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/shopping/requests/summary", nil)
		req.Header.Set("Authorization", "Bearer token-"+user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, _ := get(testUser); code != http.StatusForbidden {
		t.Errorf("a member = %d, want 403", code)
	}
	code, body := get(otherMember)
	if code != http.StatusOK || body["households"] != float64(1) || body["custom"] != float64(1) {
		t.Fatalf("operator = %d %v", code, body)
	}
	item := body["items"].([]any)[0].(map[string]any)
	if item["key"] != "corner-market" || item["inCatalog"] != false || item["custom"] != true || item["notes"] == nil || item["places"] == nil {
		t.Errorf("item = %v", item)
	}
}
