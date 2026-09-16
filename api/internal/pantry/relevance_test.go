package pantry

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

func TestStillRelevantStopsALowStockPushAfterARestock(t *testing.T) {
	f := newUsageFixture(t)
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Olive Oil", Source: PurchaseManual, Quantity: "16", Unit: "tbsp"})
	if err != nil {
		t.Fatal(err)
	}
	low := notifications.Notification{
		HouseholdID: testHousehold, Type: notifications.TypePantryLow,
		Subject: notifications.Subject{Kind: notifications.SubjectPantryItem, ID: res.Item.ID},
	}

	// Just bought: in stock, so an alert still waiting to be pushed is stale.
	if ok, err := f.svc.StillRelevant(f.ctx, low); err != nil || ok {
		t.Errorf("StillRelevant(in stock) = %v, %v; want false", ok, err)
	}

	item := cloneItem(f.item(t, res.Item.ID))
	item.Status = StatusLow
	if _, err := f.store.UpdateItem(f.ctx, item); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.svc.StillRelevant(f.ctx, low); err != nil || !ok {
		t.Errorf("StillRelevant(low) = %v, %v; want true", ok, err)
	}

	gone := low
	gone.Subject.ID = "66e5a1f2c3b4a5d6e7f8ffff"
	if ok, err := f.svc.StillRelevant(f.ctx, gone); err != nil || ok {
		t.Errorf("StillRelevant(deleted item) = %v, %v; want false", ok, err)
	}
	if ok, _ := f.svc.StillRelevant(f.ctx, notifications.Notification{Type: notifications.TypeShoppingOrderDue}); !ok {
		t.Error("StillRelevant(shopping.order_due) = false; other types aren't the pantry's to judge")
	}
}
