package shopping

import (
	"math/big"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func cents(v int64) *int64 { return &v }

// boughtLine is a confirmed handoff line for cost and leftover tests.
func boughtLine(id, name, category string, coverage providers.Coverage, size *PackageSize, packages int, amounts ...Amount) HandoffLine {
	return HandoffLine{
		ID: id, LineSource: LineSource{IngredientKey: "name:" + strings.ToLower(name), Name: name, Category: category, Amounts: amounts},
		PackageSize: size, Coverage: coverage, Packages: packages, Status: LineConfirmed, ConfirmedPackages: packages,
	}
}

func TestPantryTrackingFor(t *testing.T) {
	oz := func(q string) *PackageSize { return &PackageSize{Quantity: q, Unit: "oz"} }
	for _, tc := range []struct {
		name string
		line HandoffLine
		want PantryTracking
	}{
		{"sour cream outlasts the week", boughtLine("l", "Sour Cream", ingredients.CategoryDairyEggs, providers.CoveragePerWeek, oz("16"), 1, Amount{"2", "tbsp"}), PantryTracked},
		{"a dozen eggs for two", boughtLine("l", "Eggs", ingredients.CategoryDairyEggs, providers.CoveragePerWeek, &PackageSize{Quantity: "12", Unit: "count"}, 1, Amount{"2", "count"}), PantryTracked},
		{"shredded cheese mostly used", boughtLine("l", "Shredded Cheese", ingredients.CategoryDairyEggs, providers.CoveragePerWeek, oz("8"), 1, Amount{"7", "oz"}), PantryNotTracked},
		{"milk without a size", boughtLine("l", "Milk", ingredients.CategoryDairyEggs, providers.CoveragePerWeek, nil, 1, Amount{"1", "cup"}), PantryNotTracked},
		{"garlic cloves against a bulb", boughtLine("l", "Garlic", ingredients.CategoryProduce, providers.CoveragePerWeek, &PackageSize{Quantity: "1", Unit: "count"}, 1, Amount{"4", "clove"}), PantryNotTracked},
		{"meat never, however much is left", boughtLine("l", "Ground Beef", ingredients.CategoryMeatSeafood, providers.CoveragePerWeek, oz("32"), 1, Amount{"8", "oz"}), PantryNotTracked},
		{"exactly the minimum leftover", boughtLine("l", "Carrots", ingredients.CategoryProduce, providers.CoveragePerWeek, oz("16"), 1, Amount{"12", "oz"}), PantryTracked},
		{"soy sauce is counted by measure", boughtLine("l", "Soy Sauce", ingredients.CategoryCondiments, providers.CoveragePerAmount, nil, 1, Amount{"1", "tbsp"}), PantryTracked},
		{"a member's per_amount beef", boughtLine("l", "Ground Beef", ingredients.CategoryMeatSeafood, providers.CoveragePerAmount, oz("16"), 2, Amount{"20", "oz"}), PantryTracked},
	} {
		if got := pantryTrackingFor(tc.line, tc.line.ConfirmedPackages); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestComputeWeekCost(t *testing.T) {
	sourCream := boughtLine("l1", "Sour Cream", ingredients.CategoryDairyEggs, providers.CoveragePerWeek, &PackageSize{Quantity: "16", Unit: "oz"}, 1, Amount{"2", "tbsp"})
	sourCream.Pantry, sourCream.PurchaseID, sourCream.PriceCents = PantryTracked, "p1", cents(248)
	beef := boughtLine("l2", "Ground Beef", ingredients.CategoryMeatSeafood, providers.CoveragePerWeek, &PackageSize{Quantity: "16", Unit: "oz"}, 2, Amount{"20", "oz"})
	beef.Pantry, beef.PriceCents = PantryNotTracked, cents(1198)
	cumin := boughtLine("l3", "Ground Cumin", ingredients.CategorySpices, providers.CoveragePerAmount, &PackageSize{Quantity: "3/2", Unit: "oz"}, 1, Amount{"1", "tsp"})
	cumin.Pantry, cumin.PurchaseID = PantryTracked, "p3"
	garlic := boughtLine("l4", "Garlic", ingredients.CategoryProduce, providers.CoveragePerWeek, nil, 1, Amount{"4", "clove"})
	garlic.Pantry, garlic.PriceCents = PantryNotTracked, cents(50)
	// Soy sauce without a package size: tracked, but the use can't be measured.
	soy := boughtLine("l5", "Soy Sauce", ingredients.CategoryCondiments, providers.CoveragePerAmount, nil, 1, Amount{"1", "tbsp"})
	soy.Pantry, soy.PurchaseID = PantryTracked, "p5"
	pending := boughtLine("l6", "Rice", ingredients.CategoryPantry, providers.CoveragePerAmount, nil, 1)
	pending.Status = LinePending

	h := Handoff{ID: "h1", Proposal: Proposal{Week: "2026-W38", Lines: []HandoffLine{sourCream, beef, cumin, garlic, soy, pending}}}
	kit := &households.MealKit{WeeklyCents: 13000, Meals: 5}
	in := costInput{
		week: "2026-W38", handoffs: []Handoff{h}, meals: 5, kit: kit,
		// The soy sauce's price came in on its pantry purchase.
		purchases: map[string]pantry.Purchase{"p5": {ID: "p5", PriceCents: cents(297)}, "p1": {ID: "p1"}},
		stock:     map[string]pantry.StockValue{},
	}

	c := computeWeekCost(in)
	if c.ItemsBought != 5 || c.ItemsPriced != 4 || !c.Partial() || c.UnmeasuredItems != 1 {
		t.Fatalf("counts = %+v", c)
	}
	items := map[string]CostItem{}
	for _, it := range c.Items {
		items[it.LineID] = it
	}
	// 2 tbsp of sour cream ≈ 1.1 oz of 16 oz: about 7% of $2.48.
	if it := items["l1"]; *it.UsedCents != 17 || *it.StockedCents != 231 || it.Usage != UsageMeasured || it.Pantry != PantryTracked {
		t.Errorf("sour cream = %+v used %d stocked %d", it, *it.UsedCents, *it.StockedCents)
	}
	if it := items["l2"]; *it.UsedCents != 1198 || *it.StockedCents != 0 || it.Usage != UsageWholePackage {
		t.Errorf("beef = %+v", it)
	}
	if it := items["l3"]; it.PriceCents != nil || it.UsedCents != nil || it.Usage != UsageMeasured {
		t.Errorf("unpriced cumin = %+v", it)
	}
	if it := items["l5"]; *it.PriceCents != 297 || *it.UsedCents != 297 || it.Usage != UsageUnknown {
		t.Errorf("soy = %+v", it)
	}
	if *c.SpentCents != 1793 || c.SpentSource != SpentItemPrices || *c.UsedCents != 1562 || *c.StockedCents != 231 ||
		*c.CostPerMealCents != 312 || *c.SavedCents != 13000-1562 || c.OrderTotalCents != nil {
		t.Errorf("totals = spent %d used %d stocked %d per meal %d saved %d", *c.SpentCents, *c.UsedCents, *c.StockedCents, *c.CostPerMealCents, *c.SavedCents)
	}
	if want := "Based on 4 of 5 items with prices. 1 item couldn't be measured and count as used."; c.Summary != want {
		t.Errorf("summary = %q", c.Summary)
	}

	// The order total covers fees and the unpriced cumin; they count as used.
	// Pantry stock bought earlier and cooked this week counts too.
	in.spend = &WeekSpend{Week: "2026-W38", OrderTotalCents: 2000}
	in.earlier = 45
	c = computeWeekCost(in)
	if *c.SpentCents != 2000 || c.SpentSource != SpentOrderTotal || *c.OrderTotalCents != 2000 || c.FeesAndUnpricedCents != 207 ||
		*c.UsedCents != 1562+207+45 || *c.SavedCents != 13000-1814 {
		t.Errorf("with order total = %+v", c)
	}
	if !strings.Contains(c.Summary, "Fees, tax, and unpriced items ($2.07) count as used.") || !strings.Contains(c.Summary, "Includes $0.45 of pantry stock bought earlier.") {
		t.Errorf("summary = %q", c.Summary)
	}

	// The pantry says half the tub is gone already: stocked follows it.
	in.spend, in.earlier = nil, 0
	in.stock["p1"] = pantry.StockValue{Tracked: true, Bought: big.NewRat(16, 1), Remaining: big.NewRat(8, 1), Unit: "oz"}
	c = computeWeekCost(in)
	for _, it := range c.Items {
		if it.LineID == "l1" && (*it.StockedCents != 124 || *it.UsedCents != 124) {
			t.Errorf("sour cream with the pantry's estimate = used %d stocked %d", *it.UsedCents, *it.StockedCents)
		}
	}

	// Without a meal kit or meals there's no comparison, and nothing priced
	// means nothing is known.
	in.kit, in.meals = nil, 0
	if c = computeWeekCost(in); c.SavedCents != nil || c.CostPerMealCents != nil || c.UsedCents == nil {
		t.Errorf("no kit = %+v", c)
	}
	empty := computeWeekCost(costInput{week: "2026-W39"})
	if empty.SpentCents != nil || empty.UsedCents != nil || empty.Summary != "" || len(empty.Items) != 0 {
		t.Errorf("empty week = %+v", empty)
	}
	onlyTotal := computeWeekCost(costInput{week: "2026-W39", spend: &WeekSpend{OrderTotalCents: 13042}, meals: 4, kit: kit})
	if *onlyTotal.UsedCents != 13042 || *onlyTotal.StockedCents != 0 || *onlyTotal.SavedCents != 4*2600-13042 ||
		onlyTotal.Summary != "Based on the order total; add item prices to see what's stocked for later." {
		t.Errorf("order total only = %+v", onlyTotal)
	}
}

func TestRoundCentsAndDollars(t *testing.T) {
	if roundCents(big.NewRat(1699, 100)) != 17 || roundCents(big.NewRat(1649, 100)) != 16 || roundCents(big.NewRat(0, 1)) != 0 {
		t.Error("roundCents")
	}
	if dollars(207) != "$2.07" || dollars(13000) != "$130.00" {
		t.Error("dollars")
	}
}
