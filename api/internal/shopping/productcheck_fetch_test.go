package shopping

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// fakeWalmartPage renders a minimal, synthesized product page in the shape
// Walmart served on 2026-09-27 (see providers/walmartpage.go): the product
// under props.pageProps.initialData.data.product in __NEXT_DATA__. Nothing
// here was captured from a real page.
func fakeWalmartPage(t *testing.T, id, name, price, pickup, delivery string) string {
	t.Helper()
	product := map[string]any{
		"usItemId": id, "name": name, "availabilityStatus": "IN_STOCK",
		"priceInfo": map[string]any{"currentPrice": map[string]any{"priceString": price}},
		"fulfillmentOptions": []any{
			map[string]any{"type": "SHIPPING", "availabilityStatus": "OUT_OF_STOCK"},
			map[string]any{"type": "PICKUP", "availabilityStatus": pickup, "locationText": "Test Supercenter"},
			map[string]any{"type": "DELIVERY", "availabilityStatus": delivery},
		},
	}
	raw, err := json.Marshal(map[string]any{"props": map[string]any{"pageProps": map[string]any{"initialData": map[string]any{"data": map[string]any{"product": product}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return `<html><body><script id="__NEXT_DATA__" type="application/json">` + string(raw) + `</script></body></html>`
}

// fakeWalmart serves /ip/<id> the way walmart.com did when measured: a live
// item redirects to /ip/<slug>/<id> and answers 200; a dead one redirects to
// /ip/seort/<id> and answers 404.
type fakeWalmart struct {
	t     *testing.T
	mu    sync.Mutex
	pages map[string]string // id → body; "" is dead
	codes map[string]int    // id → forced status
	paths []string
	agent string
}

func (f *fakeWalmart) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.Path)
	f.agent = r.Header.Get("User-Agent")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "ip" {
		http.NotFound(w, r)
		return
	}
	id := parts[len(parts)-1]
	if code, ok := f.codes[id]; ok {
		if code == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "30")
		}
		w.WriteHeader(code)
		return
	}
	body, live := f.pages[id]
	switch {
	case len(parts) == 2 && live && body != "":
		http.Redirect(w, r, "/ip/Test-Product/"+id, http.StatusMovedPermanently)
	case len(parts) == 2:
		http.Redirect(w, r, "/ip/seort/"+id, http.StatusMovedPermanently)
	case parts[1] == "seort":
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>Not found</html>"))
	default:
		_, _ = w.Write([]byte(body))
	}
}

func newFakeChecker(t *testing.T, fake *fakeWalmart) (*WalmartChecker, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	c, err := NewWalmartChecker(WalmartCheckerOptions{
		BaseURL: srv.URL, MinInterval: time.Second, Attempts: 1,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestWalmartCheckerReadsMeasuredShapes(t *testing.T) {
	fake := &fakeWalmart{t: t, pages: map[string]string{
		"51259215": fakeWalmartPage(t, "51259215", "Fresh Whole Red Onion, Each", "$0.85", "IN_STOCK", "IN_STOCK"),
		"51259216": fakeWalmartPage(t, "51259216", "Out Of Stock Thing", "$2.50", "OUT_OF_STOCK", "OUT_OF_STOCK"),
		"51259217": "<html><body>Robot or human?</body></html>",
		"99999999": "",
	}, codes: map[string]int{}}
	c, _ := newFakeChecker(t, fake)
	ctx := context.Background()

	got, err := c.CheckProduct(ctx, providers.KeyWalmart, "51259215")
	if err != nil || got.Status != providers.ProductFound || got.Name != "Fresh Whole Red Onion, Each" || got.PriceCents == nil || *got.PriceCents != 85 {
		t.Fatalf("200 with product = %+v, %v", got, err)
	}
	if fake.agent != ProductCheckUserAgent || !strings.HasPrefix(fake.agent, "DinnerOS/1.0 (+https://api.tlps.dev") {
		t.Errorf("User-Agent = %q", fake.agent)
	}
	if got, err := c.CheckProduct(ctx, providers.KeyWalmart, "99999999"); err != nil || got.Status != providers.ProductGone {
		t.Errorf("404 = %+v, %v, want gone", got, err)
	}
	if got, err := c.CheckProduct(ctx, providers.KeyWalmart, "51259216"); err != nil || got.Status != providers.ProductUnavailable || got.Name == "" {
		t.Errorf("no pickup or delivery = %+v, %v, want unavailable", got, err)
	}
	if got, err := c.CheckProduct(ctx, providers.KeyWalmart, "51259217"); err != nil || got.Status != providers.ProductUnknown || got.Detail != providers.DetailNoPageData {
		t.Errorf("garbage page = %+v, %v, want unknown", got, err)
	}
	if strings.Join(fake.paths, " ") != "/ip/51259215 /ip/Test-Product/51259215 /ip/99999999 /ip/seort/99999999 /ip/51259216 /ip/Test-Product/51259216 /ip/51259217 /ip/Test-Product/51259217" {
		t.Errorf("paths = %v", fake.paths)
	}
	// Nothing but a product page is ever requested.
	if got, err := c.CheckProduct(ctx, providers.KeyWalmart, "../search"); err != nil || got.Status != providers.ProductUnknown {
		t.Errorf("bad id = %+v, %v", got, err)
	}
}

func TestWalmartCheckerStopsOnRefusalAndThrottle(t *testing.T) {
	fake := &fakeWalmart{t: t, pages: map[string]string{}, codes: map[string]int{"11111111": http.StatusForbidden, "22222222": http.StatusTooManyRequests, "33333333": http.StatusBadGateway}}
	c, _ := newFakeChecker(t, fake)
	ctx := context.Background()
	var stop *CheckStop
	got, err := c.CheckProduct(ctx, providers.KeyWalmart, "11111111")
	if !errors.As(err, &stop) || !stop.Blocked || got.Status != providers.ProductUnknown || got.Detail != providers.DetailBlocked {
		t.Errorf("403 = %+v, %v, want unknown and a blocked stop", got, err)
	}
	got, err = c.CheckProduct(ctx, providers.KeyWalmart, "22222222")
	if !errors.As(err, &stop) || stop.Blocked || got.Status != providers.ProductUnknown || got.Detail != providers.DetailThrottled {
		t.Errorf("429 = %+v, %v, want unknown and a throttle stop", got, err)
	}
	if got, err := c.CheckProduct(ctx, providers.KeyWalmart, "33333333"); err != nil || got.Status != providers.ProductUnknown || got.Detail != providers.DetailServerError {
		t.Errorf("502 = %+v, %v, want unknown", got, err)
	}
}

func TestWalmartCheckerNetworkErrorIsUnknown(t *testing.T) {
	c, srv := newFakeChecker(t, &fakeWalmart{t: t})
	srv.Close()
	got, err := c.CheckProduct(context.Background(), providers.KeyWalmart, "51259215")
	if err != nil || got.Status != providers.ProductUnknown || got.Detail != providers.DetailNetwork {
		t.Fatalf("network error = %+v, %v, want unknown with no stop", got, err)
	}
}

func TestWalmartCheckerRedirectsOffSiteOrToTheBotWall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ip/44444444":
			http.Redirect(w, r, "https://example.com/ip/44444444", http.StatusFound)
		case "/ip/55555555":
			http.Redirect(w, r, "/blocked?url=x", http.StatusFound)
		default:
			_, _ = w.Write([]byte("<html>press and hold</html>"))
		}
	}))
	defer srv.Close()
	c, err := NewWalmartChecker(WalmartCheckerOptions{BaseURL: srv.URL, Attempts: 1, Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.CheckProduct(context.Background(), providers.KeyWalmart, "44444444"); err != nil || got.Status != providers.ProductUnknown || got.Detail != providers.DetailOffSite {
		t.Errorf("off-site redirect = %+v, %v", got, err)
	}
	var stop *CheckStop
	if got, err := c.CheckProduct(context.Background(), providers.KeyWalmart, "55555555"); !errors.As(err, &stop) || !stop.Blocked || got.Status != providers.ProductUnknown {
		t.Errorf("bot wall = %+v, %v, want a blocked stop", got, err)
	}
}
