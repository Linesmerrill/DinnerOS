package instacart

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func items() []providers.ListItem {
	return []providers.ListItem{
		{Name: "Yellow Onion", Measurements: []providers.ListMeasurement{{Quantity: 2, Unit: "each"}}},
		{Name: "Salt"},
	}
}

// The page request carries the key, the list's names and measurements, and
// nothing else; the link comes back as https.
func TestMakeListPage(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/idp/v1/products/products_link" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers = %v", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"products_link_url":"http://www.instacart.com/store/shopping_lists/1?aff_id=123&offer_id=1&affiliate_platform=idp_partner"}`))
	}))
	defer srv.Close()

	p := New(Options{APIKey: "test-key", APIBase: srv.URL + "/", Affiliate: true, LinkbackURL: "https://example.test/shop"})
	link, err := p.MakeListPage(context.Background(), "  Dinners   for the week of November 9 ", items())
	if err != nil {
		t.Fatal(err)
	}
	if link != "https://www.instacart.com/store/shopping_lists/1?aff_id=123&offer_id=1&affiliate_platform=idp_partner" {
		t.Errorf("link = %s", link)
	}
	if got["title"] != "Dinners for the week of November 9" || got["link_type"] != "shopping_list" || got["expires_in"] != float64(30) {
		t.Errorf("body = %v", got)
	}
	lines := got["line_items"].([]any)
	onion := lines[0].(map[string]any)
	m := onion["line_item_measurements"].([]any)[0].(map[string]any)
	if onion["name"] != "Yellow Onion" || m["quantity"] != float64(2) || m["unit"] != "each" {
		t.Errorf("onion = %v", onion)
	}
	if salt := lines[1].(map[string]any); salt["name"] != "Salt" || salt["line_item_measurements"] != nil {
		t.Errorf("salt = %v", salt)
	}
	// Deprecated per-item quantity and unit are never sent.
	if _, ok := onion["quantity"]; ok {
		t.Error("sent the deprecated quantity")
	}
	if lp := got["landing_page_configuration"].(map[string]any); lp["partner_linkback_url"] != "https://example.test/shop" {
		t.Errorf("landing page = %v", lp)
	}
	if !p.AffiliateTracked() || p.Key() != providers.KeyInstacart || p.HandoffKind() != providers.HandoffListPage {
		t.Error("provider identity")
	}
}

// Every failure is ErrUnavailable, carrying Instacart's message but never the
// key.
func TestMakeListPageFailures(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"refused key": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"bad request": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid quantity: -0.1. Cannot be lower than or equal to 0.0","code":1001}}`))
		},
		"down":    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"no link": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) },
		"not a link": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"products_link_url":"javascript:alert(1)"}`))
		},
		"not json": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) },
	}
	for name, h := range cases {
		srv := httptest.NewServer(h)
		_, err := New(Options{APIKey: "secret-key", APIBase: srv.URL}).MakeListPage(context.Background(), "List", items())
		srv.Close()
		if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret-key") {
			t.Errorf("%s: %v", name, err)
		}
		if name == "bad request" && !strings.Contains(err.Error(), "Invalid quantity") {
			t.Errorf("bad request message = %v", err)
		}
	}
	if _, err := New(Options{APIKey: "k"}).MakeListPage(context.Background(), "List", nil); err == nil {
		t.Error("made a page with no items")
	}
}

func TestTrimTitle(t *testing.T) {
	if trimTitle("   ") != "Groceries" {
		t.Error("empty title")
	}
	if got := trimTitle(strings.Repeat("é", 150)); len([]rune(got)) != MaxTitleLength {
		t.Errorf("long title = %d runes", len([]rune(got)))
	}
}
