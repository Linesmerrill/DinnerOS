package shopping

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// Fixtures are made up; nothing here comes from Kroger's catalog.

// fakeKroger is a cart-writing provider in memory: the pure Kroger provider
// plus sign-in, stores, search, and a cart.
type fakeKroger struct {
	*providers.Kroger
	codes     map[string]providers.CustomerToken // code → tokens
	refreshed int
	cart      []providers.CartItem
	cartToken string
	cartErr   error
	refuse    bool // refresh is refused: the member signed out
	now       func() time.Time
}

func newFakeKroger(now func() time.Time) *fakeKroger {
	return &fakeKroger{Kroger: providers.NewKroger("frys"), codes: map[string]providers.CustomerToken{}, now: now}
}

func (k *fakeKroger) AuthorizeURL(state string) string {
	return "https://login.example.test/authorize?state=" + url.QueryEscape(state)
}

func (k *fakeKroger) ExchangeCode(_ context.Context, code string) (providers.CustomerToken, error) {
	t, ok := k.codes[code]
	if !ok {
		return providers.CustomerToken{}, providers.ErrCustomerSignedOut
	}
	return t, nil
}

func (k *fakeKroger) RefreshCustomer(_ context.Context, refresh string) (providers.CustomerToken, error) {
	if k.refuse || refresh != "refresh-1" {
		return providers.CustomerToken{}, providers.ErrCustomerSignedOut
	}
	k.refreshed++
	return providers.CustomerToken{AccessToken: "access-2", RefreshToken: "refresh-1", ExpiresAt: k.now().Add(30 * time.Minute)}, nil
}

func (k *fakeKroger) AddToCart(_ context.Context, token string, items []providers.CartItem) error {
	if k.cartErr != nil {
		return k.cartErr
	}
	k.cartToken = token
	k.cart = append(k.cart, items...)
	return nil
}

func (k *fakeKroger) FindStores(_ context.Context, q providers.StoreQuery) ([]providers.Store, error) {
	return []providers.Store{{StoreID: "66000123", Name: "Fry's Test Store", Address: "1 Test Rd, Phoenix, AZ"}}, nil
}

func (k *fakeKroger) SearchProducts(_ context.Context, q providers.SearchQuery) ([]providers.Product, error) {
	price := int64(399)
	return []providers.Product{{ProductID: "0001111000001", Name: "Test Ground Beef " + q.StoreID, Size: "16 oz", PriceCents: &price, Stock: providers.StockHigh}}, nil
}

type fakeMembers struct{}

func (fakeMembers) GetMembership(_ context.Context, householdID, userID string) (households.Membership, error) {
	if householdID != testHousehold || (userID != testUser && userID != otherMember) {
		return households.Membership{}, errors.New("not a member")
	}
	return memberOf(userID), nil
}

// withKroger rebuilds the fixture's service with the fake Kroger provider.
func (f *fixture) withKroger(t *testing.T) *fakeKroger {
	t.Helper()
	k := newFakeKroger(func() time.Time { return *f.clock })
	svc := NewService(ServiceOptions{
		Store: f.store, Providers: providers.NewRegistry(providers.NewWalmart(providers.WalmartOptions{}), k),
		Grocery: f.plans, Catalog: f.recipes, Pantry: f.pantry, Plans: f.plans, Events: f.events,
		Members: fakeMembers{}, TokenSecret: []byte("a test signing key that is long enough for hmac"),
	})
	svc.now = func() time.Time { return *f.clock }
	f.svc = svc
	return k
}

// connect signs the household in through the provider's page.
func (f *fixture) connect(t *testing.T, k *fakeKroger) {
	t.Helper()
	k.codes["code-1"] = providers.CustomerToken{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: f.clock.Add(30 * time.Minute)}
	authorize, err := f.svc.ConnectURL(f.ctx, f.actor, "kroger")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authorize)
	if _, err := f.svc.CompleteConnection(f.ctx, "kroger", u.Query().Get("state"), "code-1"); err != nil {
		t.Fatal(err)
	}
}

// A member signs in to Fry's once for the household; the tokens are stored
// sealed, refreshed when they expire, and forgotten when Fry's refuses them
// (decision 649).
func TestIntegrationKrogerSignIn(t *testing.T) {
	f := newFixture(t)
	k := f.withKroger(t)
	ctx := f.ctx

	c, err := f.svc.Connection(ctx, testHousehold, "kroger")
	if err != nil || c.Connected {
		t.Fatalf("before = %+v, %v", c, err)
	}
	if _, err := f.svc.Connection(ctx, testHousehold, "walmart"); err == nil {
		t.Error("Walmart has a sign-in")
	}
	if _, err := f.svc.ConnectURL(ctx, viewer(), "kroger"); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer started a sign-in: %v", err)
	}

	authorize, err := f.svc.ConnectURL(ctx, f.actor, "kroger")
	if err != nil {
		t.Fatal(err)
	}
	state := mustQuery(t, authorize, "state")
	// A forged or tampered state is refused.
	for _, bad := range []string{"", "x.y", state[:len(state)-2] + "AA", strings.Replace(state, ".", "x.", 1)} {
		if _, err := f.svc.CompleteConnection(ctx, "kroger", bad, "code-1"); !errors.Is(err, ErrBadConnectState) {
			t.Errorf("state %q: %v, want ErrBadConnectState", bad, err)
		}
	}
	// An expired one too.
	*f.clock = f.clock.Add(connectStateTTL + time.Minute)
	if _, err := f.svc.CompleteConnection(ctx, "kroger", state, "code-1"); !errors.Is(err, ErrBadConnectState) {
		t.Errorf("expired state: %v", err)
	}

	f.connect(t, k)
	c, err = f.svc.Connection(ctx, testHousehold, "kroger")
	if err != nil || !c.Connected || c.ConnectedBy != testUser {
		t.Fatalf("after = %+v, %v", c, err)
	}
	// Stored sealed: neither token appears in the document.
	stored, err := f.store.GetProviderAccount(ctx, testHousehold, providers.KeyKroger)
	if err != nil || strings.Contains(stored.AccessToken, "access-1") || strings.Contains(stored.RefreshToken, "refresh-1") || stored.AccessToken == "" {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	token, err := f.svc.customerToken(ctx, testHousehold, k)
	if err != nil || token != "access-1" || k.refreshed != 0 {
		t.Errorf("fresh token = %q, %v (refreshed %d)", token, err, k.refreshed)
	}
	*f.clock = f.clock.Add(time.Hour)
	if token, err = f.svc.customerToken(ctx, testHousehold, k); err != nil || token != "access-2" || k.refreshed != 1 {
		t.Errorf("expired token = %q, %v (refreshed %d)", token, err, k.refreshed)
	}
	*f.clock = f.clock.Add(time.Hour)
	k.refuse = true
	if _, err := f.svc.customerToken(ctx, testHousehold, k); !errors.Is(err, ErrNotConnected) {
		t.Errorf("refused refresh: %v, want ErrNotConnected", err)
	}
	if c, _ := f.svc.Connection(ctx, testHousehold, "kroger"); c.Connected {
		t.Error("a refused account is still connected")
	}

	k.refuse = false
	f.connect(t, k)
	if err := f.svc.Disconnect(ctx, f.actor, "kroger"); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.svc.Connection(ctx, testHousehold, "kroger"); c.Connected {
		t.Error("still connected after disconnecting")
	}
}

// With Fry's, "send" fills the signed-in cart itself, adds only what's new
// on a later send, and records nothing when Fry's refuses.
func TestIntegrationKrogerFillsTheCart(t *testing.T) {
	f := newFixture(t)
	k := f.withKroger(t)
	ctx := f.ctx
	if _, err := f.svc.UpdateStore(ctx, f.actor, "kroger", "66000123", " Fry's   Test Store "); err != nil {
		t.Fatal(err)
	}
	if s, _ := f.svc.Settings(ctx, testHousehold); s.StoreName != "Fry's Test Store" || s.StoreID != "66000123" {
		t.Errorf("settings = %+v", s)
	}
	for name, upc := range map[string]string{"Ground Beef": "0001111000001", "Kidney Beans": "0001111000005"} {
		if _, _, err := f.svc.PutPreference(ctx, f.actor, "kroger", f.keys[name], PreferenceInput{
			ProductURL: upc, DisplayName: name, PackageSize: map[string]*PackageSize{"Ground Beef": {Quantity: "16", Unit: "oz"}, "Kidney Beans": {Quantity: "1", Unit: "can"}}[name],
			Coverage: providers.CoveragePerAmount,
		}); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}

	// Not signed in: nothing is sent or recorded.
	if _, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "kroger", MatchInput{}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("send before signing in: %v", err)
	}
	if hs, _ := f.svc.ListHandoffs(ctx, testHousehold, HandoffFilter{}); len(hs) != 0 {
		t.Errorf("handoffs = %d", len(hs))
	}

	f.connect(t, k)
	// Fry's fails: nothing is recorded as sent.
	k.cartErr = errors.New("kroger: 503")
	if _, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "kroger", MatchInput{}); !errors.Is(err, ErrCartUnavailable) {
		t.Fatalf("failed cart: %v", err)
	}
	if m, _ := f.svc.Match(ctx, testHousehold, testWeek, "kroger", MatchInput{}); m.Cart != nil {
		t.Errorf("a failed send left cart state: %+v", m.Cart)
	}

	k.cartErr = nil
	h, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "kroger", MatchInput{})
	if err != nil || !isNew || len(h.Links) != 1 || h.Links[0].URL != "https://www.frysfood.com/cart" {
		t.Fatalf("send = %+v, %v, %v", h.Links, isNew, err)
	}
	if k.cartToken != "access-1" || len(k.cart) != 2 || cartQty(k.cart, "0001111000001") != 3 || cartQty(k.cart, "0001111000005") != 2 {
		t.Errorf("cart = %+v", k.cart)
	}

	// Sending again with beef up by one adds only that package.
	names := []string{"Ground Beef", "Kidney Beans"}
	if _, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "kroger", f.allLines(map[string]int{"Ground Beef": 4}, names...)); err != nil {
		t.Fatal(err)
	}
	if len(k.cart) != 3 || k.cart[2].ProductID != "0001111000001" || k.cart[2].Quantity != 1 {
		t.Errorf("second send cart = %+v", k.cart)
	}

	// A failed later send puts the handoff back: the extra package is still
	// to add.
	k.cartErr = errors.New("kroger: 503")
	if _, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "kroger", f.allLines(map[string]int{"Ground Beef": 5}, names...)); !errors.Is(err, ErrCartUnavailable) {
		t.Fatalf("failed second send: %v", err)
	}
	m, err := f.svc.Match(ctx, testHousehold, testWeek, "kroger", f.allLines(map[string]int{"Ground Beef": 5}, names...))
	if err != nil {
		t.Fatal(err)
	}
	if c := cartFor(t, m, "Ground Beef"); c.SentPackages != 4 || c.AddPackages != 1 {
		t.Errorf("after a failed send, beef cart = %+v", c)
	}

	// Fry's refusing the sign-in asks the member to sign in again.
	k.cartErr = providers.ErrCustomerSignedOut
	if _, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "kroger", f.allLines(map[string]int{"Ground Beef": 5}, names...)); !errors.Is(err, ErrNotConnected) {
		t.Errorf("signed out: %v", err)
	}
	if c, _ := f.svc.Connection(ctx, testHousehold, "kroger"); c.Connected {
		t.Error("still connected after Fry's refused the sign-in")
	}
}

func cartQty(cart []providers.CartItem, id string) int {
	n := 0
	for _, it := range cart {
		if it.ProductID == id {
			n += it.Quantity
		}
	}
	return n
}

func mustQuery(t *testing.T, raw, key string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get(key)
}

// The routes: stores, search priced at the household's store, and the
// sign-in callback, which always ends in the app.
func TestIntegrationKrogerRoutes(t *testing.T) {
	f := newFixture(t)
	k := f.withKroger(t)
	ctx := f.ctx
	if _, err := f.svc.UpdateStore(ctx, f.actor, "kroger", "66000123", "Fry's Test Store"); err != nil {
		t.Fatal(err)
	}
	stores, err := f.svc.FindStores(ctx, testHousehold, "kroger", "85001")
	if err != nil || len(stores) != 1 {
		t.Fatalf("stores = %+v, %v", stores, err)
	}
	if _, err := f.svc.FindStores(ctx, testHousehold, "kroger", "8500"); err == nil {
		t.Error("a 4-digit ZIP was accepted")
	}
	results, err := f.svc.SearchProducts(ctx, testHousehold, "kroger", "  ground   beef ")
	if err != nil || len(results) != 1 || results[0].Name != "Test Ground Beef 66000123" || results[0].PackageSize == nil ||
		results[0].PackageSize.Quantity != "16" || results[0].URL != "https://www.frysfood.com/p/item/0001111000001" {
		t.Fatalf("search = %+v, %v", results, err)
	}
	if _, err := f.svc.SearchProducts(ctx, testHousehold, "kroger", "be"); err == nil {
		t.Error("a 2-letter search was accepted")
	}
	if _, err := f.svc.SearchProducts(ctx, testHousehold, "walmart", "beef"); err == nil {
		t.Error("Walmart searched")
	}

	h := NewHandler(HandlerOptions{Service: f.svc, AppURLScheme: "dinneros"})
	r := chi.NewRouter()
	r.Get("/shopping/{provider}/callback", h.connectCallback)
	callback := func(query string) string {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/shopping/kroger/callback?"+query, nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("callback status = %d", rec.Code)
		}
		return rec.Header().Get("Location")
	}
	if got := callback("error=access_denied"); got != "dinneros://shopping/connected?provider=kroger&result=cancelled" {
		t.Errorf("cancelled = %q", got)
	}
	if got := callback("state=forged&code=code-1"); !strings.HasSuffix(got, "result=failed") {
		t.Errorf("forged = %q", got)
	}
	k.codes["code-1"] = providers.CustomerToken{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: f.clock.Add(30 * time.Minute)}
	authorize, _ := f.svc.ConnectURL(ctx, f.actor, "kroger")
	if got := callback("state=" + url.QueryEscape(mustQuery(t, authorize, "state")) + "&code=code-1"); !strings.HasSuffix(got, "result=connected") {
		t.Errorf("connected = %q", got)
	}
	if c, _ := f.svc.Connection(ctx, testHousehold, "kroger"); !c.Connected {
		t.Error("the callback didn't connect")
	}
}
