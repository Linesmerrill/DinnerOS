package pantry

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestMemoryUsageStoreContract(t *testing.T) {
	runTrackingContract(t, newMemoryStore())
	runUsageStoreContract(t, newMemoryUsageStore())
}

func TestIntegrationMongoUsageStoreContract(t *testing.T) {
	store, _ := newTestMongoStore(t)
	runTrackingContract(t, store)
	runUsageStoreContract(t, store)
}

// runTrackingContract checks that every usage field of an item round-trips
// and that clearing them removes them.
func runTrackingContract(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	item := Item{
		HouseholdID: testHousehold, Key: "butter", DisplayName: "Butter", Category: "dairy-eggs",
		Quantity: "2", Unit: "package", Status: StatusInStock, StatusSource: StatusSourceEstimate, StatusSetAt: day(1),
		UnitSize:            &UnitSize{Unit: "package", Quantity: "8", SizeUnit: "oz"},
		LowThresholdPercent: 75, LowAlertCycleID: "cycle0",
		History: []Segment{
			{StartedAt: day(-10), EndedAt: day(-2), Unit: "oz", Start: "16", RecipeUsed: "4", Remaining: "0"},
			{StartedAt: day(-2), EndedAt: day(0), Unit: "oz", Start: "8", RecipeUsed: "1/2", Remaining: "3", Observed: true},
		},
		Rate:      &Rate{PerDay: "3/2", Unit: "oz", Segments: 2, ComputedAt: testNow},
		CreatedAt: testNow, UpdatedBy: testUser, UpdatedAt: testNow,
	}
	startCycle(&item, "66e5a1f2c3b4a5d6e7f80f01", CycleGroceryList, ratOf("16"), "oz", testNow)
	item.Tracking.SegmentRecipeUsed, item.Tracking.RecipeUsed, item.Tracking.RecipeUses, item.Tracking.SkippedUses = "5/2", "5/2", 2, 1

	saved, err := store.InsertItem(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetItem(ctx, testHousehold, saved.ID)
	want := item
	want.ID, want.Version = saved.ID, 1
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetItem() = %+v, %v\nwant %+v", got, err, want)
	}

	cleared := got
	cleared.Tracking, cleared.UnitSize, cleared.History, cleared.Rate = nil, nil, nil, nil
	cleared.LowThresholdPercent, cleared.LowAlertCycleID = 0, ""
	updated, err := store.UpdateItem(ctx, cleared)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetItem(ctx, testHousehold, saved.ID)
	cleared.Version = updated.Version
	if !reflect.DeepEqual(got, cleared) {
		t.Errorf("after clearing = %+v\nwant %+v", got, cleared)
	}

	// A bulk status change is a person's, and keeps tracking.
	withTracking := got
	startCycle(&withTracking, "c2", CycleManual, ratOf("1"), "cup", testNow)
	if _, err := store.UpdateItem(ctx, withTracking); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(ctx, testHousehold, []string{saved.ID}, StatusOut, testUser, day(2)); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetItem(ctx, testHousehold, saved.ID)
	if got.StatusSource != StatusSourcePerson || !got.StatusSetAt.Equal(day(2)) || got.Tracking == nil || got.Tracking.CycleID != "c2" || got.Quantity != "" {
		t.Errorf("after SetStatus = %+v", got)
	}
}

func runUsageStoreContract(t *testing.T, store UsageStore) {
	t.Helper()
	ctx := context.Background()
	const itemID = "66e5a1f2c3b4a5d6e7f80d01"
	first := Purchase{
		ID: "66e5a1f2c3b4a5d6e7f80f01", HouseholdID: testHousehold, ItemID: itemID, ItemKey: "butter", Source: PurchaseGroceryList,
		Quantity: "2", Unit: "package", UnitSize: &UnitSize{Unit: "package", Quantity: "8", SizeUnit: "oz"}, Week: "2026-W38",
		ClientPurchaseID: "c1", RecordedBy: testUser, PurchasedAt: testNow,
	}
	saved, err := store.InsertPurchase(ctx, first)
	if err != nil || !reflect.DeepEqual(saved, first) {
		t.Fatalf("InsertPurchase() = %+v, %v", saved, err)
	}
	dup := first
	dup.ID = "66e5a1f2c3b4a5d6e7f80f02"
	if _, err := store.InsertPurchase(ctx, dup); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate client purchase error = %v", err)
	}
	second := Purchase{ID: "66e5a1f2c3b4a5d6e7f80f03", HouseholdID: testHousehold, ItemID: itemID, ItemKey: "butter", Source: PurchaseManual, RecordedBy: testUser, PurchasedAt: day(3)}
	third := second
	third.ID = "66e5a1f2c3b4a5d6e7f80f04" // no client ID: never a duplicate
	for _, p := range []Purchase{second, third} {
		if _, err := store.InsertPurchase(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := store.FindPurchaseByClientID(ctx, testHousehold, testUser, "c1"); err != nil || got.ID != first.ID {
		t.Errorf("FindPurchaseByClientID() = %+v, %v", got, err)
	}
	if _, err := store.FindPurchaseByClientID(ctx, otherHousehold, testUser, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindPurchaseByClientID(other household) error = %v", err)
	}
	list, err := store.ListPurchases(ctx, testHousehold, itemID, 2)
	if err != nil || len(list) != 2 || !list[0].PurchasedAt.Equal(day(3)) || !list[1].PurchasedAt.Equal(day(3)) {
		t.Errorf("ListPurchases(limit 2) = %+v, %v", list, err)
	}
	if list, _ := store.ListPurchases(ctx, testHousehold, itemID, 10); len(list) != 3 || list[2].ID != first.ID {
		t.Errorf("ListPurchases() = %+v", list)
	}

	fromHandoff := Purchase{
		ID: "66e5a1f2c3b4a5d6e7f80f05", HouseholdID: testHousehold, ItemID: itemID, ItemKey: "butter", Source: PurchaseProvider,
		Quantity: "2", Unit: "package", UnitSize: &UnitSize{Unit: "package", Quantity: "16", SizeUnit: "oz"}, Week: "2026-W38",
		Provider:   &ProviderRef{Key: "walmart", HandoffID: "66e5a1f2c3b4a5d6e7f80b01", LineID: "l1", ProductID: "123456789"},
		RecordedBy: testUser, PurchasedAt: day(4),
	}
	if saved, err := store.InsertPurchase(ctx, fromHandoff); err != nil || !reflect.DeepEqual(saved, fromHandoff) {
		t.Fatalf("InsertPurchase(provider) = %+v, %v", saved, err)
	}
	again := fromHandoff
	again.ID, again.RecordedBy = "66e5a1f2c3b4a5d6e7f80f06", "66e5a1f2c3b4a5d6e7f80c09"
	if _, err := store.InsertPurchase(ctx, again); !errors.Is(err, ErrDuplicate) {
		t.Errorf("second purchase for a handoff line error = %v", err)
	}
	otherLine := again
	otherLine.Provider = &ProviderRef{Key: "walmart", HandoffID: "66e5a1f2c3b4a5d6e7f80b01", LineID: "l2", ProductID: "123456789"}
	if _, err := store.InsertPurchase(ctx, otherLine); err != nil {
		t.Errorf("another line error = %v", err)
	}
	if got, err := store.FindPurchaseByProviderLine(ctx, testHousehold, "66e5a1f2c3b4a5d6e7f80b01", "l1"); err != nil || !reflect.DeepEqual(got, fromHandoff) {
		t.Errorf("FindPurchaseByProviderLine() = %+v, %v", got, err)
	}
	if _, err := store.FindPurchaseByProviderLine(ctx, otherHousehold, "66e5a1f2c3b4a5d6e7f80b01", "l1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindPurchaseByProviderLine(other household) error = %v", err)
	}

	usage := CookUsage{
		HouseholdID: testHousehold, SourceKey: "entry:e1", RecipeID: "66e5a1f2c3b4a5d6e7f80e01", EntryID: "e1", UserID: testUser,
		Servings: 3, ScaledFrom: 2, OccurredAt: testNow, CreatedAt: testNow,
		Lines: []CookLine{
			{ItemID: itemID, Ingredient: "Butter", Quantity: "3", Unit: "tbsp", Deducted: "3/2", TrackingUnit: "oz", CycleID: "c"},
			{ItemID: itemID, Ingredient: "Salt", SkipReason: SkipNoAmount},
		},
	}
	savedUsage, err := store.InsertCookUsage(ctx, usage)
	if err != nil || savedUsage.ID == "" {
		t.Fatalf("InsertCookUsage() = %+v, %v", savedUsage, err)
	}
	if _, err := store.InsertCookUsage(ctx, usage); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate cook usage error = %v", err)
	}
	usage.HouseholdID = otherHousehold
	if _, err := store.InsertCookUsage(ctx, usage); err != nil {
		t.Errorf("same key in another household error = %v", err)
	}

	if _, err := store.GetSettings(ctx, testHousehold); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSettings() before put error = %v", err)
	}
	for _, pct := range []int{60, 90} {
		want := Settings{HouseholdID: testHousehold, LowThresholdPercent: pct, UpdatedBy: testUser, UpdatedAt: testNow}
		if got, err := store.PutSettings(ctx, want); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("PutSettings(%d) = %+v, %v", pct, got, err)
		}
		if got, err := store.GetSettings(ctx, testHousehold); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("GetSettings() = %+v, %v", got, err)
		}
	}
}
