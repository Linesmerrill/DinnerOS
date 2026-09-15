package providers

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Item IDs in these tests are made up.

func TestWalmartParseProduct(t *testing.T) {
	w := NewWalmart(WalmartOptions{})
	good := []struct{ name, in, id string }{
		{"raw id", "123456789", "123456789"},
		{"raw id with spaces", "  55512345 \n", "55512345"},
		{"slug", "https://www.walmart.com/ip/Great-Value-Test-Beans-15-oz/123456789", "123456789"},
		{"no slug", "https://www.walmart.com/ip/123456789", "123456789"},
		{"query string", "https://www.walmart.com/ip/Test-Rice-2-lb/98765432?classType=REGULAR&athbdg=L1600&from=/search", "98765432"},
		{"fragment", "https://www.walmart.com/ip/Test-Rice/98765432#reviews", "98765432"},
		{"trailing slash", "https://www.walmart.com/ip/Test-Rice/98765432/", "98765432"},
		{"bare host", "https://walmart.com/ip/Test-Rice/98765432", "98765432"},
		{"http", "http://www.walmart.com/ip/98765432", "98765432"},
		{"no scheme", "www.walmart.com/ip/Test-Rice/98765432", "98765432"},
		{"no scheme bare host", "walmart.com/ip/98765432?selected=true", "98765432"},
		{"upper-case host", "https://WWW.Walmart.COM/ip/Test/98765432", "98765432"},
		{"percent-encoded slug", "https://www.walmart.com/ip/Caf%C3%A9-Test-Coffee/100000001", "100000001"},
		{"15 digits", "https://www.walmart.com/ip/x/123456789012345", "123456789012345"},
	}
	for _, tt := range good {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := w.ParseProduct(tt.in)
			if err != nil {
				t.Fatalf("ParseProduct(%q) error = %v", tt.in, err)
			}
			if ref.ProductID != tt.id || ref.URL != "https://www.walmart.com/ip/"+tt.id {
				t.Errorf("ParseProduct(%q) = %+v, want item %s", tt.in, ref, tt.id)
			}
		})
	}

	bad := []struct{ name, in string }{
		{"empty", "   "},
		{"short id", "1234"},
		{"leading zero id", "0123456789"},
		{"16 digits", "1234567890123456"},
		{"other host", "https://www.target.com/ip/Test/123456789"},
		{"lookalike host", "https://www.walmart.com.example.com/ip/Test/123456789"},
		{"suffix host", "https://notwalmart.com/ip/Test/123456789"},
		{"subdomain", "https://affil.walmart.com/ip/Test/123456789"},
		{"userinfo", "https://www.walmart.com@evil.example/ip/Test/123456789"},
		{"userinfo on walmart", "https://user@www.walmart.com/ip/Test/123456789"},
		{"port", "https://www.walmart.com:8443/ip/Test/123456789"},
		{"short link", "https://walmrt.us/3xAmPlE"},
		{"ftp", "ftp://www.walmart.com/ip/Test/123456789"},
		{"javascript", "javascript:alert(1)//www.walmart.com/ip/123456789"},
		{"search page", "https://www.walmart.com/search?q=beans"},
		{"cart link", "https://www.walmart.com/sc/cart/addToCart?items=123456789"},
		{"browse path", "https://www.walmart.com/browse/food/123456789"},
		{"ip without id", "https://www.walmart.com/ip/Test-Beans"},
		{"ip with letters", "https://www.walmart.com/ip/Test/12345abc"},
		{"extra segment", "https://www.walmart.com/ip/Test/seller/123456789"},
		{"empty slug", "https://www.walmart.com/ip//123456789"},
		{"id only in query", "https://www.walmart.com/ip/Test?itemId=123456789"},
		{"text around link", "Check this out https://www.walmart.com/ip/Test/123456789"},
		{"encoded slash in path", "https://www.walmart.com/ip/Test%2F123/123456789"},
		{"too long", "https://www.walmart.com/ip/" + strings.Repeat("a", MaxProductInputLength) + "/123456789"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := w.ParseProduct(tt.in)
			var v *ValidationError
			if !errors.As(err, &v) {
				t.Fatalf("ParseProduct(%q) = %+v, %v; want a ValidationError", tt.in, ref, err)
			}
		})
	}
}

func TestWalmartNormalizeStoreID(t *testing.T) {
	w := NewWalmart(WalmartOptions{})
	for in, want := range map[string]string{"5435": "5435", " 100 ": "100", "007": "7", "999999": "999999"} {
		if got, err := w.NormalizeStoreID(in); err != nil || got != want {
			t.Errorf("NormalizeStoreID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "abc", "12a", "1234567", "-5", "5 435"} {
		if got, err := w.NormalizeStoreID(in); err == nil {
			t.Errorf("NormalizeStoreID(%q) = %q, want an error", in, got)
		}
	}
}

func TestWalmartBuildCartLinks(t *testing.T) {
	w := NewWalmart(WalmartOptions{})
	links, err := w.BuildCartLinks([]CartItem{
		{ProductID: "945190001", Quantity: 1, LineIDs: []string{"l1"}},
		{ProductID: "660760002", Quantity: 2, LineIDs: []string{"l2"}},
		{ProductID: "945190001", Quantity: 2, LineIDs: []string{"l3"}},
	}, StoreRef{StoreID: "5435"})
	if err != nil || len(links) != 1 {
		t.Fatalf("BuildCartLinks() = %+v, %v", links, err)
	}
	want := "https://www.walmart.com/sc/cart/addToCart?items=945190001_3,660760002_2&storeId=5435"
	if links[0].URL != want {
		t.Errorf("URL = %s, want %s", links[0].URL, want)
	}
	if it := links[0].Items; len(it) != 2 || strings.Join(it[0].LineIDs, ",") != "l1,l3" || it[0].Quantity != 3 {
		t.Errorf("items = %+v", it)
	}

	links, err = w.BuildCartLinks([]CartItem{{ProductID: "945190001", Quantity: 1}, {ProductID: "660760002", Quantity: 1}}, StoreRef{})
	if err != nil || len(links) != 1 || links[0].URL != "https://www.walmart.com/sc/cart/addToCart?items=945190001,660760002" {
		t.Errorf("without a store = %+v, %v", links, err)
	}
	if links, err := w.BuildCartLinks(nil, StoreRef{StoreID: "5435"}); err != nil || links != nil {
		t.Errorf("no items = %+v, %v", links, err)
	}

	for name, items := range map[string][]CartItem{
		"bad id":        {{ProductID: "abc", Quantity: 1}},
		"zero quantity": {{ProductID: "945190001", Quantity: 0}},
		"too many":      {{ProductID: "945190001", Quantity: MaxPackages + 1}},
	} {
		if _, err := w.BuildCartLinks(items, StoreRef{}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := w.BuildCartLinks([]CartItem{{ProductID: "945190001", Quantity: 1}}, StoreRef{StoreID: "x"}); err == nil {
		t.Error("bad store: want an error")
	}
	// Merged quantities stop at the maximum.
	links, _ = w.BuildCartLinks([]CartItem{{ProductID: "945190001", Quantity: 60}, {ProductID: "945190001", Quantity: 60}}, StoreRef{})
	if !strings.HasSuffix(links[0].URL, "items=945190001_99") {
		t.Errorf("capped merge = %s", links[0].URL)
	}
}

func TestWalmartBuildCartLinksSplits(t *testing.T) {
	w := NewWalmart(WalmartOptions{})
	var items []CartItem
	for i := range 95 {
		items = append(items, CartItem{ProductID: strconv.Itoa(100000000 + i), Quantity: 1 + i%3, LineIDs: []string{"l" + strconv.Itoa(i+1)}})
	}
	links, err := w.BuildCartLinks(items, StoreRef{StoreID: "5435"})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 {
		t.Fatalf("got %d links, want 3 (40 + 40 + 15)", len(links))
	}
	var seen []string
	for i, l := range links {
		if len(l.URL) > MaxCartURLLength || len(l.Items) > MaxItemsPerCartLink || !strings.HasSuffix(l.URL, "&storeId=5435") {
			t.Errorf("link %d: %d chars, %d items: %s", i, len(l.URL), len(l.Items), l.URL)
		}
		for _, it := range l.Items {
			seen = append(seen, it.LineIDs...)
		}
	}
	if len(seen) != 95 || seen[0] != "l1" || seen[94] != "l95" {
		t.Errorf("lines across links = %v", seen)
	}

	// The length limit applies to the wrapped link.
	tracked := NewWalmart(WalmartOptions{Affiliate: &ImpactAffiliate{PublisherID: "1234567", AdID: "565706", CampaignID: "9383"}})
	if !tracked.AffiliateTracked() || w.AffiliateTracked() {
		t.Fatal("AffiliateTracked() is wrong")
	}
	links, err = tracked.BuildCartLinks(items, StoreRef{StoreID: "5435"})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, l := range links {
		total += len(l.Items)
		if len(l.URL) > MaxCartURLLength {
			t.Errorf("tracked link is %d chars", len(l.URL))
		}
		u, err := url.Parse(l.URL)
		if err != nil || u.Host != "goto.walmart.com" || u.Path != "/m/1234567/565706/9383" || u.Query().Get("veh") != "aff" ||
			!strings.HasPrefix(u.Query().Get("u"), WalmartCartURL+"?items=") {
			t.Errorf("tracked link = %s", l.URL)
		}
	}
	if total != 95 {
		t.Errorf("tracked links hold %d items", total)
	}
	// An incomplete affiliate config is ignored.
	if NewWalmart(WalmartOptions{Affiliate: &ImpactAffiliate{PublisherID: "1"}}).AffiliateTracked() {
		t.Error("an incomplete affiliate config tracks links")
	}
}

func TestRegistryAndCapabilities(t *testing.T) {
	r := NewRegistry(NewWalmart(WalmartOptions{}))
	p, ok := r.Get(KeyWalmart)
	if !ok || len(r.Enabled()) != 1 {
		t.Fatalf("Get(walmart) = %v, %v", p, ok)
	}
	if _, ok := r.Get(KeyInstacart); ok {
		t.Error("instacart is enabled")
	}
	c := CapabilitiesOf(p)
	if c.Handoff != HandoffCartLink || !c.PasteProductLink || c.ProductSearch || c.ProductLookup || c.StoreFinder || c.CartWrite || c.OrderImport {
		t.Errorf("capabilities = %+v", c)
	}
	if !KeyKroger.Known() || Key("amazon").Known() {
		t.Error("Known() is wrong")
	}
	var nilRegistry *Registry
	if _, ok := nilRegistry.Get(KeyWalmart); ok || nilRegistry.Enabled() != nil {
		t.Error("a nil registry has providers")
	}
}
