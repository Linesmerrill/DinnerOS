package baseline

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// A meal another coming week holds (planned later, or in another week's
// suggestions) is spaced away like one planned last week, but teaches
// nothing: no familiarity, no habit (decision 642).
func TestNearbyWeekOnlySpacesMealsApart(t *testing.T) {
	p := New(Options{})
	in := input(meal("curry"), meal("tacos"), meal("soup"), meal("pasta"), meal("salad"), meal("burger"))
	in.History = []autopilot.Interaction{{Kind: autopilot.KindNearby, ItemID: "curry", Week: "2026-W39", Day: autopilot.Monday}}
	for _, rec := range rank(t, p, in, autopilot.Monday).Items {
		if rec.ItemID != "curry" {
			continue
		}
		if rec.Signals[SignalRecency] != -1 || rec.Signals[SignalFamiliarity] != 0 || rec.Signals[SignalWeekdayAffinity] != 0 {
			t.Errorf("curry signals = %v; want only recency -1", rec.Signals)
		}
		return
	}
	t.Fatal("curry not ranked")
}
