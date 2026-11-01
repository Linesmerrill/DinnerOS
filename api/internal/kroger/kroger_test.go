package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// Fixtures are made up; nothing here comes from Kroger's catalog.

type fakeKroger struct {
	t         *testing.T
	appTokens atomic.Int32
	cart      []cartItem
	cartToken string
	status    int
}

func (f *fakeKroger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	switch r.URL.Path {
	case "/v1/connect/oauth2/token":
		id, secret, ok := r.BasicAuth()
		if !ok || id != "client-1" || secret != "secret-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "client_credentials":
			f.appTokens.Add(1)
			if r.Form.Get("scope") != "product.compact" {
				f.t.Errorf("app scope = %q", r.Form.Get("scope"))
			}
			_, _ = io.WriteString(w, `{"access_token":"app-token","expires_in":1800}`)
		case "authorization_code":
			if r.Form.Get("code") != "code-1" || r.Form.Get("redirect_uri") != "https://api.example.test/cb" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"member-access","refresh_token":"member-refresh","expires_in":1800}`)
		case "refresh_token":
			if r.Form.Get("refresh_token") != "member-refresh" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"member-access-2","expires_in":1800}`)
		}
	case "/v1/locations/70300001":
		_, _ = io.WriteString(w, `{"data":{"locationId":"70300001","chain":"RALPHS","name":"Ralphs - Test St"}}`)
	case "/v1/locations/03500001":
		_, _ = io.WriteString(w, `{"data":{"locationId":"03500001","chain":"HART","name":"Harris Teeter Fuel Center"}}`)
	case "/v1/locations":
		// Every enabled banner at once: no chain filter, and stores of other
		// chains come back too.
		if r.URL.Query().Get("filter.chain") == "" && r.URL.Query().Get("filter.zipCode.near") == "90001" {
			_, _ = io.WriteString(w, `{"data":[
				{"locationId":"70300001","chain":"RALPHS","name":"Ralphs - Test St","address":{"addressLine1":"2 Test Ave","city":"Los Angeles","state":"CA"}},
				{"locationId":"70500001","chain":"FOOD4LESS","name":"Food 4 Less - Test Blvd","address":{"addressLine1":"3 Test Blvd","city":"Los Angeles","state":"CA"}},
				{"locationId":"66000999","chain":"FRYS","name":"Fry's Food And Drug - Yuma","address":{"addressLine1":"4 Test Rd","city":"Yuma","state":"AZ"}}]}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer app-token" || r.URL.Query().Get("filter.chain") != "FRYS" || r.URL.Query().Get("filter.zipCode.near") != "85001" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"locationId":"66000123","name":"Fry's Test Store","address":{"addressLine1":"1 Test Rd","city":"Phoenix","state":"AZ","zipCode":"85001"}}]}`)
	case "/v1/products":
		q := r.URL.Query()
		// Like Kroger, a term with words no product has matches nothing.
		if term := q.Get("filter.term"); term != "pineapple" && strings.HasSuffix(term, " pineapple") {
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer app-token" || q.Get("filter.term") != "pineapple" || q.Get("filter.locationId") != "66000123" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"data":[
			{"productId":"0001111041700","upc":"0001111041700","brand":"Test Farms","description":"Test Pineapple Chunks",
			 "images":[{"perspective":"back","sizes":[{"size":"medium","url":"https://img.example.test/back.jpg"}]},
			           {"perspective":"front","featured":true,"sizes":[{"size":"small","url":"https://img.example.test/s.jpg"},{"size":"medium","url":"https://img.example.test/m.jpg"}]}],
			 "items":[{"size":"20 oz","price":{"regular":3.49,"promo":2.99},"inventory":{"stockLevel":"LOW"}}]},
			{"productId":"0001111041701","description":"Test Pineapple Juice","items":[{"size":"46 fl oz","inventory":{"stockLevel":"TEMPORARILY_OUT_OF_STOCK"}}]}
		]}`)
	case "/v1/cart/add":
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		f.cartToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if f.cartToken != "member-access" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body cartRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.cart = append(f.cart, body.Items...)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTest(t *testing.T) (*Provider, *fakeKroger) {
	t.Helper()
	fake := &fakeKroger{t: t}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return New(Options{ClientID: "client-1", ClientSecret: "secret-1", RedirectURL: "https://api.example.test/cb", APIBase: srv.URL, Banner: "frys"}), fake
}

func TestSignIn(t *testing.T) {
	p, _ := newTest(t)
	ctx := context.Background()
	u, err := url.Parse(p.AuthorizeURL("state-1"))
	if err != nil || u.Path != "/v1/connect/oauth2/authorize" {
		t.Fatalf("authorize URL = %v, %v", u, err)
	}
	q := u.Query()
	if q.Get("scope") != "cart.basic:write" || q.Get("state") != "state-1" || q.Get("client_id") != "client-1" ||
		q.Get("redirect_uri") != "https://api.example.test/cb" || q.Get("response_type") != "code" || strings.Contains(u.String(), "secret-1") {
		t.Errorf("authorize query = %v", q)
	}
	token, err := p.ExchangeCode(ctx, "code-1")
	if err != nil || token.AccessToken != "member-access" || token.RefreshToken != "member-refresh" || time.Until(token.ExpiresAt) < 29*time.Minute {
		t.Fatalf("ExchangeCode = %+v, %v", token, err)
	}
	if _, err := p.ExchangeCode(ctx, "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("a refused code: %v, want ErrUnauthorized", err)
	}
	// A refresh that doesn't rotate the refresh token keeps the old one.
	token, err = p.RefreshCustomer(ctx, "member-refresh")
	if err != nil || token.AccessToken != "member-access-2" || token.RefreshToken != "member-refresh" {
		t.Errorf("RefreshCustomer = %+v, %v", token, err)
	}
	if _, err := p.RefreshCustomer(ctx, "revoked"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("a revoked refresh token: %v, want ErrUnauthorized", err)
	}
}

func TestStoresAndProducts(t *testing.T) {
	p, fake := newTest(t)
	ctx := context.Background()
	stores, err := p.FindStores(ctx, providers.StoreQuery{ZIPCode: "85001"})
	if err != nil || len(stores) != 1 || stores[0].StoreID != "66000123" || stores[0].Address != "1 Test Rd, Phoenix, AZ" {
		t.Fatalf("FindStores = %+v, %v", stores, err)
	}
	products, err := p.SearchProducts(ctx, providers.SearchQuery{Term: "pineapple", StoreID: "66000123"})
	if err != nil || len(products) != 2 {
		t.Fatalf("SearchProducts = %+v, %v", products, err)
	}
	chunks := products[0]
	if chunks.ProductID != "0001111041700" || chunks.Name != "Test Pineapple Chunks" || chunks.Brand != "Test Farms" || chunks.Size != "20 oz" ||
		chunks.PriceCents == nil || *chunks.PriceCents != 299 || chunks.Stock != providers.StockLow || chunks.ImageURL != "https://img.example.test/m.jpg" {
		t.Errorf("chunks = %+v", chunks)
	}
	if products[1].Stock != providers.StockOut || products[1].PriceCents != nil {
		t.Errorf("juice = %+v", products[1])
	}
	// The application token is reused.
	if n := fake.appTokens.Load(); n != 1 {
		t.Errorf("app tokens fetched = %d, want 1", n)
	}
}

func TestAddToCart(t *testing.T) {
	p, fake := newTest(t)
	ctx := context.Background()
	items := []providers.CartItem{{ProductID: "0001111041700", Quantity: 2}, {ProductID: "0001111060933", Quantity: 1}}
	if err := p.AddToCart(ctx, "member-access", items); err != nil {
		t.Fatal(err)
	}
	if len(fake.cart) != 2 || fake.cart[0] != (cartItem{UPC: "0001111041700", Quantity: 2, Modality: "PICKUP"}) {
		t.Errorf("cart = %+v", fake.cart)
	}
	if err := p.AddToCart(ctx, "expired", items); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expired token: %v, want ErrUnauthorized", err)
	}
	fake.status = http.StatusTooManyRequests
	err := p.AddToCart(ctx, "member-access", items)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "member-access") {
		t.Errorf("rate limited: %v", err)
	}
}

func TestSeveralBannersShareOneProvider(t *testing.T) {
	fake := &fakeKroger{t: t}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	p := New(Options{ClientID: "client-1", ClientSecret: "secret-1", APIBase: srv.URL, Banner: "frys", Banners: []string{"ralphs", "nope", "frys"}})
	if got := len(p.Banners()); got != 2 {
		t.Fatalf("banners = %+v: unknown and repeated keys are dropped", p.Banners())
	}
	ctx := context.Background()

	stores, err := p.FindStores(ctx, providers.StoreQuery{ZIPCode: "90001"})
	if err != nil || len(stores) != 2 {
		t.Fatalf("FindStores = %+v, %v; want Ralphs and Fry's, not Food 4 Less", stores, err)
	}
	if stores[0].Banner != "ralphs" || stores[0].Name != "Ralphs Test St" || stores[1].Banner != "frys" || stores[1].Name != "Fry's Yuma" {
		t.Errorf("stores = %+v", stores)
	}

	if banner, err := p.StoreBanner(ctx, "70300001"); err != nil || banner != "ralphs" {
		t.Errorf("StoreBanner(Ralphs) = %q, %v", banner, err)
	}
	if _, err := p.StoreBanner(ctx, "03500001"); !errors.Is(err, providers.ErrStoreNotOffered) {
		t.Errorf("StoreBanner(Harris Teeter) = %v, want not offered", err)
	}

	ralphs := p.ForBanner("ralphs")
	if ralphs.Name() != "Ralphs" || ralphs.ProductURL("0001111041700") != "https://www.ralphs.com/p/item/0001111041700" {
		t.Errorf("Ralphs view = %s %s", ralphs.Name(), ralphs.ProductURL("0001111041700"))
	}
	if p.Name() != "Fry's" || p.ForBanner("dillons").Name() != "Fry's" {
		t.Error("the default stays Fry's, and a banner that isn't on falls back to it")
	}
	// Every view shares one application token.
	if _, err := ralphs.(providers.StoreFinder).FindStores(ctx, providers.StoreQuery{ZIPCode: "90001"}); err != nil {
		t.Fatal(err)
	}
	if n := fake.appTokens.Load(); n != 1 {
		t.Errorf("app tokens fetched = %d, want 1", n)
	}
}

func TestStoreNameDropsTheRepeatedChain(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Fry's Food And Drug - Frys Food And Drug Phoenix", "Fry's Phoenix"},
		{"Fry's Food And Drug - 30th St Thomas", "Fry's 30th St Thomas"},
		{"Fry's Food And Drug - Fry's Food And Drug", "Fry's"},
		{"Fry's Marketplace - Frys Food And Drug Gilbert", "Fry's Gilbert"},
		{"Fry's Marketplace - Frys Marketplace Mesa", "Fry's Mesa"},
		{"Fry's Test Store", "Fry's Test Store"},
	} {
		if got := storeName("Fry's", c.in); got != c.want {
			t.Errorf("storeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSearchDropsLeadingWordsUntilSomethingMatches(t *testing.T) {
	p, _ := newTest(t)
	products, err := p.SearchProducts(context.Background(), providers.SearchQuery{Term: "fresh whole pineapple", StoreID: "66000123"})
	if err != nil || len(products) != 2 {
		t.Fatalf("SearchProducts = %+v, %v", products, err)
	}
}
