package shopping

import (
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

var checkT0 = time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

func found(price int64) providers.ProductCheck {
	return providers.ProductCheck{Status: providers.ProductFound, Name: "Fresh Whole Red Onion, Each", PriceCents: cents(price), Pickup: "IN_STOCK", Delivery: "IN_STOCK"}
}

var (
	gone       = providers.ProductCheck{Status: providers.ProductGone}
	unknownNet = providers.ProductCheck{Status: providers.ProductUnknown, Detail: providers.DetailNetwork}
)

func TestHealthFollowsChecks(t *testing.T) {
	p := Preference{ID: "p1", ProductID: "51259215", ProductChosenAt: checkT0}
	if h := p.Health(checkT0.Add(time.Hour)); h != HealthOK {
		t.Fatalf("just chosen = %s, want ok: the member just looked at it", h)
	}
	if h := p.Health(checkT0.Add(ProductFreshness + time.Minute)); h != HealthUnverified {
		t.Fatalf("never checked for 72h = %s, want unverified", h)
	}

	p, becameGone, _ := applyCheck(p, found(85), checkT0.Add(2*time.Hour))
	if becameGone || p.Health(checkT0.Add(ProductFreshness)) != HealthOK || p.Check.Name != "Fresh Whole Red Onion, Each" {
		t.Fatalf("found = %+v", p.Check)
	}
	// An unknown attempt after a fresh answer changes nothing sendable...
	p, _, _ = applyCheck(p, unknownNet, checkT0.Add(3*time.Hour))
	if p.Check.Status != providers.ProductUnknown || p.Check.VerifiedStatus != providers.ProductFound || p.Health(checkT0.Add(4*time.Hour)) != HealthOK {
		t.Fatalf("unknown after found = %+v", p.Check)
	}
	// ...until the last answer is older than the freshness window.
	if h := p.Health(checkT0.Add(2*time.Hour + ProductFreshness + time.Minute)); h != HealthUnverified {
		t.Errorf("stale unknown = %s, want unverified", h)
	}

	p, becameGone, _ = applyCheck(p, gone, checkT0.Add(5*time.Hour))
	if !becameGone || p.Health(checkT0.Add(5*time.Hour)) != HealthGone {
		t.Fatalf("gone = %v %+v", becameGone, p.Check)
	}
	// Gone is sticky through unknown attempts, and not "newly" gone twice.
	p, becameGone, _ = applyCheck(p, unknownNet, checkT0.Add(6*time.Hour))
	if becameGone || p.Health(checkT0.Add(6*time.Hour)) != HealthGone {
		t.Errorf("unknown after gone = %v %s", becameGone, p.Health(checkT0.Add(6*time.Hour)))
	}
	p, becameGone, _ = applyCheck(p, gone, checkT0.Add(7*time.Hour))
	if becameGone {
		t.Error("gone twice notified twice")
	}
	// A later conclusive answer brings it back.
	p, _, _ = applyCheck(p, found(85), checkT0.Add(8*time.Hour))
	if p.Health(checkT0.Add(8*time.Hour)) != HealthOK {
		t.Errorf("found after gone = %s", p.Health(checkT0.Add(8*time.Hour)))
	}

	out := found(85)
	out.Status, out.Pickup, out.Delivery = providers.ProductUnavailable, "OUT_OF_STOCK", "OUT_OF_STOCK"
	p, _, _ = applyCheck(p, out, checkT0.Add(9*time.Hour))
	if h := p.Health(checkT0.Add(9 * time.Hour)); h != HealthUnavailable {
		t.Errorf("unavailable = %s", h)
	}

	// A check of the product the member replaced says nothing about the new
	// one.
	p.ProductID, p.ProductChosenAt = "51259216", checkT0.Add(10*time.Hour)
	if p.CurrentCheck() != nil || p.Health(checkT0.Add(10*time.Hour)) != HealthOK {
		t.Errorf("re-chosen = %+v %s", p.CurrentCheck(), p.Health(checkT0.Add(10*time.Hour)))
	}
}

func TestNeedsRecheck(t *testing.T) {
	p := Preference{ID: "p1", ProductID: "51259215"}
	if !p.needsRecheck(checkT0) {
		t.Error("a product saved before choices were dated needs a check")
	}
	p, _, _ = applyCheck(p, found(85), checkT0)
	if p.needsRecheck(checkT0.Add(HandoffRecheckAge - time.Minute)) {
		t.Error("checked within a day needs no recheck")
	}
	if !p.needsRecheck(checkT0.Add(HandoffRecheckAge + time.Minute)) {
		t.Error("checked over a day ago needs a recheck")
	}
}

func TestApplyCheckPricePrecedence(t *testing.T) {
	at := checkT0

	// No saved price: the listed price is saved.
	p := Preference{ID: "p1", ProductID: "51259215", ProductChosenAt: at}
	p, _, changed := applyCheck(p, found(85), at)
	if !changed || *p.PriceCents != 85 || p.PriceSource != PriceFromProvider || !p.PriceUpdatedAt.Equal(at) {
		t.Fatalf("empty price = %v %v %s", changed, p.PriceCents, p.PriceSource)
	}
	// A price from a check follows the listed price.
	if p, _, changed = applyCheck(p, found(85), at.Add(time.Hour)); changed {
		t.Error("same listed price rewrote the price")
	}
	if p, _, changed = applyCheck(p, found(99), at.Add(2*time.Hour)); !changed || *p.PriceCents != 99 {
		t.Errorf("listed price change = %v %v", changed, *p.PriceCents)
	}

	// The member types $1.20: it stays while Walmart's price doesn't move...
	p.PriceCents, p.PriceSource = cents(120), PriceFromMember
	if p, _, changed = applyCheck(p, found(99), at.Add(3*time.Hour)); changed || *p.PriceCents != 120 || p.PriceSource != PriceFromMember {
		t.Errorf("member price with an unchanged listed price = %v %v %s", changed, *p.PriceCents, p.PriceSource)
	}
	// ...and gives way once Walmart's own price changes after it.
	if p, _, changed = applyCheck(p, found(105), at.Add(4*time.Hour)); !changed || *p.PriceCents != 105 || p.PriceSource != PriceFromProvider {
		t.Errorf("member price after a listed price change = %v %v %s", changed, *p.PriceCents, p.PriceSource)
	}

	// The first check never overrides a member's price: there is no earlier
	// listed price to say it moved.
	fresh := Preference{ID: "p2", ProductID: "51259215", PriceCents: cents(300), PriceSource: PriceFromMember}
	if fresh, _, changed = applyCheck(fresh, found(85), at); changed || *fresh.PriceCents != 300 {
		t.Errorf("first check over a member price = %v %v", changed, *fresh.PriceCents)
	}
	// Gone and unknown answers never touch the price.
	if _, _, changed = applyCheck(fresh, gone, at.Add(time.Hour)); changed {
		t.Error("gone changed the price")
	}
	if _, _, changed = applyCheck(fresh, unknownNet, at.Add(time.Hour)); changed {
		t.Error("unknown changed the price")
	}
	// A found page without a readable price leaves it too.
	noPrice := found(0)
	noPrice.PriceCents = nil
	if _, _, changed = applyCheck(Preference{ID: "p3", ProductID: "1"}, noPrice, at); changed {
		t.Error("no listed price set a price")
	}
}
