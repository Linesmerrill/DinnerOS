package pantry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// fakeAdjuster doubles the recipe's first ingredient for one entry, standing
// in for package customize.
type fakeAdjuster struct {
	entryID string
	err     error
	calls   int
}

func (f *fakeAdjuster) AdjustCookedRecipe(_ context.Context, _, entryID string, _ time.Time, r recipes.Recipe) (recipes.Recipe, error) {
	f.calls++
	if f.err != nil {
		return recipes.Recipe{}, f.err
	}
	if entryID != f.entryID {
		return r, nil
	}
	out := r
	out.Ingredients = append([]recipes.RecipeIngredient(nil), r.Ingredients...)
	out.Ingredients[0].Amounts = []recipes.Amount{{Servings: 2, Quantity: "4", Unit: "tbsp"}, {Servings: 4, Quantity: "8", Unit: "tbsp"}}
	return out, nil
}

// TestCookDeductsCustomizedMeal checks that a cooked plan entry deducts what
// the adjuster says was cooked, not the recipe as written.
func TestCookDeductsCustomizedMeal(t *testing.T) {
	f := newUsageFixture(t)
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "1", Unit: "cup"})
	if err != nil {
		t.Fatal(err)
	}
	adjuster := &fakeAdjuster{entryID: "entry-customized"}
	f.svc.SetCookAdjuster(adjuster)

	// The recipe asks for 2 tbsp of butter; doubled it is 4 tbsp of 16 (1/4 cup).
	if _, applied := f.cook(t, "entry-customized", 2); !applied {
		t.Fatal("customized meal not deducted")
	}
	if got := f.item(t, res.Item.ID).Tracking; got.RecipeUsed != "1/4" {
		t.Errorf("customized deduction = %s, want 1/4 cup", got.RecipeUsed)
	}

	// Another entry is deducted as written.
	if _, applied := f.cook(t, "entry-plain", 2); !applied {
		t.Fatal("plain meal not deducted")
	}
	if got := f.item(t, res.Item.ID).Tracking; got.RecipeUsed != "3/8" {
		t.Errorf("after the plain meal = %s, want 3/8 cup", got.RecipeUsed)
	}
	if adjuster.calls != 2 {
		t.Errorf("adjuster calls = %d", adjuster.calls)
	}
}

// TestCookAdjusterFailureDeductsNothing: a failure to read the customization
// fails the deduction rather than deducting what may not have been cooked.
func TestCookAdjusterFailureDeductsNothing(t *testing.T) {
	f := newUsageFixture(t)
	res, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "1", Unit: "cup"})
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("plans unavailable")
	f.svc.SetCookAdjuster(&fakeAdjuster{entryID: "entry1", err: boom})
	_, applied, err := f.svc.ApplyCooked(f.ctx, CookedMeal{
		HouseholdID: testHousehold, UserID: testUser, RecipeID: noodlesRecipe, EntryID: "entry1", Servings: 2, OccurredAt: *f.clock,
	})
	if !errors.Is(err, boom) || applied {
		t.Fatalf("ApplyCooked() = %v, %v", applied, err)
	}
	if got := f.item(t, res.Item.ID).Tracking; got.RecipeUsed != "0" && got.RecipeUsed != "" {
		t.Errorf("deducted anyway: %s", got.RecipeUsed)
	}
	if n := len(f.usage.cookUsages()); n != 0 {
		t.Errorf("cook usage records = %d", n)
	}
}

// TestCookWithoutEntryIgnoresAdjuster: an unplanned cooked meal has no entry
// to customize.
func TestCookWithoutEntryIgnoresAdjuster(t *testing.T) {
	f := newUsageFixture(t)
	if _, err := f.svc.RecordPurchase(f.ctx, f.actor, PurchaseInput{Name: "Butter", Source: PurchaseManual, Quantity: "1", Unit: "cup"}); err != nil {
		t.Fatal(err)
	}
	adjuster := &fakeAdjuster{entryID: "entry1"}
	f.svc.SetCookAdjuster(adjuster)
	if _, _, err := f.svc.ApplyCooked(f.ctx, CookedMeal{
		HouseholdID: testHousehold, UserID: testUser, RecipeID: noodlesRecipe, ClientEventID: "c1", Servings: 2, OccurredAt: *f.clock,
	}); err != nil {
		t.Fatal(err)
	}
	if adjuster.calls != 0 {
		t.Errorf("adjuster called %d times for an unplanned meal", adjuster.calls)
	}
}
