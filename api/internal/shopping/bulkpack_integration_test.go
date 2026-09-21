package shopping

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// TestIntegrationBulkPackToFreezerToNextWeek follows the whole story: the
// store's only ground beef is an eight-pound pack, the week needs two and a
// quarter pounds of it, the rest is sealed, and the week after that is told
// to grab it from the freezer instead of buying another eight pounds.
func TestIntegrationBulkPackToFreezerToNextWeek(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	f.svc.frozen = f.pantry
	if _, err := f.svc.UpdateSettings(ctx, f.actor, "walmart", "5435"); err != nil {
		t.Fatal(err)
	}
	// 128 oz is the smallest pack the store sells; the week's two recipes
	// use 20 oz and 1 lb, so 36 oz in all.
	f.save(t, "Ground Beef", "https://www.walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "128", Unit: "oz"})
	h, _, err := f.svc.CreateHandoff(ctx, f.actor, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}
	beef := lineFor(t, h.Proposal, "Ground Beef")

	packs, err := f.svc.BulkPacks(ctx, testHousehold, h.ID)
	if err != nil {
		t.Fatalf("BulkPacks: %v", err)
	}
	if len(packs.Packs) != 1 {
		t.Fatalf("%d packs, want the beef: %+v", len(packs.Packs), packs.Packs)
	}
	pack := packs.Packs[0]
	switch {
	case pack.LineID != beef.ID:
		t.Errorf("lineId = %q, want the beef line %q", pack.LineID, beef.ID)
	case pack.Bought != "128" || pack.Needed != "36" || pack.Surplus != "92":
		t.Errorf("amounts = bought %s, needed %s, surplus %s; want 128 / 36 / 92", pack.Bought, pack.Needed, pack.Surplus)
	case pack.SurplusPercent != 71:
		t.Errorf("surplusPercent = %d, want 71", pack.SurplusPercent)
	case !pack.Freezable:
		t.Error("freezable = false, want true: ground beef seals and freezes")
	case pack.Frozen:
		t.Error("frozen = true before anything was sealed")
	}

	// The member declines a second beef meal and seals the rest in four
	// portions instead.
	res, err := f.pantry.Freeze(ctx, f.actor, pantry.FreezeInput{
		Name: "Ground Beef", Quantity: pack.Surplus, Unit: pack.Unit, Portions: 4,
		Source: &pantry.FreezeSource{Provider: "walmart", HandoffID: h.ID, LineID: pack.LineID},
	})
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	// 92 oz in four portions is 23 oz each: about seven hours in the fridge.
	if res.Thaw.Hours != 7 || !res.Thaw.Measured {
		t.Errorf("thaw = %d hours (measured %v), want 7", res.Thaw.Hours, res.Thaw.Measured)
	}

	// The pack now says so, so the app stops offering to seal it twice.
	packs, err = f.svc.BulkPacks(ctx, testHousehold, h.ID)
	if err != nil {
		t.Fatalf("BulkPacks after freezing: %v", err)
	}
	if len(packs.Packs) != 1 || !packs.Packs[0].Frozen {
		t.Fatalf("after freezing, packs = %+v; want the beef marked frozen", packs.Packs)
	}

	// Next week plans the same two recipes.
	const nextWeek = "2026-W39"
	page, err := f.recipes.List(ctx, testHousehold, recipes.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range page.Items {
		if _, _, err := f.plans.AddEntry(ctx, testHousehold, testUser, nextWeek,
			planning.NewEntry{RecipeID: r.ID, Servings: 2}); err != nil {
			t.Fatal(err)
		}
	}
	g, err := f.plans.GroceryList(ctx, testHousehold, nextWeek)
	if err != nil {
		t.Fatalf("GroceryList: %v", err)
	}
	var beefItem *grocery.Item
	for _, c := range g.Categories {
		for i, it := range c.Items {
			if it.Name == "Ground Beef" {
				beefItem = &c.Items[i]
			}
		}
	}
	if beefItem == nil {
		t.Fatal("the beef vanished from next week's list; it must stay visible so somebody grabs it")
	}
	if beefItem.Status != grocery.StatusFromFreezer {
		t.Errorf("status = %q, want %q", beefItem.Status, grocery.StatusFromFreezer)
	}

	// And the Walmart hand-off must not buy it again.
	proposal, err := f.svc.Match(ctx, testHousehold, nextWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	for _, l := range proposal.Lines {
		if l.Name == "Ground Beef" {
			t.Fatalf("next week's cart still buys ground beef: %+v", l)
		}
	}
	found := false
	for _, e := range proposal.Excluded {
		if e.Name == "Ground Beef" {
			found = true
			if e.Reason != ExcludedInFreezer {
				t.Errorf("exclusion reason = %q, want %q", e.Reason, ExcludedInFreezer)
			}
		}
	}
	if !found {
		t.Error("the beef is neither a line nor an exclusion; the member would never learn why")
	}
}
