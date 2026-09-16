package shopping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

const (
	secondHousehold = "66e5a1f2c3b4a5d6e7f80a02"
	cappedHousehold = "66e5a1f2c3b4a5d6e7f80a03"
)

func memberOfHousehold(householdID, userID string) households.Membership {
	return households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
}

// fakeHouseholds maps a user to the households they belong to.
type fakeHouseholds map[string][]string

func (f fakeHouseholds) ListForUser(_ context.Context, userID string) ([]households.UserHousehold, error) {
	out := []households.UserHousehold{}
	for _, id := range f[userID] {
		out = append(out, households.UserHousehold{Membership: memberOfHousehold(id, userID)})
	}
	return out, nil
}

func catalogItem(t *testing.T, items []CatalogItem, key string) CatalogItem {
	t.Helper()
	for _, i := range items {
		if i.Key == key {
			return i
		}
	}
	t.Fatalf("no catalog item %q", key)
	return CatalogItem{}
}

func TestIntegrationStoreRequests(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	// Nothing has been asked for yet.
	items, err := f.svc.StoreCatalog(ctx, testHousehold, "")
	if err != nil || len(items) != len(CatalogEntries()) {
		t.Fatalf("StoreCatalogEntries() = %d items, %v", len(items), err)
	}
	for _, i := range items {
		if i.Requests != 0 || i.RequestedByHousehold {
			t.Fatalf("%s starts with demand: %+v", i.Key, i)
		}
	}

	// A catalog key, with a note that gets trimmed.
	kroger, created, err := f.svc.RequestStore(ctx, f.actor, StoreRequestInput{Key: "kroger", Note: "  closest to us  "})
	if err != nil || !created || kroger.Key != "kroger" || kroger.Name != "Kroger" || kroger.Note != "closest to us" ||
		kroger.Status() != StatusResearched || kroger.RequestedBy != testUser || !kroger.RequestedAt.Equal(testNow) {
		t.Fatalf("RequestStore(kroger) = %+v, %v, %v", kroger, created, err)
	}

	// Asking again updates the note and keeps the first asker and time.
	f.advance(time.Hour)
	again, created, err := f.svc.RequestStore(ctx, memberOf(otherMember), StoreRequestInput{Key: "kroger", Note: "delivery too"})
	if err != nil || created || again.ID != kroger.ID || again.Note != "delivery too" ||
		again.RequestedBy != testUser || !again.RequestedAt.Equal(testNow) {
		t.Errorf("second request = %+v, %v, %v", again, created, err)
	}

	// Free text matching an alias records the catalog entry, not a new one.
	f.advance(time.Hour)
	frys, created, err := f.svc.RequestStore(ctx, f.actor, StoreRequestInput{Name: " FRY'S "})
	if err != nil || !created || frys.Key != "frys" || frys.Name != "Fry's" || frys.Status() != StatusResearched {
		t.Errorf("RequestStore(frys) = %+v, %v, %v", frys, created, err)
	}

	// Free text matching nothing is normalized and title cased.
	f.advance(time.Hour)
	local, created, err := f.svc.RequestStore(ctx, f.actor, StoreRequestInput{Name: "some   local  market"})
	if err != nil || !created || local.Key != "some-local-market" || local.Name != "Some Local Market" ||
		local.Status() != StatusUnsupported {
		t.Errorf("RequestStore(free text) = %+v, %v, %v", local, created, err)
	}

	// Newest first.
	list, err := f.svc.ListStoreRequests(ctx, testHousehold)
	if err != nil || len(list) != 3 || list[0].Key != "some-local-market" || list[1].Key != "frys" || list[2].Key != "kroger" {
		t.Fatalf("ListStoreRequests() = %+v, %v", list, err)
	}

	// A second household's request adds to the global count only.
	other := memberOfHousehold(secondHousehold, testUser)
	if _, _, err := f.svc.RequestStore(ctx, other, StoreRequestInput{Key: "kroger"}); err != nil {
		t.Fatal(err)
	}
	items, err = f.svc.StoreCatalog(ctx, testHousehold, "")
	if err != nil {
		t.Fatal(err)
	}
	if k := catalogItem(t, items, "kroger"); k.Requests != 2 || !k.RequestedByHousehold {
		t.Errorf("kroger for the first household = %+v", k)
	}
	items, err = f.svc.StoreCatalog(ctx, secondHousehold, "")
	if err != nil {
		t.Fatal(err)
	}
	if k := catalogItem(t, items, "kroger"); k.Requests != 2 || !k.RequestedByHousehold {
		t.Errorf("kroger for the second household = %+v", k)
	}
	if fr := catalogItem(t, items, "frys"); fr.Requests != 1 || fr.RequestedByHousehold {
		t.Errorf("frys for the second household = %+v", fr)
	}
	// A store no one asked for stays at zero, and a free-text request never
	// joins the catalog.
	if p := catalogItem(t, items, "publix"); p.Requests != 0 || p.RequestedByHousehold {
		t.Errorf("publix = %+v", p)
	}
	if _, ok := CatalogEntryByKey("some-local-market"); ok {
		t.Error("free text must not enter the catalog")
	}

	// Search goes through the same counts.
	items, err = f.svc.StoreCatalog(ctx, testHousehold, "krog")
	if err != nil || len(items) != 1 || items[0].Key != "kroger" || items[0].Requests != 2 {
		t.Errorf("StoreCatalog(q=krog) = %+v, %v", items, err)
	}

	// Only a new store is a demand signal; updating a note records nothing.
	var requested []events.Event
	for _, e := range f.events.list {
		if e.Type == events.TypeShoppingStoreRequested {
			requested = append(requested, e)
		}
	}
	if len(requested) != 4 {
		t.Fatalf("recorded %d store_requested events, want 4", len(requested))
	}
	if p, ok := requested[0].Payload.(events.ShoppingStoreRequested); !ok || p.Key != "kroger" || !p.Catalog ||
		requested[0].HouseholdID != testHousehold || requested[0].UserID != testUser {
		t.Errorf("catalog event = %+v", requested[0])
	}
	if p, ok := requested[2].Payload.(events.ShoppingStoreRequested); !ok || p.Key != "some-local-market" || p.Catalog {
		t.Errorf("free-text event = %+v", requested[2])
	}

	for _, tc := range []struct {
		in   StoreRequestInput
		want string
	}{
		{StoreRequestInput{}, "send key"},
		{StoreRequestInput{Key: "kroger", Name: "Kroger"}, "not both"},
		{StoreRequestInput{Key: "no-such-store"}, "catalog"},
		{StoreRequestInput{Name: strings.Repeat("a", MaxStoreNameLength+1)}, "name must be at most"},
		{StoreRequestInput{Name: "Kroger", Note: strings.Repeat("n", MaxStoreRequestNote+1)}, "note must be at most"},
		{StoreRequestInput{Name: "!!!"}, "letter or digit"},
	} {
		_, _, err := f.svc.RequestStore(ctx, f.actor, tc.in)
		wantValidation(t, err, tc.want)
	}
	if _, _, err := f.svc.RequestStore(ctx, viewer(), StoreRequestInput{Key: "aldi"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer error = %v", err)
	}

	// The cap counts stores, not requests.
	capped := memberOfHousehold(cappedHousehold, testUser)
	for i := range MaxStoreRequestsPerHousehold {
		if _, _, err := f.svc.RequestStore(ctx, capped, StoreRequestInput{Name: fmt.Sprintf("Store %d", i)}); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if _, _, err := f.svc.RequestStore(ctx, capped, StoreRequestInput{Name: "One More"}); !errors.Is(err, ErrTooManyRequests) {
		t.Errorf("past the cap = %v, want ErrTooManyRequests", err)
	}
	if _, created, err := f.svc.RequestStore(ctx, capped, StoreRequestInput{Name: "Store 0", Note: "still want it"}); err != nil || created {
		t.Errorf("updating a note at the cap = %v, %v", created, err)
	}

	// Withdrawing.
	if err := f.svc.DeleteStoreRequest(ctx, other, local.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household's delete = %v", err)
	}
	if err := f.svc.DeleteStoreRequest(ctx, f.actor, local.ID); err != nil {
		t.Error(err)
	}
	if err := f.svc.DeleteStoreRequest(ctx, f.actor, local.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
	if err := f.svc.DeleteStoreRequest(ctx, f.actor, "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id = %v", err)
	}
	if list, _ := f.svc.ListStoreRequests(ctx, testHousehold); len(list) != 2 {
		t.Errorf("after withdrawing = %+v", list)
	}
}

func TestIntegrationStoreRequestsHTTP(t *testing.T) {
	f := newFixture(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
		Households: fakeHouseholds{testUser: {testHousehold}},
	}).Mount)
	do := func(method, path, body, userID string) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}
	hh := "/households/" + testHousehold

	// 1. The catalog is signed in and not household scoped.
	rec, body := do(http.MethodGet, "/shopping/catalog", "", testUser)
	items, _ := body["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != len(CatalogEntries()) {
		t.Fatalf("catalog = %d %v", rec.Code, body)
	}
	first := items[0].(map[string]any)
	if first["key"] != "walmart" || first["status"] != "available" || first["kind"] != "grocer" ||
		first["requests"] != 0.0 || first["requestedByHousehold"] != false || first["note"] == nil {
		t.Errorf("first catalog item = %v", first)
	}
	// A user in no household still gets the catalog.
	if rec, body = do(http.MethodGet, "/shopping/catalog", "", outsider); rec.Code != 200 || len(body["items"].([]any)) == 0 {
		t.Errorf("catalog for a user in no household = %d %v", rec.Code, body)
	}
	// Search by alias; an unsupported store has a null note.
	rec, body = do(http.MethodGet, "/shopping/catalog?q=quality+food", "", testUser)
	items, _ = body["items"].([]any)
	if rec.Code != 200 || len(items) != 1 || items[0].(map[string]any)["key"] != "qfc" {
		t.Fatalf("catalog search = %d %v", rec.Code, body)
	}
	rec, body = do(http.MethodGet, "/shopping/catalog?q=publix", "", testUser)
	items, _ = body["items"].([]any)
	if publix := items[0].(map[string]any); rec.Code != 200 || publix["note"] != nil || publix["status"] != "unsupported" {
		t.Errorf("publix = %v", publix)
	}

	// 2. Create, then update the note.
	rec, body = do(http.MethodPost, hh+"/shopping/requests", `{"key":"kroger","note":"closest to us"}`, testUser)
	request, _ := body["request"].(map[string]any)
	if rec.Code != http.StatusCreated || request["key"] != "kroger" || request["name"] != "Kroger" ||
		request["status"] != "researched" || request["note"] != "closest to us" || request["requestedBy"] != testUser {
		t.Fatalf("create request = %d %v", rec.Code, body)
	}
	id, _ := request["id"].(string)
	rec, body = do(http.MethodPost, hh+"/shopping/requests", `{"key":"kroger","note":"and delivery"}`, testUser)
	if rec.Code != http.StatusOK || body["request"].(map[string]any)["note"] != "and delivery" ||
		body["request"].(map[string]any)["id"] != id {
		t.Errorf("update request = %d %v", rec.Code, body)
	}
	// Free text matching an alias.
	rec, body = do(http.MethodPost, hh+"/shopping/requests", `{"name":"frys"}`, testUser)
	if rec.Code != http.StatusCreated || body["request"].(map[string]any)["key"] != "frys" ||
		body["request"].(map[string]any)["note"] != nil {
		t.Errorf("free-text request = %d %v", rec.Code, body)
	}
	// The catalog now shows this household's demand.
	rec, body = do(http.MethodGet, "/shopping/catalog?q=kroger", "", testUser)
	kroger := body["items"].([]any)[0].(map[string]any)
	if rec.Code != 200 || kroger["requests"] != 1.0 || kroger["requestedByHousehold"] != true {
		t.Errorf("kroger after requesting = %v", kroger)
	}

	// 3. List, newest first.
	rec, body = do(http.MethodGet, hh+"/shopping/requests", "", testUser)
	items, _ = body["items"].([]any)
	if rec.Code != 200 || len(items) != 2 || items[0].(map[string]any)["key"] != "frys" {
		t.Errorf("list requests = %d %v", rec.Code, body)
	}

	// 4. Withdraw.
	if rec, _ = do(http.MethodDelete, hh+"/shopping/requests/"+id, "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d", rec.Code)
	}
	if rec, _ = do(http.MethodDelete, hh+"/shopping/requests/"+id, "", testUser); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d", rec.Code)
	}

	for _, tc := range []struct {
		method, path, body, user string
		status                   int
		code                     string
	}{
		{http.MethodPost, "/shopping/requests", `{}`, testUser, 400, "validation_failed"},
		{http.MethodPost, "/shopping/requests", `{"key":"kroger","name":"Kroger"}`, testUser, 400, "validation_failed"},
		{http.MethodPost, "/shopping/requests", `{"key":"no-such-store"}`, testUser, 400, "validation_failed"},
		{http.MethodPost, "/shopping/requests", `{"name":"Kroger","unknown":1}`, testUser, 400, "invalid_request"},
		{http.MethodGet, "/shopping/requests", "", outsider, 404, "not_found"},
		{http.MethodPost, "/shopping/requests", `{"key":"aldi"}`, outsider, 404, "not_found"},
		{http.MethodDelete, "/shopping/requests/not-an-id", "", testUser, 404, "not_found"},
	} {
		rec, body := do(tc.method, hh+tc.path, tc.body, tc.user)
		errBody, _ := body["error"].(map[string]any)
		if rec.Code != tc.status || errBody["code"] != tc.code {
			t.Errorf("%s %s as %s = %d %v, want %d %s", tc.method, tc.path, tc.user, rec.Code, body, tc.status, tc.code)
		}
	}
}
