package shopping

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// fakeInstacart makes list pages in memory: the pure Instacart provider plus
// a recorded MakeListPage.
type fakeInstacart struct {
	*providers.Instacart
	pages []fakePage
	err   error
}

type fakePage struct {
	title string
	items []providers.ListItem
}

func (i *fakeInstacart) MakeListPage(_ context.Context, title string, items []providers.ListItem) (string, error) {
	if i.err != nil {
		return "", i.err
	}
	i.pages = append(i.pages, fakePage{title: title, items: items})
	return "https://www.instacart.com/store/shopping_lists/" + strconv.Itoa(len(i.pages)) + "?aff_id=1", nil
}

func (i *fakeInstacart) item(page int, name string) (providers.ListItem, bool) {
	for _, it := range i.pages[page].items {
		if it.Name == name {
			return it, true
		}
	}
	return providers.ListItem{}, false
}

// withInstacart rebuilds the fixture's service with Walmart and the fake
// Instacart provider.
func (f *fixture) withInstacart(t *testing.T) *fakeInstacart {
	t.Helper()
	ic := &fakeInstacart{Instacart: providers.NewInstacart(true)}
	svc := NewService(ServiceOptions{
		Store: f.store, Providers: providers.NewRegistry(providers.NewWalmart(providers.WalmartOptions{}), ic),
		Grocery: f.plans, Catalog: f.recipes, Pantry: f.pantry, Plans: f.plans, Events: f.events,
		Members: fakeMembers{}, TokenSecret: []byte("a test signing key that is long enough for hmac"),
	})
	svc.now = func() time.Time { return *f.clock }
	f.svc = svc
	return ic
}

// With Instacart, every item on the list goes to one Instacart page by name
// and amount, no saved products needed; a later send lists only what's new,
// and nothing is recorded when Instacart fails. Walmart is untouched.
func TestIntegrationInstacartListPage(t *testing.T) {
	f := newFixture(t)
	ic := f.withInstacart(t)
	ctx := f.ctx

	// Walmart stays the default; Instacart is a second store with no store ID.
	if _, err := f.svc.UpdateStore(ctx, f.actor, "walmart", "", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateStore(ctx, f.actor, "instacart", "123", "", false); err == nil {
		t.Error("Instacart took a store ID")
	}
	s, err := f.svc.UpdateStore(ctx, f.actor, "instacart", "", "", false)
	if err != nil || s.Provider != providers.KeyWalmart || len(s.Stores) != 2 {
		t.Fatalf("settings = %+v, %v", s, err)
	}
	if _, _, err := f.svc.PutPreference(ctx, f.actor, "instacart", f.keys["Milk"], PreferenceInput{ProductURL: "123456789"}); err == nil {
		t.Error("saved a product for Instacart")
	}

	// The match needs no saved products: every item to buy is a line.
	m, err := f.svc.Match(ctx, testHousehold, testWeek, "instacart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !m.AffiliateTracked || len(m.Lines) == 0 || len(m.Links) != 1 || m.Links[0].URL != "" {
		t.Fatalf("match = %d lines, links %+v, tracked %v", len(m.Lines), m.Links, m.AffiliateTracked)
	}
	for _, l := range m.Lines {
		if l.ProductID != "" || l.Packages != 1 || l.Reason != "" {
			t.Errorf("line %s = product %q, %d packages, reason %q", l.Name, l.ProductID, l.Packages, l.Reason)
		}
	}
	if slices.ContainsFunc(m.Excluded, func(e Excluded) bool { return e.Reason == ExcludedNoProduct }) {
		t.Error("an item was left out for having no saved product")
	}

	// Instacart fails: nothing is recorded.
	ic.err = errors.New("instacart: 503")
	if _, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "instacart", MatchInput{}); !errors.Is(err, ErrCartUnavailable) {
		t.Fatalf("failed send: %v", err)
	}
	if hs, _ := f.svc.ListHandoffs(ctx, testHousehold, HandoffFilter{}); len(hs) != 0 {
		t.Errorf("a failed send stored %d handoffs", len(hs))
	}

	ic.err = nil
	h, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "instacart", MatchInput{})
	if err != nil || !isNew || len(h.Links) != 1 || h.Links[0].URL != "https://www.instacart.com/store/shopping_lists/1?aff_id=1" {
		t.Fatalf("send = %+v, %v, %v", h.Links, isNew, err)
	}
	if len(ic.pages) != 1 || ic.pages[0].title != "Dinners for the week" || len(ic.pages[0].items) != len(h.Lines) {
		t.Fatalf("pages = %+v", ic.pages)
	}
	// Amounts go in Instacart's units: 4 cloves of garlic are one head, the
	// onions are each, the milk a cup.
	want := map[string]providers.ListMeasurement{
		"Garlic": {Quantity: 1, Unit: "each"}, "Yellow Onion": {Quantity: 2, Unit: "each"}, "Milk": {Quantity: 1, Unit: "cup"},
	}
	for name, m := range want {
		it, ok := ic.item(0, name)
		if !ok || len(it.Measurements) != 1 || it.Measurements[0] != m {
			t.Errorf("%s = %+v (%v)", name, it, ok)
		}
	}

	// Nothing new: no page, no links.
	again, isNew, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "instacart", MatchInput{})
	if err != nil || isNew || len(again.Links) != 0 || len(ic.pages) != 1 {
		t.Fatalf("second send = %+v, %v, %v (pages %d)", again.Links, isNew, err, len(ic.pages))
	}

	// Walmart's own week is separate and unchanged.
	if wm, err := f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{}); err != nil || wm.Cart != nil {
		t.Errorf("walmart match = %+v, %v", wm.Cart, err)
	}
}
