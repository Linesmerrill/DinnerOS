package pantry

import (
	"errors"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

func TestRecordProviderPurchase(t *testing.T) {
	f := newUsageFixture(t)
	line := ProviderRef{Key: "walmart", HandoffID: "66e5a1f2c3b4a5d6e7f80b11", LineID: "l1", ProductID: "123456789"}
	in := ProviderPurchaseInput{
		IngredientID: f.catalog.id("Butter"), Quantity: "2", Unit: "package",
		UnitSize: &UnitSize{Unit: "package", Quantity: "16", SizeUnit: "oz"}, Week: "2026-W38", Provider: line,
	}
	res, err := f.svc.RecordProviderPurchase(f.ctx, f.actor, in)
	if err != nil || !res.Created {
		t.Fatalf("RecordProviderPurchase() = %+v, %v", res, err)
	}
	p, item := res.Purchase, res.Item
	if p.Source != PurchaseProvider || p.Provider == nil || *p.Provider != line || p.Week != "2026-W38" || p.Quantity != "2" || p.Unit != "package" {
		t.Errorf("purchase = %+v", p)
	}
	if item.Status != StatusInStock || item.Tracking == nil || item.Tracking.CycleSource != CycleProvider || item.Tracking.Reference != "32" ||
		item.Tracking.Unit != "oz" || item.UnitSize == nil || item.UnitSize.Quantity != "16" {
		t.Errorf("item = %+v tracking %+v", item, item.Tracking)
	}

	// The same line again, even by another member with other amounts, returns
	// the first purchase and changes nothing.
	other := households.Membership{HouseholdID: testHousehold, UserID: "66e5a1f2c3b4a5d6e7f80c02", Role: households.RoleMember}
	f.advance(3600e9)
	in.Quantity = "5"
	again, err := f.svc.RecordProviderPurchase(f.ctx, other, in)
	if err != nil || again.Created || again.Purchase.ID != p.ID || again.Item.Tracking.Reference != "32" {
		t.Errorf("repeat = %+v, %v", again, err)
	}
	if found, ok, err := f.svc.FindProviderPurchase(f.ctx, testHousehold, line.HandoffID, "l2"); err != nil || ok {
		t.Errorf("FindProviderPurchase(l2) = %+v, %v, %v", found, ok, err)
	}

	// A free-text line and a converted count.
	res, err = f.svc.RecordProviderPurchase(f.ctx, f.actor, ProviderPurchaseInput{
		Name: "Large Eggs", Quantity: "24", Unit: "count", Provider: ProviderRef{Key: "walmart", HandoffID: line.HandoffID, LineID: "l2", ProductID: "223456789"},
	})
	if err != nil || res.Item.Key != "large eggs" || res.Item.Quantity != "24" || res.Purchase.UnitSize != nil {
		t.Errorf("eggs = %+v, %v", res, err)
	}

	for _, tc := range []struct {
		in   ProviderPurchaseInput
		want string
	}{
		{ProviderPurchaseInput{Name: "Rice", Provider: ProviderRef{Key: "walmart", HandoffID: "h", LineID: "l9"}}, "needs a quantity"},
		{ProviderPurchaseInput{Name: "Rice", Quantity: "1", Unit: "package", UnitSize: &UnitSize{Unit: "can", Quantity: "2", SizeUnit: "lb"}, Provider: ProviderRef{Key: "walmart", HandoffID: "h", LineID: "l9"}}, "purchase unit"},
		{ProviderPurchaseInput{Name: "Rice", Quantity: "1", Unit: "package", UnitSize: &UnitSize{Unit: "package", Quantity: "2", SizeUnit: "count"}, Provider: ProviderRef{Key: "walmart", HandoffID: "h", LineID: "l9"}}, "volume or weight"},
		{ProviderPurchaseInput{Name: "Rice", Quantity: "1", Week: "38", Provider: ProviderRef{Key: "walmart", HandoffID: "h", LineID: "l9"}}, "ISO week"},
	} {
		_, err := f.svc.RecordProviderPurchase(f.ctx, f.actor, tc.in)
		wantValidation(t, err, tc.want)
	}
	if _, err := f.svc.RecordProviderPurchase(f.ctx, f.actor, ProviderPurchaseInput{Name: "Rice", Quantity: "1"}); err == nil {
		t.Error("a purchase without a handoff line succeeded")
	}
	viewer := households.Membership{HouseholdID: testHousehold, UserID: testUser, Role: "viewer"}
	if _, err := f.svc.RecordProviderPurchase(f.ctx, viewer, in); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer error = %v", err)
	}
}
