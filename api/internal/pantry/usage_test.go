package pantry

import (
	"math/big"
	"testing"
	"time"
)

func day(n float64) time.Time { return testNow.Add(time.Duration(n * float64(24*time.Hour))) }

func wantRat(t *testing.T, name string, got *big.Rat, want string) {
	t.Helper()
	if got == nil || got.Cmp(ratOf(want)) != 0 {
		t.Errorf("%s = %v, want %s", name, got, want)
	}
}

func TestConvertAmount(t *testing.T) {
	stick := &UnitSize{Unit: "package", Quantity: "8", SizeUnit: "oz"}
	for _, tc := range []struct {
		q, from, to string
		size        *UnitSize
		want        string // "" means no conversion
	}{
		{"1", "tbsp", "tbsp", nil, "1"},
		{"1", "tbsp", "cup", nil, "1/16"},
		{"3", "tsp", "tbsp", nil, "1"},
		{"1", "lb", "oz", nil, "16"},
		{"1", "kg", "g", nil, "1000"},
		{"1", "cup", "oz", nil, ""},      // volume never becomes weight
		{"2", "count", "g", nil, ""},     // a count needs a size
		{"2", "clove", "count", nil, ""}, // different discrete units
		{"2", "package", "lb", stick, "1"},
		{"4", "oz", "package", stick, "1/2"},
		{"1", "can", "oz", stick, ""}, // the size is for packages only
		{"1", "bogus", "g", nil, ""},
	} {
		got, ok := convertAmount(ratOf(tc.q), tc.from, tc.to, tc.size)
		switch {
		case tc.want == "" && ok:
			t.Errorf("convert %s %s → %s = %v, want no conversion", tc.q, tc.from, tc.to, got)
		case tc.want != "" && (!ok || got.Cmp(ratOf(tc.want)) != 0):
			t.Errorf("convert %s %s → %s = %v, %v, want %s", tc.q, tc.from, tc.to, got, ok, tc.want)
		}
	}
}

func TestLearnRate(t *testing.T) {
	seg := func(startDay, endDay float64, unit, start, used, left string) Segment {
		return Segment{StartedAt: day(startDay), EndedAt: day(endDay), Unit: unit, Start: start, RecipeUsed: used, Remaining: left}
	}
	history := []Segment{
		seg(0, 6, "tbsp", "16", "4", "0"),   // 12 tbsp over 6 days: 2/day
		seg(6, 6.5, "tbsp", "16", "0", "0"), // under a day: ignored
	}
	if r := learnRate(history, "tbsp", nil, testNow); r != nil {
		t.Errorf("rate from one usable segment = %+v, want nil", r)
	}
	history = append(history,
		seg(7, 10, "tbsp", "16", "6", "4"),  // 6 over 3 days: 2/day
		seg(10, 18, "cup", "1", "0", "0"),   // 16 tbsp over 8 days: 2/day
		seg(18, 22, "tbsp", "16", "0", "0"), // 4/day
		seg(22, 24, "oz", "8", "0", "0"),    // doesn't convert: ignored
	)
	r := learnRate(history, "tbsp", nil, testNow)
	if r == nil || r.PerDay != "2" || r.Unit != "tbsp" || r.Segments != 4 || !r.ComputedAt.Equal(testNow) {
		t.Fatalf("rate = %+v, want 2 tbsp/day from 4 segments", r)
	}
	// An even count takes the mean of the middle two; recipe use beyond the
	// start never makes the rate negative.
	r = learnRate([]Segment{seg(0, 2, "tbsp", "4", "10", "0"), seg(2, 4, "tbsp", "6", "0", "0")}, "tbsp", nil, testNow)
	if r == nil || r.PerDay != "3/2" {
		t.Errorf("rate = %+v, want (0 + 3) / 2", r)
	}
}

func trackedButter() Item {
	item := Item{ID: "item1", HouseholdID: testHousehold, Key: "butter", DisplayName: "Butter", Status: StatusInStock,
		StatusSource: StatusSourcePerson, StatusSetAt: testNow, Quantity: "16", Unit: "tbsp"}
	startCycle(&item, "cycle1", CycleGroceryList, ratOf("16"), "tbsp", testNow)
	return item
}

func TestEstimateExplainsRecipeAndOtherUse(t *testing.T) {
	item := trackedButter()
	item.Tracking.SegmentRecipeUsed, item.Tracking.RecipeUsed, item.Tracking.RecipeUses = "6", "6", 2
	item.Rate = &Rate{PerDay: "1", Unit: "tbsp", Segments: 3}

	e := estimateItem(item, 80, day(5))
	if e == nil {
		t.Fatal("estimate = nil")
	}
	wantRat(t, "remaining", e.Remaining, "5")
	wantRat(t, "otherUsed", e.OtherUsed, "5")
	wantRat(t, "dailyRate", e.DailyRate, "1")
	if e.PercentRemaining != 31 || e.PercentUsed != 69 || e.BelowThreshold || e.ThresholdPct != 80 || e.ThresholdItem || e.RateSegments != 3 {
		t.Errorf("estimate = %+v", e)
	}
	if want := "About 31% left: 2 recipes used 6 tbsp, plus about 1 tbsp a day of other use."; e.Summary != want {
		t.Errorf("summary = %q\nwant %q", e.Summary, want)
	}

	// One more day crosses 80% used with the item's own 70% threshold too.
	item.LowThresholdPercent = 70
	e = estimateItem(item, 80, day(7))
	if !e.BelowThreshold || e.ThresholdPct != 70 || !e.ThresholdItem || e.PercentRemaining != 19 {
		t.Errorf("later estimate = %+v", e)
	}
	// Never below zero, and zero when out.
	if e := estimateItem(item, 80, day(100)); e.Remaining.Sign() != 0 || e.PercentUsed != 100 {
		t.Errorf("long after = %+v", e)
	}
	out := item
	out.Status = StatusOut
	if e := estimateItem(out, 80, testNow); e.Remaining.Sign() != 0 {
		t.Errorf("out = %+v", e)
	}
	if e := estimateItem(Item{}, 80, testNow); e != nil {
		t.Errorf("untracked estimate = %+v", e)
	}
	skipped := trackedButter()
	skipped.Tracking.SkippedUses = 1
	if e := estimateItem(skipped, 0, testNow); e.Summary != "About 100% left. 1 recipe couldn't be counted." || e.ThresholdPct != DefaultLowThresholdPercent {
		t.Errorf("skipped summary = %q (threshold %d)", e.Summary, e.ThresholdPct)
	}
}

func TestNeedsAutoLow(t *testing.T) {
	low := trackedButter()
	low.Tracking.SegmentRecipeUsed = "14"
	e := estimateItem(low, 80, testNow)
	if !needsAutoLow(low, e) {
		t.Fatal("87.5% used should need auto-low")
	}
	alerted := cloneItem(low)
	alerted.LowAlertCycleID = "cycle1"
	personLater := cloneItem(low)
	personLater.StatusSetAt = day(1)
	estimateLater := cloneItem(personLater)
	estimateLater.StatusSource = StatusSourceEstimate
	notInStock := cloneItem(low)
	notInStock.Status = StatusLow
	for name, item := range map[string]Item{"alerted this cycle": alerted, "person set status since": personLater, "already low": notInStock} {
		if needsAutoLow(item, estimateItem(item, 80, day(1))) {
			t.Errorf("%s: needsAutoLow = true", name)
		}
	}
	if !needsAutoLow(estimateLater, estimateItem(estimateLater, 80, day(1))) {
		t.Error("an estimate-set status must not block the next check")
	}
}

func TestApplyPersonEdit(t *testing.T) {
	ids := 0
	newID := func() string { ids++; return "edit" + string(rune('0'+ids)) }

	// An untracked item gets a cycle when a person sets an amount.
	before := Item{Status: StatusInStock}
	after := cloneItem(before)
	after.Quantity, after.Unit = "2", "cup"
	applyPersonEdit(before, &after, testNow, newID)
	if after.Tracking == nil || after.Tracking.CycleSource != CycleEdit || after.Tracking.Reference != "2" || after.Tracking.Unit != "cup" {
		t.Fatalf("tracking = %+v", after.Tracking)
	}

	// A correction below the start closes the segment as observed and keeps
	// the cycle.
	before = trackedButter()
	before.Tracking.SegmentRecipeUsed, before.Tracking.RecipeUsed, before.Tracking.RecipeUses = "4", "4", 1
	after = cloneItem(before)
	after.Quantity, after.Unit = "1/2", "cup"
	applyPersonEdit(before, &after, day(3), newID)
	tr := after.Tracking
	if tr.CycleID != "cycle1" || tr.SegmentStart != "8" || !tr.SegmentStartedAt.Equal(day(3)) || tr.SegmentRecipeUsed != "0" || tr.RecipeUsed != "4" || tr.Reference != "16" {
		t.Errorf("corrected tracking = %+v", tr)
	}
	if len(after.History) != 1 || after.History[0].Remaining != "8" || !after.History[0].Observed || after.History[0].RecipeUsed != "4" {
		t.Errorf("history = %+v", after.History)
	}
	if before.Tracking.SegmentStart != "16" || len(before.History) != 0 {
		t.Error("applyPersonEdit changed the original item")
	}
	if e := estimateItem(after, 80, day(3)); e.AdjustedAt.IsZero() || e.PercentRemaining != 50 {
		t.Errorf("estimate after correction = %+v", e)
	}

	// More than the cycle started with is a restock without a purchase.
	after = cloneItem(before)
	after.Quantity, after.Unit = "2", "cup"
	applyPersonEdit(before, &after, day(3), newID)
	if after.Tracking.CycleID == "cycle1" || after.Tracking.Reference != "32" || after.Tracking.Unit != "tbsp" {
		t.Errorf("above reference = %+v", after.Tracking)
	}

	// A unit that doesn't convert starts over in that unit.
	after = cloneItem(before)
	after.Quantity, after.Unit = "3", "count"
	applyPersonEdit(before, &after, day(3), newID)
	if after.Tracking.CycleID == "cycle1" || after.Tracking.Unit != "count" || len(after.History) != 0 {
		t.Errorf("unconvertible = %+v, history %v", after.Tracking, after.History)
	}

	// Marking out keeps the cycle; clearing the amount ends tracking.
	after = cloneItem(before)
	after.Status, after.Quantity, after.Unit = StatusOut, "", ""
	applyPersonEdit(before, &after, day(4), newID)
	if after.Tracking == nil || after.StatusSource != StatusSourcePerson || !after.StatusSetAt.Equal(day(4)) {
		t.Errorf("marked out = %+v", after)
	}
	cleared := cloneItem(before)
	cleared.Quantity, cleared.Unit = "", ""
	applyPersonEdit(before, &cleared, day(4), newID)
	if cleared.Tracking != nil {
		t.Errorf("cleared tracking = %+v", cleared.Tracking)
	}

	// An amount after being out records that it ran out when marked out.
	wasOut := after
	back := cloneItem(wasOut)
	back.Status, back.Quantity, back.Unit = StatusInStock, "1", "cup"
	applyPersonEdit(wasOut, &back, day(6), newID)
	if n := len(back.History); n != 1 || back.History[0].Remaining != "0" || !back.History[0].EndedAt.Equal(day(4)) || !back.History[0].Observed {
		t.Errorf("history after out = %+v", back.History)
	}
	if back.Tracking.CycleID == "cycle1" || back.Tracking.Reference != "1" {
		t.Errorf("tracking after out = %+v", back.Tracking)
	}
}

func TestRestock(t *testing.T) {
	item := trackedButter()
	item.Tracking.SegmentRecipeUsed = "6"
	// Marked low: replacing it assumes the old stock was used up.
	item.Status = StatusLow
	restock(&item, "cycle2", CycleManual, "1", "cup", day(8))
	if item.Tracking.CycleID != "cycle2" || item.Tracking.Reference != "1" || item.Status != StatusInStock || !item.StatusSetAt.Equal(day(8)) {
		t.Errorf("restocked = %+v", item)
	}
	if h := item.History; len(h) != 1 || h[0].Remaining != "0" || h[0].Observed || !h[0].EndedAt.Equal(day(8)) || h[0].RecipeUsed != "6" {
		t.Errorf("history = %+v", h)
	}

	// Marked out by a person on day 10: the segment ended then.
	item.Status, item.StatusSource, item.StatusSetAt = StatusOut, StatusSourcePerson, day(10)
	item.Quantity, item.Unit = "", ""
	restock(&item, "cycle3", CycleGroceryList, "2", "package", day(12))
	if h := item.History[1]; !h.EndedAt.Equal(day(10)) || !h.Observed {
		t.Errorf("out segment = %+v", h)
	}
	if item.Rate == nil || item.Rate.Segments != 2 {
		t.Errorf("rate after two segments = %+v", item.Rate)
	}

	// A known package size tracks the purchase in the size's unit.
	item.UnitSize = &UnitSize{Unit: "package", Quantity: "8", SizeUnit: "oz"}
	item.Status = StatusLow
	restock(&item, "cycle4", CycleGroceryList, "2", "package", day(20))
	if item.Tracking.Unit != "oz" || item.Tracking.Reference != "16" || item.Quantity != "2" || item.Unit != "package" {
		t.Errorf("sized restock = %+v", item.Tracking)
	}
	// A purchase without an amount ends tracking.
	restock(&item, "cycle5", CycleManual, "", "", day(21))
	if item.Tracking != nil || item.Status != StatusInStock {
		t.Errorf("unquantified restock = %+v", item)
	}
	for range MaxHistorySegments + 2 {
		startCycle(&item, "x", CycleEdit, ratOf("1"), "oz", day(30))
		closeSegment(&item, day(31), ratOf("0"), false, day(31))
	}
	if len(item.History) != MaxHistorySegments {
		t.Errorf("history length = %d, want %d", len(item.History), MaxHistorySegments)
	}
}

func TestRestockCarriesInStockLeftovers(t *testing.T) {
	// Soy sauce: a 10 fl oz bottle, 4 fl oz used by recipes, still in stock.
	item := Item{ID: "soy", HouseholdID: testHousehold, Key: "soy sauce", DisplayName: "Soy Sauce", Status: StatusInStock,
		StatusSource: StatusSourcePerson, StatusSetAt: testNow, Quantity: "1", Unit: "package",
		UnitSize: &UnitSize{Unit: "package", Quantity: "10", SizeUnit: "fl oz"}}
	restock(&item, "c1", CycleProvider, "1", "package", testNow)
	item.Tracking.SegmentRecipeUsed, item.Tracking.RecipeUsed = "4", "4"

	// Another bottle arrives: 6 left + 10 bought = 16 fl oz, and 100% is 16.
	restock(&item, "c2", CycleProvider, "1", "package", day(10))
	if tr := item.Tracking; tr.CycleID != "c2" || tr.Unit != "fl oz" || tr.Reference != "16" || tr.SegmentStart != "16" {
		t.Fatalf("carried tracking = %+v", tr)
	}
	if h := item.History; len(h) != 1 || !h[0].Carried || h[0].Remaining != "6" || h[0].Observed {
		t.Errorf("carried history = %+v", h)
	}
	// Carried segments never teach the rate, however many there are.
	for _, id := range []string{"c3", "c4", "c5"} {
		restock(&item, id, CycleProvider, "1", "package", day(30))
	}
	if item.Rate != nil {
		t.Errorf("rate from carried segments = %+v", item.Rate)
	}

	// Less than MinCarryPercent left counts as used up.
	item.Tracking.SegmentRecipeUsed = new(big.Rat).Sub(ratOf(item.Tracking.SegmentStart), ratOf("1")).RatString()
	restock(&item, "c9", CycleManual, "1", "package", day(70))
	if tr := item.Tracking; tr.Reference != "10" {
		t.Errorf("crumb carried: %+v", tr)
	}
	if h := item.History[len(item.History)-1]; h.Carried || h.Remaining != "0" {
		t.Errorf("crumb segment = %+v", h)
	}
}
