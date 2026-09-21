package pantry

import (
	"context"
	"math/big"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func TestFreezeRecordsTheSealedAmount(t *testing.T) {
	svc, _, _ := newTestService(t)
	res, err := svc.Freeze(context.Background(), member(testHousehold), FreezeInput{
		Name: "Pork Loin", Quantity: "54", Unit: "oz", Portions: 3,
		Source: &FreezeSource{Provider: "walmart", HandoffID: "h1", LineID: "l1"},
	})
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if !res.Created {
		t.Error("Created = false, want true: the pantry had no pork loin")
	}
	item := res.Item
	switch {
	case item.Storage != StorageFreezer:
		t.Errorf("Storage = %q, want %q", item.Storage, StorageFreezer)
	case item.Status != StatusInStock:
		t.Errorf("Status = %q, want %q: sealing it means you have it", item.Status, StatusInStock)
	case item.Quantity != "54" || item.Unit != "oz":
		t.Errorf("amount = %q %q, want the amount actually sealed, 54 oz", item.Quantity, item.Unit)
	case item.Portions != 3:
		t.Errorf("Portions = %d, want 3", item.Portions)
	case item.FrozenOn != testNow.Format(DateLayout):
		t.Errorf("FrozenOn = %q, want %q", item.FrozenOn, testNow.Format(DateLayout))
	case item.FrozenFrom != "h1:l1":
		t.Errorf("FrozenFrom = %q, want %q", item.FrozenFrom, "h1:l1")
	}
	// The sealed amount starts a usage cycle, so cooked meals count it down
	// like any other pantry amount.
	if item.Tracking == nil {
		t.Fatal("Tracking is nil, want a cycle started from the sealed amount")
	}
	if item.Tracking.CycleSource != CycleFrozen || item.Tracking.Reference != "54" {
		t.Errorf("cycle = %q %q, want %q 54", item.Tracking.CycleSource, item.Tracking.Reference, CycleFrozen)
	}
}

func TestFreezeIsIdempotentPerHandoffLine(t *testing.T) {
	svc, _, _ := newTestService(t)
	src := &FreezeSource{Provider: "walmart", HandoffID: "h1", LineID: "l1"}
	in := FreezeInput{Name: "Pork Loin", Quantity: "54", Unit: "oz", Source: src}
	first, err := svc.Freeze(context.Background(), member(testHousehold), in)
	if err != nil {
		t.Fatalf("first Freeze: %v", err)
	}
	second, err := svc.Freeze(context.Background(), member(testHousehold), in)
	if err != nil {
		t.Fatalf("second Freeze: %v", err)
	}
	if !second.AlreadyFrozen {
		t.Error("AlreadyFrozen = false, want true: the same line was sealed twice")
	}
	if second.Item.Version != first.Item.Version {
		t.Errorf("version %d → %d, want no write on the second call", first.Item.Version, second.Item.Version)
	}
	items, err := svc.store.ListItems(context.Background(), testHousehold, ListFilter{})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("%d items, want 1: freezing twice must not double the freezer", len(items))
	}
}

func TestFreezeNeedsAnAmount(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, err := svc.Freeze(context.Background(), member(testHousehold), FreezeInput{Name: "Pork Loin"})
	wantValidation(t, err, "sealed")
}

func TestFrozenStockIsOnlyTheFreezer(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	if _, _, err := svc.Add(ctx, member(testHousehold), AddInput{Name: "Olive Oil", Quantity: "16", Unit: "oz"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := svc.Freeze(ctx, member(testHousehold), FreezeInput{Name: "Pork Loin", Quantity: "54", Unit: "oz"}); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	stock, err := svc.FrozenStock(ctx, testHousehold)
	if err != nil {
		t.Fatalf("FrozenStock: %v", err)
	}
	if len(stock) != 1 || stock[0].Item.DisplayName != "Pork Loin" {
		t.Fatalf("FrozenStock = %+v, want only the pork loin", stock)
	}
	if !slices.Contains(stock[0].Keys, UnresolvedKeyPrefix+"pork loin") {
		t.Errorf("Keys = %v, want the grocery key a recipe line would carry", stock[0].Keys)
	}
}

// A frozen item covers a grocery line without being bought again, and
// without vanishing from the list.
func TestGroceryPantryPutsFrozenItemsInTheirOwnSet(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	if _, err := svc.Freeze(ctx, member(testHousehold), FreezeInput{Name: "Pork Loin", Quantity: "54", Unit: "oz"}); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	stock, err := svc.GroceryPantry(ctx, testHousehold)
	if err != nil {
		t.Fatalf("GroceryPantry: %v", err)
	}
	key := UnresolvedKeyPrefix + "pork loin"
	if stock.InStock[key] {
		t.Error("the frozen loin is in InStock, which would drop its line from the list entirely")
	}
	if !stock.Frozen(key) {
		t.Errorf("Frozen(%q) = false, want true", key)
	}
	list, err := grocery.Aggregate([]grocery.RecipeSelection{{
		RecipeID: "r1", RecipeName: "Pork Chops", RecipeServings: 2, TargetServings: 2,
		Lines: []grocery.Line{{IngredientKey: key, Name: "Pork Loin", Category: ingredients.CategoryMeatSeafood}},
	}}, stock)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("%d items, want the line kept on the list", len(list.Items))
	}
	if got := list.Items[0].Status; got != grocery.StatusFromFreezer {
		t.Errorf("status = %q, want %q", got, grocery.StatusFromFreezer)
	}
}

// The thaw model: roughly five hours a pound in the fridge for dense
// protein, two for bread and deli, divided by the portions it was split
// into, clamped and rounded.
func TestThawHoursByWeightAndCategory(t *testing.T) {
	cases := []struct {
		name     string
		ounces   *big.Rat
		category string
		want     int
	}{
		{"a pound of pork", big.NewRat(16, 1), ingredients.CategoryMeatSeafood, 5},
		{"four pounds of pork", big.NewRat(64, 1), ingredients.CategoryMeatSeafood, 20},
		{"a small portion floors at the minimum", big.NewRat(2, 1), ingredients.CategoryMeatSeafood, MinThawHours},
		{"twenty pounds caps at the maximum", big.NewRat(320, 1), ingredients.CategoryMeatSeafood, MaxThawHours},
		{"a pound of bread is quicker", big.NewRat(16, 1), ingredients.CategoryBakery, LightHoursPerPound},
	}
	for _, c := range cases {
		if got := thawHours(c.ounces, c.category); got != c.want {
			t.Errorf("%s: thawHours = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestThawForWeighsOnePortionNotTheWholeBag(t *testing.T) {
	whole := ThawFor(Item{
		Category: ingredients.CategoryMeatSeafood, Quantity: "64", Unit: "oz",
		Storage: StorageFreezer,
	})
	if !whole.Measured || whole.Hours != 20 {
		t.Fatalf("whole bag = %d hours (measured %v), want 20", whole.Hours, whole.Measured)
	}
	split := ThawFor(Item{
		Category: ingredients.CategoryMeatSeafood, Quantity: "64", Unit: "oz",
		Storage: StorageFreezer, Portions: 4,
	})
	if split.Hours != 5 {
		t.Errorf("four portions = %d hours, want 5: you thaw a portion, not the bag", split.Hours)
	}
	if split.Portions != 4 {
		t.Errorf("Portions = %d, want 4", split.Portions)
	}
}

func TestThawForFallsBackWhenTheAmountIsNotAWeight(t *testing.T) {
	e := ThawFor(Item{Category: ingredients.CategoryMeatSeafood, Quantity: "2", Unit: "count", Storage: StorageFreezer})
	if e.Measured {
		t.Error("Measured = true, want false: a count of two isn't a weight")
	}
	if e.Hours != DefaultThawHours {
		t.Errorf("Hours = %d, want the default %d", e.Hours, DefaultThawHours)
	}
	if e.Summary == "" {
		t.Error("Summary is empty; it is shown to people even when unmeasured")
	}
}
