package shopping

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func TestBulkPackOf(t *testing.T) {
	oz := func(q string) *PackageSize { return &PackageSize{Quantity: q, Unit: "oz"} }
	for _, tc := range []struct {
		name    string
		line    HandoffLine
		want    bool
		percent int
	}{
		{
			// The case the whole feature exists for: the smallest loin at
			// the store is four pounds and Thursday uses ten ounces.
			name: "a four-pound loin for ten ounces",
			line: boughtLine("l1", "Pork Loin", ingredients.CategoryMeatSeafood,
				providers.CoveragePerWeek, oz("64"), 1, Amount{"10", "oz"}),
			want: true, percent: 84,
		},
		{
			name: "half the pack left is the threshold",
			line: boughtLine("l2", "Ground Beef", ingredients.CategoryMeatSeafood,
				providers.CoveragePerWeek, oz("16"), 1, Amount{"8", "oz"}),
			want: true, percent: 50,
		},
		{
			name: "a pack mostly used is ordinary shopping",
			line: boughtLine("l3", "Ground Beef", ingredients.CategoryMeatSeafood,
				providers.CoveragePerWeek, oz("16"), 1, Amount{"12", "oz"}),
			want: false,
		},
		{
			// Sour cream's leftover is already recorded as pantry stock, so
			// next week's list knows about it; there is nothing to rescue.
			name: "a tracked leftover is already handled",
			line: boughtLine("l4", "Sour Cream", ingredients.CategoryDairyEggs,
				providers.CoveragePerWeek, oz("16"), 1, Amount{"2", "tbsp"}),
			want: false,
		},
		{
			name: "per_amount lines carry over by measure",
			line: boughtLine("l5", "Soy Sauce", ingredients.CategoryCondiments,
				providers.CoveragePerAmount, oz("16"), 1, Amount{"1", "tbsp"}),
			want: false,
		},
		{
			name: "without a package size nothing is measured",
			line: boughtLine("l6", "Pork Loin", ingredients.CategoryMeatSeafood,
				providers.CoveragePerWeek, nil, 1, Amount{"10", "oz"}),
			want: false,
		},
	} {
		pack, ok := bulkPackOf(tc.line)
		if ok != tc.want {
			t.Errorf("%s: bulkPackOf ok = %v, want %v", tc.name, ok, tc.want)
			continue
		}
		if ok && pack.SurplusPercent != tc.percent {
			t.Errorf("%s: surplus = %d%%, want %d%%", tc.name, pack.SurplusPercent, tc.percent)
		}
	}
}

func TestBulkPackReportsTheRealAmounts(t *testing.T) {
	line := boughtLine("l1", "Pork Loin", ingredients.CategoryMeatSeafood,
		providers.CoveragePerWeek, &PackageSize{Quantity: "64", Unit: "oz"}, 1, Amount{"10", "oz"})
	pack, ok := bulkPackOf(line)
	if !ok {
		t.Fatal("bulkPackOf = false, want a bulk pack")
	}
	switch {
	case pack.Bought != "64":
		t.Errorf("Bought = %q, want 64", pack.Bought)
	case pack.Needed != "10":
		t.Errorf("Needed = %q, want 10", pack.Needed)
	case pack.Surplus != "54":
		t.Errorf("Surplus = %q, want 54: the amount there is to freeze or cook again", pack.Surplus)
	case pack.Unit != "oz":
		t.Errorf("Unit = %q, want oz", pack.Unit)
	case !pack.Freezable:
		t.Error("Freezable = false, want true: meat is worth sealing")
	}
	if got := surplusText(pack); got != "This week uses 10 oz of 64 oz" {
		t.Errorf("surplusText = %q", got)
	}
}

// A skipped line was never bought, so it has no surplus to do anything with.
func TestBulkPackSkipsSkippedLines(t *testing.T) {
	line := boughtLine("l1", "Pork Loin", ingredients.CategoryMeatSeafood,
		providers.CoveragePerWeek, &PackageSize{Quantity: "64", Unit: "oz"}, 1, Amount{"10", "oz"})
	line.Status = LineSkipped
	if _, ok := bulkPackOf(line); ok {
		t.Error("bulkPackOf(skipped) = true, want false")
	}
}

// A big produce leftover is already pantry stock, so next week's list
// already knows about it and there is nothing for the assistant to rescue.
// That is what leaves meat and seafood as the case this feature is for.
func TestBulkPackLeavesTrackedProduceAlone(t *testing.T) {
	line := boughtLine("l1", "Carrots", ingredients.CategoryProduce,
		providers.CoveragePerWeek, &PackageSize{Quantity: "32", Unit: "oz"}, 1, Amount{"4", "oz"})
	if pantryTrackingFor(line, 1) != PantryTracked {
		t.Fatal("the carrots are not tracked; this test no longer says what it means to")
	}
	if _, ok := bulkPackOf(line); ok {
		t.Error("bulkPackOf(tracked produce) = true, want false")
	}
}

// Sealing advice is per category: a frozen carrot is not the carrot the
// recipe asked for, but a frozen pork loin is the pork loin.
func TestFreezableCategories(t *testing.T) {
	for _, c := range []string{ingredients.CategoryProduce, ingredients.CategoryDairyEggs, ingredients.CategoryPantry} {
		if freezableCategories[c] {
			t.Errorf("%q is freezable, want not", c)
		}
	}
	for _, c := range []string{ingredients.CategoryMeatSeafood, ingredients.CategoryBakery, ingredients.CategoryDeli} {
		if !freezableCategories[c] {
			t.Errorf("%q is not freezable, want freezable", c)
		}
	}
}

func TestSplitFrozenFrom(t *testing.T) {
	handoffID, lineID, ok := splitFrozenFrom("abc123:l4")
	if !ok || handoffID != "abc123" || lineID != "l4" {
		t.Errorf("splitFrozenFrom = %q, %q, %v", handoffID, lineID, ok)
	}
	if _, _, ok := splitFrozenFrom(""); ok {
		t.Error("splitFrozenFrom(\"\") = true, want false")
	}
	if _, _, ok := splitFrozenFrom("noseparator"); ok {
		t.Error("splitFrozenFrom without a separator = true, want false")
	}
}
