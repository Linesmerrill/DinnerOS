package pantry

import (
	"context"
	"slices"
	"testing"
)

// An item the household says also counts as another ingredient answers for
// it: "Thyme" kept dried covers a recipe's "Dried Thyme", so the list doesn't
// buy dried thyme the household has (decision 641).
func TestAlsoCountsAsCoversTheOtherIngredient(t *testing.T) {
	ctx := context.Background()
	svc, _, catalog := newTestService(t, "Thyme", "Dried Thyme", "Rosemary")
	actor := member(testHousehold)
	thyme := mustAdd(t, svc, testHousehold, AddInput{Name: "Thyme", Quantity: "4", Unit: "oz"})
	mustAdd(t, svc, testHousehold, AddInput{Name: "Rosemary", Quantity: "1", Unit: "oz"})

	stock, err := svc.GroceryPantry(ctx, testHousehold)
	if err != nil {
		t.Fatal(err)
	}
	if stock.Has(catalog.id("Dried Thyme")) {
		t.Fatal("dried thyme matched before it was linked")
	}

	names := []string{"Dried Thyme", " dried  thyme ", "Thyme", ""}
	updated, err := svc.Update(ctx, actor, thyme.ID, UpdateInput{AlsoCountsAs: &names})
	if err != nil {
		t.Fatal(err)
	}
	// Duplicates, blanks, and the item's own name are dropped.
	if !slices.Equal(updated.AlsoCountsAs, []string{"Dried Thyme"}) {
		t.Fatalf("alsoCountsAs = %q", updated.AlsoCountsAs)
	}

	stock, err = svc.GroceryPantry(ctx, testHousehold)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{catalog.id("Dried Thyme"), UnresolvedKeyPrefix + "dried thyme", catalog.id("Thyme")} {
		if !stock.Has(key) {
			t.Errorf("stock doesn't have %s", key)
		}
	}
	// What's at home for dried thyme is the thyme item's amount.
	if a, ok := stock.Amounts[catalog.id("Dried Thyme")]; !ok || a.Unit.Code != "oz" || a.Quantity.Rat().RatString() != "4" {
		t.Errorf("dried thyme on hand = %+v, %v", a, ok)
	}

	// Clearing the links stops the match.
	none := []string{}
	if updated, err = svc.Update(ctx, actor, thyme.ID, UpdateInput{AlsoCountsAs: &none}); err != nil || len(updated.AlsoCountsAs) != 0 {
		t.Fatalf("clear = %+v, %v", updated.AlsoCountsAs, err)
	}
	if stock, _ = svc.GroceryPantry(ctx, testHousehold); stock.Has(catalog.id("Dried Thyme")) {
		t.Error("dried thyme still matches after the link was cleared")
	}

	tooMany := make([]string, MaxAlsoCountsAs+1)
	for i := range tooMany {
		tooMany[i] = "Herb " + string(rune('A'+i))
	}
	if _, err := svc.Update(ctx, actor, thyme.ID, UpdateInput{AlsoCountsAs: &tooMany}); err == nil {
		t.Error("more than MaxAlsoCountsAs names were accepted")
	}
}

// A cooked meal's ingredient comes off the item that also counts as it.
func TestAlsoCountsAsIsDeductedWhenCooked(t *testing.T) {
	f := newUsageFixture(t)
	cheese, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Parmigiano", Source: PurchaseManual, Quantity: "2", Unit: "cup"})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"Parmesan"}
	if _, err := f.svc.Update(f.ctx, f.actor, cheese.Item.ID, UpdateInput{AlsoCountsAs: &names}); err != nil {
		t.Fatal(err)
	}
	if _, applied := f.cook(t, "entry-1", 2); !applied {
		t.Fatal("meal not deducted")
	}
	if got := f.item(t, cheese.Item.ID).Tracking; got == nil || got.RecipeUsed != "1/2" {
		t.Errorf("parmigiano deduction = %+v, want 1/2 cup for the recipe's parmesan", got)
	}
}

// The same ingredient under another name is the same item: a pantry "Yellow
// Onion" answers for a recipe's "Onion" (decision 640).
func TestSameIngredientNamesMatchThePantry(t *testing.T) {
	ctx := context.Background()
	svc, _, catalog := newTestService(t, "Onion")
	onion := mustAdd(t, svc, testHousehold, AddInput{Name: "Yellow Onion", Quantity: "3", Unit: "count"})
	if onion.IngredientID != catalog.id("Onion") {
		t.Errorf("yellow onion linked to %q, want the onion %q", onion.IngredientID, catalog.id("Onion"))
	}
	stock, err := svc.GroceryPantry(ctx, testHousehold)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{catalog.id("Onion"), UnresolvedKeyPrefix + "onion", UnresolvedKeyPrefix + "yellow onion"} {
		if !stock.Has(key) {
			t.Errorf("stock doesn't have %s", key)
		}
	}
}
