package shopping

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// mealKitHousehold returns a household with a meal kit baseline.
type mealKitHousehold struct{ kit *households.MealKit }

func (f mealKitHousehold) GetHousehold(_ context.Context, id string) (households.Household, error) {
	return households.Household{ID: id, TimeZone: "UTC", MealKit: f.kit}, nil
}

// TestIntegrationLeftoversPricesAndWeekCost follows one week end to end: "Did
// you order these?" with prices → what goes in the pantry → cooking counts it
// down → low stock puts it back on the list, and the week's cost against a
// $130-for-5 meal kit along the way.
func TestIntegrationLeftoversPricesAndWeekCost(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	f.svc.households = mealKitHousehold{kit: &households.MealKit{WeeklyCents: 13000, Meals: 5}}
	if _, err := f.svc.UpdateSettings(ctx, f.actor, "walmart", "5435"); err != nil {
		t.Fatal(err)
	}
	// A pint of milk for the tacos' cup: dairy bought for the week, but half
	// of it is left over.
	f.save(t, "Milk", "https://www.walmart.com/ip/Test-Milk/100000004", &PackageSize{Quantity: "16", Unit: "floz"})
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	f.save(t, "Garlic", "100000003", &PackageSize{Quantity: "1", Unit: "count"})
	f.save(t, "Kidney Beans", "https://www.walmart.com/ip/Test-Beans/100000005", &PackageSize{Quantity: "1", Unit: "can"})
	// Tomato paste bought earlier by hand, priced: pantry stock this week's
	// cooking draws on.
	paste, err := f.pantry.RecordPurchase(ctx, f.actor, pantry.PurchaseInput{Name: "Tomato Paste", Source: pantry.PurchaseManual, Quantity: "6", Unit: "oz", PriceCents: cents(120)})
	if err != nil || paste.Purchase.PriceCents == nil {
		t.Fatalf("tomato paste = %+v, %v", paste, err)
	}

	h, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil || len(h.Lines) != 4 {
		t.Fatalf("handoff = %+v, %v", h, err)
	}
	milk, beef, garlic, beans := lineFor(t, h.Proposal, "Milk"), lineFor(t, h.Proposal, "Ground Beef"), lineFor(t, h.Proposal, "Garlic"), lineFor(t, h.Proposal, "Kidney Beans")
	if _, err := f.svc.Confirm(ctx, f.actor, h.ID, ConfirmInput{Lines: []ConfirmLine{{LineID: milk.ID, PriceCents: cents(-1)}}}); err == nil {
		t.Error("a negative price was accepted")
	}
	res, err := f.svc.Confirm(ctx, f.actor, h.ID, ConfirmInput{Lines: []ConfirmLine{
		{LineID: milk.ID, PriceCents: cents(298)}, {LineID: beef.ID, PriceCents: cents(1198)}, {LineID: garlic.ID}, {LineID: beans.ID},
	}})
	if err != nil || res.Handoff.Status() != HandoffDone {
		t.Fatalf("confirm = %+v, %v", res, err)
	}

	// What went in the pantry: the milk and beans, not the beef or garlic.
	lines := map[string]HandoffLine{}
	for _, l := range res.Handoff.Lines {
		lines[l.Name] = l
	}
	if l := lines["Milk"]; l.Pantry != PantryTracked || l.PurchaseID == "" || l.PriceCents == nil || *l.PriceCents != 298 {
		t.Errorf("milk line = %+v", l)
	}
	for _, name := range []string{"Ground Beef", "Garlic"} {
		if l := lines[name]; l.Status != LineConfirmed || l.Pantry != PantryNotTracked || l.PurchaseID != "" {
			t.Errorf("%s line = %+v", name, l)
		}
	}
	if l := lines["Kidney Beans"]; l.Pantry != PantryTracked || l.PurchaseID == "" {
		t.Errorf("beans line = %+v", l)
	}
	if len(res.Purchases) != 2 {
		t.Fatalf("purchases = %+v", res.Purchases)
	}
	var milkItem pantry.Item
	for _, p := range res.Purchases {
		if p.LineID == milk.ID {
			milkItem = p.Item
			if p.Purchase.PriceCents == nil || *p.Purchase.PriceCents != 298 {
				t.Errorf("milk purchase price = %v", p.Purchase.PriceCents)
			}
		}
	}
	if milkItem.Tracking == nil || milkItem.Tracking.Unit != "floz" || milkItem.Tracking.Reference != "16" {
		t.Fatalf("milk item = %+v", milkItem)
	}
	if pref, _ := f.svc.GetPreference(ctx, testHousehold, "walmart", f.keys["Milk"]); pref.PriceCents == nil || *pref.PriceCents != 298 {
		t.Errorf("milk saved price = %+v", pref.PriceCents)
	}

	// A price entered afterwards reaches the pantry purchase and the saved
	// product (per can).
	priced, err := f.svc.SetPrices(ctx, f.actor, h.ID, []LinePrice{{LineID: beans.ID, PriceCents: cents(178)}, {LineID: garlic.ID, PriceCents: cents(50)}})
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := priced.Line(beans.ID); *l.PriceCents != 178 {
		t.Errorf("beans priced = %+v", l)
	}
	if list, _ := f.pantry.PurchasesByIDs(ctx, testHousehold, []string{lines["Kidney Beans"].PurchaseID}); len(list) != 1 || *list[0].PriceCents != 178 {
		t.Errorf("beans purchase = %+v", list)
	}
	if pref, _ := f.svc.GetPreference(ctx, testHousehold, "walmart", f.keys["Kidney Beans"]); *pref.PriceCents != 89 {
		t.Errorf("beans saved price = %d", *pref.PriceCents)
	}
	if _, err := f.svc.SetPrices(ctx, viewer(), h.ID, []LinePrice{{LineID: beans.ID}}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer prices error = %v", err)
	}

	// This week's Shop tab leaves out what was bought: fresh items as
	// ordered, tracked ones as in the pantry.
	m, err := f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	got := reasons(m)
	if got[f.keys["Ground Beef"]] != ExcludedOrdered || got[f.keys["Garlic"]] != ExcludedOrdered ||
		got[f.keys["Milk"]] != ExcludedInPantry || got[f.keys["Kidney Beans"]] != ExcludedInPantry || len(m.Lines) != 0 {
		t.Errorf("match after ordering = %v, lines %+v", got, m.Lines)
	}
	// Next week nothing was ordered yet, so the beef isn't hidden there.
	if keys, _ := f.svc.orderedKeys(ctx, testHousehold, "2026-W39", providers.KeyWalmart); len(keys) != 0 {
		t.Errorf("next week's ordered keys = %v", keys)
	}

	// Cook the tacos (planned): a cup of milk and a tablespoon of paste.
	plan, err := f.plans.Get(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	var tacos, tacosRecipe string
	for _, e := range plan.Entries {
		if e.RecipeName == "Beef Tacos" {
			tacos, tacosRecipe = e.ID, e.RecipeID
		}
	}
	cooked := func(entryID, clientEventID string) {
		t.Helper()
		m := pantry.CookedMeal{HouseholdID: testHousehold, UserID: testUser, RecipeID: tacosRecipe,
			EntryID: entryID, ClientEventID: clientEventID, Servings: 2, OccurredAt: time.Now().Add(time.Minute)}
		if _, applied, err := f.pantry.ApplyCooked(ctx, m); err != nil || !applied {
			t.Fatalf("cook = %v, %v", applied, err)
		}
	}
	cooked(tacos, "")
	items, err := f.pantry.List(ctx, testHousehold, pantry.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		switch it.DisplayName {
		case "Milk":
			if it.Tracking.RecipeUsed != "8" || it.Status != pantry.StatusInStock {
				t.Errorf("milk after tacos = %+v", it.Tracking)
			}
		case "Tomato Paste":
			// A tablespoon against ounces of weight, by typical density.
			if it.Tracking.RecipeUsed == "0" {
				t.Errorf("tomato paste wasn't counted down: %+v", it.Tracking)
			}
		case "Ground Beef", "Garlic":
			t.Errorf("%s is in the pantry", it.DisplayName)
		}
	}

	// The week's cost: milk half used, beef and garlic used up, both cans of
	// beans used, and 10% of the tomato paste from earlier stock.
	c, err := f.svc.WeekCost(ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if c.ItemsBought != 4 || c.ItemsPriced != 4 || *c.SpentCents != 1724 || c.SpentSource != SpentItemPrices || c.EarlierStockUsedCents != 12 ||
		*c.UsedCents != 149+1198+50+178+12 || *c.StockedCents != 149 || c.Meals != 2 || *c.CostPerMealCents != 794 ||
		*c.SavedCents != 2*2600-1587 || c.Summary != "Based on prices for all 4 items. Includes $0.12 of pantry stock bought earlier." {
		t.Errorf("week cost = %+v", c)
	}
	if err := f.svc.SetWeekSpend(ctx, f.actor, testWeek, cents(1900)); err != nil {
		t.Fatal(err)
	}
	s, err := f.svc.Savings(ctx, testHousehold, 0)
	if err != nil || len(s.Weeks) != 1 || s.WeeksCounted != 1 || *s.TotalSavedCents != 5200-(1587+176) || s.Weeks[0].FeesAndUnpricedCents != 176 {
		t.Errorf("savings = %+v, %v", s, err)
	}
	if err := f.svc.SetWeekSpend(ctx, viewer(), testWeek, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer spend error = %v", err)
	}

	// Cooking another cup uses the rest: the estimate marks the milk low, and
	// low stock puts it back on the list.
	cooked("", "extra-tacos")
	if _, err := f.pantry.List(ctx, testHousehold, pantry.ListQuery{}); err != nil {
		t.Fatal(err)
	}
	m, err = f.svc.Match(ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	if l := lineFor(t, m, "Milk"); l.GroceryStatus != "toBuy" {
		t.Errorf("low milk = %+v", l)
	}
	// Restocking while some is left adds to it rather than replacing it.
	more, err := f.pantry.RecordPurchase(ctx, f.actor, pantry.PurchaseInput{Name: "Tomato Paste", Source: pantry.PurchaseManual, Quantity: "6", Unit: "oz"})
	if err != nil || more.Item.Tracking.Reference == "6" {
		t.Errorf("second tomato paste = %+v, %v", more.Item.Tracking, err)
	}
}
