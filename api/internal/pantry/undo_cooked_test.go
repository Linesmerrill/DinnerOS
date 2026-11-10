package pantry

import (
	"context"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
)

// A meal marked made, then "didn't make it": what it took goes back, and
// marking it made again takes it once more (decision 662).
func TestUndoCookedGivesTheMealsUseBack(t *testing.T) {
	f := newUsageFixture(t)
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{
		Name: "Butter", Source: PurchaseGroceryList, Quantity: "1", Unit: "cup", ClientPurchaseID: "p1",
	})
	if err != nil {
		t.Fatal(err)
	}
	butter := res.Item.ID
	f.advance(time.Hour)
	f.cook(t, "entry1", 4) // 4 Tbsp = 1/4 cup
	f.cook(t, "entry2", 2) // 2 Tbsp = 1/8 cup
	if got := f.item(t, butter).Tracking; got.RecipeUsed != "3/8" || got.RecipeUses != 2 {
		t.Fatalf("after two cooks = %+v", got)
	}

	undone, err := f.svc.UndoCooked(f.ctx, testHousehold, "entry1")
	if err != nil || !undone {
		t.Fatalf("UndoCooked() = %v, %v", undone, err)
	}
	if got := f.item(t, butter).Tracking; got.RecipeUsed != "1/8" || got.SegmentRecipeUsed != "1/8" || got.RecipeUses != 1 {
		t.Errorf("after undo = %+v", got)
	}
	// A second "didn't make it" gives nothing back twice.
	if undone, err := f.svc.UndoCooked(f.ctx, testHousehold, "entry1"); err != nil || undone {
		t.Errorf("second UndoCooked() = %v, %v", undone, err)
	}
	// A meal that was never deducted has nothing to undo.
	if undone, err := f.svc.UndoCooked(f.ctx, testHousehold, "never"); err != nil || undone {
		t.Errorf("UndoCooked(never) = %v, %v", undone, err)
	}
	// Made it after all: deducted again.
	if _, applied := f.cook(t, "entry1", 4); !applied {
		t.Fatal("cook after undo didn't apply")
	}
	if got := f.item(t, butter).Tracking; got.RecipeUsed != "3/8" || got.RecipeUses != 2 {
		t.Errorf("after cooking again = %+v", got)
	}
}

// Taking back the meal that pushed an item low puts it back in stock.
func TestUndoCookedClearsALowItPushed(t *testing.T) {
	f := newUsageFixture(t)
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{
		Name: "Butter", Source: PurchaseGroceryList, Quantity: "1", Unit: "cup", ClientPurchaseID: "p1",
	})
	if err != nil {
		t.Fatal(err)
	}
	butter := res.Item.ID
	f.advance(time.Hour)
	for _, e := range []string{"e1", "e2", "e3"} {
		f.cook(t, e, 4) // 3/4 cup
	}
	f.cook(t, "e4", 2) // 13/16: low
	if got := f.item(t, butter); got.Status != StatusLow {
		t.Fatalf("status = %s, want low", got.Status)
	}
	if _, err := f.svc.UndoCooked(f.ctx, testHousehold, "e4"); err != nil {
		t.Fatal(err)
	}
	got := f.item(t, butter)
	if got.Status != StatusInStock || got.LowAlertCycleID != "" || got.Tracking.RecipeUsed != "3/4" {
		t.Errorf("after undo = %+v tracking %+v", got, got.Tracking)
	}
}

// A use from a cycle that has since been restocked isn't given back: the new
// purchase already counts from a fresh start.
func TestUndoCookedLeavesARestockedItemAlone(t *testing.T) {
	f := newUsageFixture(t)
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{
		Name: "Butter", Source: PurchaseGroceryList, Quantity: "1", Unit: "cup", ClientPurchaseID: "p1",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.advance(time.Hour)
	f.cook(t, "entry1", 4)
	f.advance(time.Hour)
	if _, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{ItemID: res.Item.ID, Source: PurchaseManual, Quantity: "1", Unit: "cup"}); err != nil {
		t.Fatal(err)
	}
	before := f.item(t, res.Item.ID).Tracking
	if _, err := f.svc.UndoCooked(f.ctx, testHousehold, "entry1"); err != nil {
		t.Fatal(err)
	}
	if got := f.item(t, res.Item.ID).Tracking; got.RecipeUsed != before.RecipeUsed || got.RecipeUses != before.RecipeUses {
		t.Errorf("restocked item changed: %+v, was %+v", got, before)
	}
}

// The listener deducts recipe.cooked and gives a recipe.skipped entry back.
func TestCookedListenerUndoesOnSkip(t *testing.T) {
	f := newUsageFixture(t)
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{
		Name: "Butter", Source: PurchaseGroceryList, Quantity: "1", Unit: "cup", ClientPurchaseID: "p1",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.advance(time.Hour)
	l := f.svc.CookedListener()
	l.EventsStored(context.Background(), []events.Event{{
		HouseholdID: testHousehold, UserID: testUser, Type: events.TypeRecipeCooked, RecipeID: noodlesRecipe,
		Payload: events.RecipeCooked{EntryID: "e1", Servings: 4, Implicit: true}, OccurredAt: *f.clock,
	}})
	if got := f.item(t, res.Item.ID).Tracking; got.RecipeUsed != "1/4" {
		t.Fatalf("after implicit cook = %+v", got)
	}
	l.EventsStored(context.Background(), []events.Event{{
		HouseholdID: testHousehold, UserID: testUser, Type: events.TypeRecipeSkipped, RecipeID: noodlesRecipe,
		Payload: events.RecipeSkipped{EntryID: "e1"}, OccurredAt: *f.clock,
	}})
	if got := f.item(t, res.Item.ID).Tracking; got.RecipeUsed != "0" || got.RecipeUses != 0 {
		t.Errorf("after skip = %+v", got)
	}
}
