package pantry

import (
	"context"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// groceryCatalog is the synthetic catalog for the grocery scenario.
var groceryCatalog = []string{"Olive Oil", "Salt", "Butter", "Flour", "Lime"}

func TestGroceryPantryDrivesAggregate(t *testing.T) {
	svc, _, catalog := newTestService(t, groceryCatalog...)
	runGroceryPantryScenario(t, svc, catalog)
}

// runGroceryPantryScenario builds a pantry through the service, converts it
// with GroceryPantry, and checks every status grocery.Aggregate assigns. Lines
// are keyed the way the planner keys them: catalog ingredient ID, or
// UnresolvedKeyPrefix + normalized name for lines without one.
func runGroceryPantryScenario(t *testing.T, svc *Service, catalog *fakeCatalog) {
	t.Helper()
	ctx := context.Background()
	actor := member(testHousehold)

	staples, err := svc.AddDefaultStaples(ctx, actor)
	if err != nil {
		t.Fatalf("AddDefaultStaples() error = %v", err)
	}
	byName := map[string]Item{}
	for _, it := range staples.Added {
		byName[it.DisplayName] = it
	}
	if _, err := svc.SetStatuses(ctx, actor, []StatusUpdate{
		{ItemID: byName["Butter"].ID, Status: StatusLow},
		{ItemID: byName["Flour"].ID, Status: StatusLow},
		{ItemID: byName["Salt"].ID, Status: StatusOut},
	}); err != nil {
		t.Fatalf("SetStatuses() error = %v", err)
	}
	mustAdd(t, svc, testHousehold, AddInput{Name: "Za'atar"})
	// Added as free text before the catalog knew it; the catalog learns it later.
	if sesame := mustAdd(t, svc, testHousehold, AddInput{Name: "Sesame Oil", Quantity: "1/4", Unit: "cup"}); sesame.IngredientID != "" {
		t.Fatalf("sesame oil linked before the catalog had it: %+v", sesame)
	}
	sesameID := catalog.add("Sesame Oil").ID
	mustAdd(t, svc, otherHousehold, AddInput{Name: "Lime"})

	stock, err := svc.GroceryPantry(ctx, testHousehold)
	if err != nil {
		t.Fatalf("GroceryPantry() error = %v", err)
	}

	amount := func(num, den int64) *ingredients.Quantity {
		q := ingredients.NewQuantity(num, den)
		return &q
	}
	id := catalog.id
	lines := []grocery.Line{
		{IngredientKey: id("Olive Oil"), Name: "Olive Oil", Category: "pantry", Quantity: amount(2, 1), UnitCode: "tbsp", PantryStaple: true},
		{IngredientKey: id("Butter"), Name: "Butter", Category: "dairy-eggs", Quantity: amount(1, 1), UnitCode: "tbsp"},
		{IngredientKey: id("Flour"), Name: "Flour", Category: "pantry", Quantity: amount(1, 2), UnitCode: "cup", PantryStaple: true},
		{IngredientKey: id("Salt"), Name: "Salt", Category: "spices", PantryStaple: true},
		{IngredientKey: id("Lime"), Name: "Lime", Category: "produce", Quantity: amount(1, 1), UnitCode: "count"},
		{IngredientKey: sesameID, Name: "Sesame Oil", Category: "pantry", Quantity: amount(1, 1), UnitCode: "tbsp"},
		{IngredientKey: UnresolvedKeyPrefix + "za'atar", Name: "Za'atar", Category: "spices", Quantity: amount(1, 1), UnitCode: "tsp"},
		{IngredientKey: UnresolvedKeyPrefix + "black pepper", Name: "Black Pepper", Category: "spices", PantryStaple: true},
	}
	list, err := grocery.Aggregate([]grocery.RecipeSelection{
		{RecipeID: "r1", RecipeName: "Test Stir Fry", RecipeServings: 2, TargetServings: 2, Lines: lines},
	}, stock)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}

	want := map[string]grocery.Status{
		id("Olive Oil"):                      grocery.StatusInPantry,   // staple in stock
		id("Butter"):                         grocery.StatusToBuy,      // low, not flagged by the recipe
		id("Flour"):                          grocery.StatusPantryHint, // low, flagged as a staple by the recipe
		id("Salt"):                           grocery.StatusToBuy,      // out, even though the recipe flags it
		id("Lime"):                           grocery.StatusToBuy,      // only another household has it
		sesameID:                             grocery.StatusInPantry,   // resolved against the catalog at read time
		UnresolvedKeyPrefix + "za'atar":      grocery.StatusInPantry,   // free text, no catalog ID
		UnresolvedKeyPrefix + "black pepper": grocery.StatusInPantry,   // default staple not in the catalog
	}
	if len(list.Items) != len(want) {
		t.Fatalf("list has %d items, want %d: %+v", len(list.Items), len(want), list.Items)
	}
	for _, item := range list.Items {
		if item.Status != want[item.IngredientKey] {
			t.Errorf("%s (%s) status = %s, want %s", item.Name, item.IngredientKey, item.Status, want[item.IngredientKey])
		}
	}

	other, err := svc.GroceryPantry(ctx, otherHousehold)
	if err != nil || !other.Has(id("Lime")) || other.Has(id("Olive Oil")) || stock.Has(id("Lime")) {
		t.Errorf("households share pantry state: other=%+v, %v", other, err)
	}
	if _, err := svc.GroceryPantry(ctx, ""); err == nil {
		t.Error("GroceryPantry without a household succeeded")
	}
}
