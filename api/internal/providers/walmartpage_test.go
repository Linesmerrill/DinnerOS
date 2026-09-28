package providers

import (
	"encoding/json"
	"testing"
)

// syntheticPage wraps a minimal, made-up product object in the page shape
// Walmart served on 2026-09-27: a __NEXT_DATA__ script whose
// props.pageProps.initialData.data.product holds the item. Nothing here was
// captured from a real page.
func syntheticPage(t *testing.T, product any) []byte {
	t.Helper()
	data := map[string]any{"props": map[string]any{"pageProps": map[string]any{"initialData": map[string]any{"data": map[string]any{"product": product}}}}}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(`<!doctype html><html><head><title>t</title></head><body><div id="__next"></div>` +
		`<script id="__NEXT_DATA__" type="application/json">` + string(raw) + `</script></body></html>`)
}

func product(id string, pickup, delivery string) map[string]any {
	return map[string]any{
		"usItemId": id, "name": "Fresh Whole Red Onion, Each", "availabilityStatus": "IN_STOCK",
		"priceInfo": map[string]any{"currentPrice": map[string]any{"price": 0.85, "priceString": "$0.85"}},
		"fulfillmentOptions": []any{
			map[string]any{"type": "SHIPPING", "availabilityStatus": "OUT_OF_STOCK"},
			map[string]any{"type": "PICKUP", "availabilityStatus": pickup, "locationText": "Test Supercenter"},
			map[string]any{"type": "DELIVERY", "availabilityStatus": delivery},
		},
	}
}

func TestParseWalmartProductPageFound(t *testing.T) {
	// The measured red onion: shipping out of stock, pickup and delivery in
	// stock, $0.85.
	got := ParseWalmartProductPage("51259215", 200, "/ip/Fresh-Whole-Red-Onion-Each/51259215", syntheticPage(t, product("51259215", "IN_STOCK", "IN_STOCK")))
	if got.Status != ProductFound || got.Name != "Fresh Whole Red Onion, Each" || got.PriceCents == nil || *got.PriceCents != 85 ||
		got.Pickup != "IN_STOCK" || got.Delivery != "IN_STOCK" || got.Store != "Test Supercenter" || got.Detail != "" {
		t.Fatalf("ParseWalmartProductPage() = %+v", got)
	}
	// Either channel in stock is enough.
	if got := ParseWalmartProductPage("51259215", 200, "/ip/x/51259215", syntheticPage(t, product("51259215", "OUT_OF_STOCK", "LIMITED_STOCK"))); got.Status != ProductFound {
		t.Errorf("delivery only = %+v", got)
	}
}

func TestParseWalmartProductPageGone(t *testing.T) {
	// A dead item redirects to /ip/seort/<id> and answers 404.
	if got := ParseWalmartProductPage("999999999", 404, "/ip/seort/999999999", []byte("<html>not found</html>")); got.Status != ProductGone {
		t.Fatalf("404 = %+v, want gone", got)
	}
	// A 404 somewhere else on the site is not an answer about the item.
	if got := ParseWalmartProductPage("999999999", 404, "/search", nil); got.Status != ProductUnknown || got.Detail != DetailOffSite {
		t.Errorf("404 off the product path = %+v, want unknown", got)
	}
}

func TestParseWalmartProductPageUnavailable(t *testing.T) {
	got := ParseWalmartProductPage("51259215", 200, "/ip/x/51259215", syntheticPage(t, product("51259215", "OUT_OF_STOCK", "OUT_OF_STOCK")))
	if got.Status != ProductUnavailable || got.Name == "" || got.PriceCents == nil {
		t.Fatalf("no pickup or delivery = %+v, want unavailable with name and price", got)
	}
	// Shipping only: not in stock for pickup or delivery.
	p := product("51259215", "", "")
	p["fulfillmentOptions"] = []any{map[string]any{"type": "SHIPPING", "availabilityStatus": "IN_STOCK"}}
	if got := ParseWalmartProductPage("51259215", 200, "/ip/x/51259215", syntheticPage(t, p)); got.Status != ProductUnavailable {
		t.Errorf("shipping only = %+v, want unavailable", got)
	}
	// A status value we've never seen is not in stock.
	if got := ParseWalmartProductPage("51259215", 200, "/ip/x/51259215", syntheticPage(t, product("51259215", "MAYBE", "SOON"))); got.Status != ProductUnavailable {
		t.Errorf("unknown values = %+v, want unavailable", got)
	}
}

func TestParseWalmartProductPageUnknownNeverFound(t *testing.T) {
	missingOptions := product("51259215", "IN_STOCK", "IN_STOCK")
	delete(missingOptions, "fulfillmentOptions")
	noName := product("51259215", "IN_STOCK", "IN_STOCK")
	noName["name"] = "  "
	for _, tc := range []struct {
		name   string
		status int
		path   string
		body   []byte
		detail string
	}{
		{"garbage", 200, "/ip/x/51259215", []byte("<html><body>Robot or human?</body></html>"), DetailNoPageData},
		{"broken json", 200, "/ip/x/51259215", []byte(`<script id="__NEXT_DATA__" type="application/json">{"props":</script>`), DetailNoPageData},
		{"moved product object", 200, "/ip/x/51259215", syntheticPage(t, nil), DetailNoProduct},
		{"no name", 200, "/ip/x/51259215", syntheticPage(t, noName), DetailNoProduct},
		{"different item", 200, "/ip/x/51259216", syntheticPage(t, product("51259216", "IN_STOCK", "IN_STOCK")), DetailDifferentItem},
		{"no fulfillment", 200, "/ip/x/51259215", syntheticPage(t, missingOptions), DetailNoFulfillment},
		{"redirected off the product path", 200, "/blocked", syntheticPage(t, product("51259215", "IN_STOCK", "IN_STOCK")), DetailOffSite},
		{"other status", 410, "/ip/x/51259215", nil, DetailUnexpected},
	} {
		got := ParseWalmartProductPage("51259215", tc.status, tc.path, tc.body)
		if got.Status != ProductUnknown || got.Detail != tc.detail || got.Name != "" {
			t.Errorf("%s = %+v, want unknown (%s)", tc.name, got, tc.detail)
		}
	}
}

func TestParseWalmartPrice(t *testing.T) {
	for in, want := range map[string]int64{"$0.85": 85, "$12": 1200, "$1,024.50": 102450, " $3.99 ": 399} {
		if got := parseWalmartPrice(in); got == nil || *got != want {
			t.Errorf("parseWalmartPrice(%q) = %v, want %d", in, got, want)
		}
	}
	for _, in := range []string{"", "$1.24/lb", "From $3", "3.99", "$3.9", "$1,00.00"} {
		if got := parseWalmartPrice(in); got != nil {
			t.Errorf("parseWalmartPrice(%q) = %d, want nil", in, *got)
		}
	}
	// A page with an unreadable price is still found; it just has no price.
	p := product("51259215", "IN_STOCK", "IN_STOCK")
	p["priceInfo"] = map[string]any{"currentPrice": map[string]any{"priceString": "$1.24/lb"}}
	if got := ParseWalmartProductPage("51259215", 200, "/ip/x/51259215", syntheticPage(t, p)); got.Status != ProductFound || got.PriceCents != nil {
		t.Errorf("unreadable price = %+v", got)
	}
}
