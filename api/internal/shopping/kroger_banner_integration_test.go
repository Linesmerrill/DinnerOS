package shopping

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// bannerKroger is the fake Kroger with Fry's and Ralphs on: it knows which
// banner each store is, and gives a household's view by banner.
type bannerKroger struct {
	*fakeKroger
	stores map[string]string // store ID → banner
}

func (k *bannerKroger) StoreBanner(_ context.Context, storeID string) (string, error) {
	banner, ok := k.stores[storeID]
	if !ok {
		return "", providers.ErrStoreNotOffered
	}
	return banner, nil
}

func (k *bannerKroger) ForBanner(banner string) providers.GroceryProvider {
	return bannerView{bannerKroger: k, banner: k.Kroger.WithBanner(banner)}
}

// bannerView is the fake as a household at one banner sees it: that banner's
// name and links, the same sign-in and cart.
type bannerView struct {
	*bannerKroger
	banner *providers.Kroger
}

func (v bannerView) Name() string                       { return v.banner.Name() }
func (v bannerView) ProductURL(productID string) string { return v.banner.ProductURL(productID) }
func (v bannerView) CartURL() string                    { return v.banner.CartURL() }
func (v bannerView) BuildCartLinks(items []providers.CartItem, store providers.StoreRef) ([]providers.CartLink, error) {
	return v.banner.BuildCartLinks(items, store)
}

func (f *fixture) withBannerKroger(t *testing.T) *bannerKroger {
	t.Helper()
	base := f.withKroger(t)
	base.Kroger = providers.NewKroger("frys", "ralphs")
	k := &bannerKroger{fakeKroger: base, stores: map[string]string{"66000123": "frys", "70300001": "ralphs"}}
	svc := NewService(ServiceOptions{
		Store: f.store, Providers: providers.NewRegistry(providers.NewWalmart(providers.WalmartOptions{}), k),
		Grocery: f.plans, Catalog: f.recipes, Pantry: f.pantry, Plans: f.plans, Events: f.events,
		Members: fakeMembers{}, TokenSecret: []byte("a test signing key that is long enough for hmac"),
	})
	svc.now = func() time.Time { return *f.clock }
	f.svc = svc
	return k
}

func TestIntegrationAHouseholdShopsAtItsStoresBanner(t *testing.T) {
	f := newFixture(t)
	k := f.withBannerKroger(t)
	ctx := f.ctx

	settings, err := f.svc.UpdateStore(ctx, f.actor, "kroger", "70300001", "Ralphs Test St")
	if err != nil || settings.Banner != "ralphs" {
		t.Fatalf("settings = %+v, %v: the banner follows the store", settings, err)
	}
	p, err := f.svc.ProviderFor(ctx, testHousehold, "kroger")
	if err != nil || p.Name() != "Ralphs" {
		t.Fatalf("provider = %v, %v", p, err)
	}
	results, err := f.svc.SearchProducts(ctx, testHousehold, "kroger", "ground beef")
	if err != nil || len(results) != 1 || results[0].URL != "https://www.ralphs.com/p/item/0001111000001" {
		t.Fatalf("search = %+v, %v: product pages are on the household's banner", results, err)
	}

	// Saved products and the sign-in stay under one provider key, so switching
	// stores between banners keeps them.
	f.connect(t, k.fakeKroger)
	pref, _, err := f.svc.PutPreference(ctx, f.actor, "kroger", f.keys["Ground Beef"], PreferenceInput{
		ProductID: "0001111000001", DisplayName: "Test Ground Beef", PackageSize: &PackageSize{Quantity: "16", Unit: "oz"},
	})
	if err != nil || pref.Provider != providers.KeyKroger {
		t.Fatalf("pref = %+v, %v", pref, err)
	}
	h, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "kroger", MatchInput{Lines: []LineSelection{{IngredientKey: f.keys["Ground Beef"]}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Links) != 1 || !strings.HasPrefix(h.Links[0].URL, "https://www.ralphs.com/") {
		t.Errorf("links = %+v: the cart opens on Ralphs", h.Links)
	}
	if len(k.cart) != 1 || k.cart[0].ProductID != "0001111000001" {
		t.Errorf("cart = %+v", k.cart)
	}

	if settings, err = f.svc.UpdateStore(ctx, f.actor, "kroger", "66000123", "Fry's Test Store"); err != nil || settings.Banner != "frys" {
		t.Fatalf("back to Fry's = %+v, %v", settings, err)
	}
	if p, _ := f.svc.ProviderFor(ctx, testHousehold, "kroger"); p.Name() != "Fry's" {
		t.Errorf("provider = %s", p.Name())
	}
	if prefs, _ := f.svc.ListPreferences(ctx, testHousehold, "kroger"); len(prefs) != 1 {
		t.Errorf("saved products = %+v, want the one saved at Ralphs", prefs)
	}

	var validation *ValidationError
	if _, err := f.svc.UpdateStore(ctx, f.actor, "kroger", "03500001", "Harris Teeter"); !errors.As(err, &validation) {
		t.Errorf("a store of a banner that isn't on = %v", err)
	}
}

func TestIntegrationSettingsAndProvidersNameTheBanner(t *testing.T) {
	f := newFixture(t)
	f.withBannerKroger(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
		Households: fakeHouseholds{testUser: {testHousehold}},
	}).Mount)
	do := func(method, path, body string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+testUser)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %v", method, path, rec.Code, out)
		}
		return out
	}
	items := do(http.MethodGet, "/shopping/providers", "")["items"].([]any)
	kroger := items[1].(map[string]any)
	if kroger["name"] != "Kroger" || len(kroger["banners"].([]any)) != 2 {
		t.Errorf("kroger = %v: several banners are the family", kroger)
	}
	if walmart := items[0].(map[string]any); walmart["banners"] != nil {
		t.Errorf("walmart = %v", walmart)
	}
	settings := do(http.MethodPut, "/households/"+testHousehold+"/shopping/settings", `{"provider":"kroger","storeId":"70300001","storeName":"Ralphs Test St"}`)
	if settings["banner"] != "ralphs" || settings["providerName"] != "Ralphs" {
		t.Errorf("settings = %v", settings)
	}
	if got := do(http.MethodGet, "/households/"+testHousehold+"/shopping/settings", ""); got["providerName"] != "Ralphs" {
		t.Errorf("settings = %v", got)
	}
}

func TestIntegrationProviderForWithoutABannerIsTheDefault(t *testing.T) {
	f := newFixture(t)
	f.withBannerKroger(t)
	if p, err := f.svc.ProviderFor(f.ctx, testHousehold, "kroger"); err != nil || p.Name() != "Fry's" {
		t.Errorf("no settings: %v, %v", p, err)
	}
	if p, err := f.svc.ProviderFor(f.ctx, testHousehold, "walmart"); err != nil || p.Name() != "Walmart" {
		t.Errorf("walmart: %v, %v", p, err)
	}
	if _, err := f.svc.ProviderFor(f.ctx, testHousehold, "target"); err == nil {
		t.Error("an unknown provider resolved")
	}
}
