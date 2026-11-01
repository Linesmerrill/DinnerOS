package shopping

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// A household that shops at Walmart sets up Fry's too: every Walmart saved
// product and cart stays, and each store uses its own store number.
func TestIntegrationASecondStoreKeepsTheFirst(t *testing.T) {
	f := newFixture(t)
	f.withBannerKroger(t)
	ctx := f.ctx

	if _, err := f.svc.UpdateStore(ctx, f.actor, "walmart", "1234", "", true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.PutPreference(ctx, f.actor, "walmart", f.keys["Ground Beef"], PreferenceInput{ProductID: "10450114", DisplayName: "Test Ground Beef"}); err != nil {
		t.Fatal(err)
	}
	walmartCart, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}

	settings, err := f.svc.UpdateStore(ctx, f.actor, "kroger", "70300001", "Ralphs Test St", false)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != providers.KeyWalmart || settings.StoreID != "1234" || len(settings.Stores) != 2 {
		t.Fatalf("settings = %+v: Walmart stays the default, Ralphs is added", settings)
	}
	if ralphs, ok := settings.Store(providers.KeyKroger); !ok || ralphs.Banner != "ralphs" || ralphs.StoreID != "70300001" {
		t.Errorf("ralphs = %+v", ralphs)
	}

	// Each store searches and builds carts at its own store.
	results, err := f.svc.SearchProducts(ctx, testHousehold, "kroger", "ground beef")
	if err != nil || len(results) != 1 || !strings.HasSuffix(results[0].Name, "70300001") || !strings.Contains(results[0].URL, "ralphs.com") {
		t.Errorf("kroger search = %+v, %v", results, err)
	}
	m, err := f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil || m.StoreID != "1234" {
		t.Errorf("walmart match store = %q, %v", m.StoreID, err)
	}

	// Walmart's saved products and cart are untouched.
	if prefs, _ := f.svc.ListPreferences(ctx, testHousehold, "walmart"); len(prefs) != 1 {
		t.Errorf("walmart saved products = %d", len(prefs))
	}
	if h, err := f.svc.GetHandoff(ctx, testHousehold, walmartCart.ID); err != nil || h.Provider != providers.KeyWalmart {
		t.Errorf("walmart cart = %+v, %v", h, err)
	}

	// An older app picking Fry's makes it the default, still keeping Walmart.
	if settings, err = f.svc.UpdateStore(ctx, f.actor, "kroger", "66000123", "Fry's Test Store", true); err != nil {
		t.Fatal(err)
	}
	if settings.Provider != providers.KeyKroger || settings.Banner != "frys" || len(settings.Stores) != 2 {
		t.Errorf("settings = %+v", settings)
	}
	if w, ok := settings.Store(providers.KeyWalmart); !ok || w.StoreID != "1234" {
		t.Errorf("walmart = %+v: its store number stays", w)
	}
	if prefs, _ := f.svc.ListPreferences(ctx, testHousehold, "walmart"); len(prefs) != 1 {
		t.Errorf("walmart saved products after the switch = %d", len(prefs))
	}
}

// A household set up before several stores has its one store as its list.
func TestIntegrationOneStoreSettingsReadAsAList(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.PutSettings(f.ctx, Settings{HouseholdID: testHousehold, Provider: providers.KeyWalmart, UpdatedBy: testUser, UpdatedAt: *f.clock}); err != nil {
		t.Fatal(err)
	}
	s, err := f.svc.Settings(f.ctx, testHousehold)
	if err != nil || len(s.Stores) != 1 || s.Stores[0].Provider != providers.KeyWalmart || s.TripProvider(testWeek) != providers.KeyWalmart {
		t.Fatalf("settings = %+v, %v", s, err)
	}
	f.withBannerKroger(t)
	if s, err = f.svc.UpdateStore(f.ctx, f.actor, "kroger", "66000123", "Fry's Test Store", false); err != nil || len(s.Stores) != 2 || s.Provider != providers.KeyWalmart {
		t.Errorf("adding Fry's = %+v, %v", s, err)
	}
}

func TestIntegrationDefaultTripAndRemovingStores(t *testing.T) {
	f := newFixture(t)
	k := f.withBannerKroger(t)
	ctx := f.ctx
	var validation *ValidationError

	if _, err := f.svc.SetDefaultStore(ctx, f.actor, "kroger"); !errors.As(err, &validation) {
		t.Errorf("default before set up = %v", err)
	}
	if _, err := f.svc.UpdateStore(ctx, f.actor, "walmart", "", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetTripStore(ctx, f.actor, testWeek, "kroger"); !errors.As(err, &validation) {
		t.Errorf("trip to a store not set up = %v", err)
	}
	if _, err := f.svc.UpdateStore(ctx, f.actor, "kroger", "66000123", "Fry's Test Store", false); err != nil {
		t.Fatal(err)
	}

	s, err := f.svc.SetTripStore(ctx, f.actor, testWeek, "kroger")
	if err != nil || s.TripProvider(testWeek) != providers.KeyKroger || s.TripProvider("2026-W40") != providers.KeyWalmart {
		t.Fatalf("trip = %+v, %v", s, err)
	}
	if _, err := f.svc.SetTripStore(ctx, f.actor, "not-a-week", "kroger"); !errors.As(err, &validation) {
		t.Errorf("bad week = %v", err)
	}
	// Back to the default forgets the week's choice.
	if s, err = f.svc.SetTripStore(ctx, f.actor, testWeek, "walmart"); err != nil || len(s.Trips) != 0 {
		t.Errorf("trips = %+v, %v", s.Trips, err)
	}
	if s, err = f.svc.SetTripStore(ctx, f.actor, testWeek, ""); err != nil || len(s.Trips) != 0 {
		t.Errorf("clear = %+v, %v", s.Trips, err)
	}

	// Only the newest weeks' choices are kept.
	for i := range MaxTrips + 3 {
		week := "2026-W" + twoDigits(10+i)
		if _, err := f.svc.SetTripStore(ctx, f.actor, week, "kroger"); err != nil {
			t.Fatal(err)
		}
	}
	s, _ = f.svc.Settings(ctx, testHousehold)
	if len(s.Trips) != MaxTrips || s.Trips[0].Week != "2026-W13" {
		t.Errorf("trips = %+v", s.Trips)
	}

	if s, err = f.svc.SetDefaultStore(ctx, f.actor, "kroger"); err != nil || s.Provider != providers.KeyKroger || s.StoreID != "66000123" || s.Banner != "frys" {
		t.Errorf("default = %+v, %v", s, err)
	}

	// Removing the default makes the other store the default and drops its
	// trips; the removed store's saved products stay.
	f.connect(t, k.fakeKroger)
	if _, _, err := f.svc.PutPreference(ctx, f.actor, "kroger", f.keys["Ground Beef"], PreferenceInput{ProductID: "0001111000001", DisplayName: "Test Ground Beef"}); err != nil {
		t.Fatal(err)
	}
	if s, err = f.svc.RemoveStore(ctx, f.actor, "kroger"); err != nil || s.Provider != providers.KeyWalmart || len(s.Stores) != 1 || len(s.Trips) != 0 {
		t.Errorf("after removing = %+v, %v", s, err)
	}
	if prefs, _ := f.svc.ListPreferences(ctx, testHousehold, "kroger"); len(prefs) != 1 {
		t.Errorf("kroger saved products = %d, want kept", len(prefs))
	}
	if _, err := f.svc.RemoveStore(ctx, f.actor, "kroger"); !errors.As(err, &validation) {
		t.Errorf("removing again = %v", err)
	}
	if s, err = f.svc.RemoveStore(ctx, f.actor, "walmart"); err != nil || s.Provider != "" || len(s.Stores) != 0 {
		t.Errorf("no stores = %+v, %v", s, err)
	}

	if _, err := f.svc.SetTripStore(ctx, viewer(), testWeek, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer = %v", err)
	}
}

func twoDigits(n int) string {
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func TestIntegrationStoreRoutes(t *testing.T) {
	f := newFixture(t)
	f.withBannerKroger(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
		Households: fakeHouseholds{testUser: {testHousehold}},
	}).Mount)
	do := func(method, path, body string, want int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+"/shopping"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+testUser)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != want {
			t.Fatalf("%s %s = %d %v", method, path, rec.Code, out)
		}
		return out
	}
	do(http.MethodPut, "/settings", `{"provider":"walmart","storeId":null}`, http.StatusOK)
	got := do(http.MethodPut, "/settings", `{"provider":"kroger","storeId":"70300001","storeName":"Ralphs Test St","makeDefault":false}`, http.StatusOK)
	if got["provider"] != "walmart" {
		t.Errorf("default = %v", got["provider"])
	}
	stores := got["stores"].([]any)
	if len(stores) != 2 {
		t.Fatalf("stores = %v", stores)
	}
	ralphs := stores[1].(map[string]any)
	if ralphs["provider"] != "kroger" || ralphs["providerName"] != "Ralphs" || ralphs["isDefault"] != false || ralphs["storeId"] != "70300001" {
		t.Errorf("ralphs = %v", ralphs)
	}
	if w := stores[0].(map[string]any); w["isDefault"] != true || w["storeId"] != nil || w["providerName"] != "Walmart" {
		t.Errorf("walmart = %v", w)
	}

	got = do(http.MethodPut, "/weeks/"+testWeek+"/store", `{"provider":"kroger"}`, http.StatusOK)
	if trips := got["trips"].([]any); len(trips) != 1 || trips[0].(map[string]any)["provider"] != "kroger" || trips[0].(map[string]any)["week"] != testWeek {
		t.Errorf("trips = %v", trips)
	}
	got = do(http.MethodPut, "/weeks/"+testWeek+"/store", `{"provider":null}`, http.StatusOK)
	if trips := got["trips"].([]any); len(trips) != 0 {
		t.Errorf("trips after clearing = %v", trips)
	}
	do(http.MethodPut, "/weeks/"+testWeek+"/store", `{"provider":"target"}`, http.StatusBadRequest)

	got = do(http.MethodPut, "/settings/default", `{"provider":"kroger"}`, http.StatusOK)
	if got["provider"] != "kroger" || got["providerName"] != "Ralphs" {
		t.Errorf("default = %v", got)
	}
	do(http.MethodPut, "/settings/default", `{}`, http.StatusBadRequest)
	got = do(http.MethodDelete, "/settings/stores/kroger", "", http.StatusOK)
	if got["provider"] != "walmart" || len(got["stores"].([]any)) != 1 {
		t.Errorf("after delete = %v", got)
	}
	if got = do(http.MethodGet, "/settings", "", http.StatusOK); len(got["stores"].([]any)) != 1 || got["trips"] == nil {
		t.Errorf("get = %v", got)
	}
}
