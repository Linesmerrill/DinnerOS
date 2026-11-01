package providers

import (
	"errors"
	"testing"
)

func TestKrogerParseProduct(t *testing.T) {
	k := NewKroger("frys")
	if k.Name() != "Fry's" || k.HandoffKind() != HandoffCartWrite || k.CartURL() != "https://www.frysfood.com/cart" {
		t.Fatalf("banner = %+v", k.Banner())
	}
	for in, want := range map[string]struct{ id, name string }{
		"0001111041700": {"0001111041700", ""},
		"11111041700":   {"0011111041700", ""},
		"https://www.frysfood.com/p/test-pineapple-chunks-20-oz/0001111041700?fulfillment=PICKUP": {"0001111041700", "test pineapple chunks 20 oz"},
		"kroger.com/p/test-rice/0001111060933":                                                    {"0001111060933", "test rice"},
	} {
		ref, err := k.ParseProduct(in)
		if err != nil || ref.ProductID != want.id || ref.Name != want.name || ref.URL != "https://www.frysfood.com/p/item/"+want.id {
			t.Errorf("ParseProduct(%q) = %+v, %v", in, ref, err)
		}
	}
	ref, _ := k.ParseProduct("https://www.frysfood.com/p/test-pineapple-chunks-20-oz/0001111041700")
	if ref.Size == nil || ref.Size.Quantity.String() != "20" || ref.Size.Unit.Code != "oz" {
		t.Errorf("size = %+v", ref.Size)
	}
	for _, in := range []string{"", "123", "https://www.walmart.com/ip/x/123456", "https://www.frysfood.com/search?q=rice", "https://www.frysfood.com/p/x/123"} {
		var v *ValidationError
		if _, err := k.ParseProduct(in); !errors.As(err, &v) {
			t.Errorf("ParseProduct(%q) error = %v, want a ValidationError", in, err)
		}
	}
	if KrogerBannerFor("nope").Key != "frys" || KrogerBannerFor("Ralphs").Name != "Ralphs" {
		t.Error("banner lookup")
	}
}

func TestKrogerCartLinks(t *testing.T) {
	k := NewKroger("")
	links, err := k.BuildCartLinks([]CartItem{
		{ProductID: "0001111041700", Quantity: 1, LineIDs: []string{"l1"}},
		{ProductID: "0001111060933", Quantity: 2, LineIDs: []string{"l2"}},
		{ProductID: "0001111041700", Quantity: 2, LineIDs: []string{"l3"}},
	}, StoreRef{StoreID: "70100123"})
	if err != nil || len(links) != 1 || links[0].URL != "https://www.frysfood.com/cart" || len(links[0].Items) != 2 ||
		links[0].Items[0].Quantity != 3 || len(links[0].Items[0].LineIDs) != 2 {
		t.Fatalf("links = %+v, %v", links, err)
	}
	if links, err := k.BuildCartLinks(nil, StoreRef{}); err != nil || links != nil {
		t.Errorf("no items = %+v, %v", links, err)
	}
	for _, items := range [][]CartItem{{{ProductID: "123", Quantity: 1}}, {{ProductID: "0001111041700", Quantity: 0}}} {
		if _, err := k.BuildCartLinks(items, StoreRef{}); err == nil {
			t.Errorf("BuildCartLinks(%+v) accepted", items)
		}
	}
	if id, err := k.NormalizeStoreID(" 701abc23 "); err != nil || id != "701ABC23" {
		t.Errorf("NormalizeStoreID = %q, %v", id, err)
	}
	if _, err := k.NormalizeStoreID("123"); err == nil {
		t.Error("short store ID accepted")
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]string{"20 oz": "20 oz", "46 fl oz": "46 floz", "12 ct": "12 count", "2.5 lb": "5/2 lb", "1 gal": "128 floz", "1/2 gal": "64 floz"} {
		got := ParseSize(in)
		if got == nil || got.Quantity.String()+" "+got.Unit.Code != want {
			t.Errorf("ParseSize(%q) = %+v, want %s", in, got, want)
		}
	}
	for _, in := range []string{"", "family size", "2 - 3 lb", "6 ct 12 oz"} {
		if got := ParseSize(in); got != nil {
			t.Errorf("ParseSize(%q) = %+v, want nil", in, got)
		}
	}
}

func TestKrogerServesSeveralBanners(t *testing.T) {
	k := NewKroger("frys", "ralphs", "RALPHS", "harristeeter", "frys")
	if got := len(k.Banners()); got != 2 || k.Banners()[0].Key != "frys" {
		t.Fatalf("banners = %+v, want Fry's then Ralphs", k.Banners())
	}
	if keys := k.CatalogKeys(); len(keys) != 2 || keys[0] != "frys" || keys[1] != "ralphs" {
		t.Errorf("catalog keys = %v", keys)
	}
	ralphs := k.WithBanner("ralphs")
	if ralphs.Name() != "Ralphs" || ralphs.CartURL() != "https://www.ralphs.com/cart" || len(ralphs.Banners()) != 2 {
		t.Errorf("Ralphs view = %s %s %d", ralphs.Name(), ralphs.CartURL(), len(ralphs.Banners()))
	}
	if k.WithBanner("dillons") != k || k.WithBanner("") != k {
		t.Error("a banner that isn't on keeps the default")
	}
	if b, ok := k.BannerForChain("ralphs"); !ok || b.Key != "ralphs" {
		t.Errorf("chain RALPHS = %+v, %v", b, ok)
	}
	if _, ok := k.BannerForChain("DILLONS"); ok {
		t.Error("a chain that isn't on matched")
	}
	if len(KrogerBannerKeys()) != 15 {
		t.Errorf("known banners = %v", KrogerBannerKeys())
	}
}
