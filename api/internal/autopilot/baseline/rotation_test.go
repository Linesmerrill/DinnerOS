package baseline

import (
	"fmt"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// readiness ranks the meals for Monday and returns each one's rotation signal.
func readiness(t *testing.T, in autopilot.Input) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, rec := range rank(t, New(Options{}), in, autopilot.Monday).Items {
		out[rec.ItemID] = rec.Signals[SignalRotation]
	}
	return out
}

// cooked is a meal cooked on a day of the planned week (W38), or of week.
func cooked(id string, day autopilot.Day, week ...string) autopilot.Interaction {
	w := testWeek
	if len(week) > 0 {
		w = week[0]
	}
	return autopilot.Interaction{Kind: autopilot.KindCooked, ItemID: id, Week: w, Day: day}
}

// A favorite the household just had goes to the bottom of the pasta stack,
// under every pasta it hasn't had since, however well it's rated, and stays
// buried until six other pastas have had their turn (decision 647).
func TestAJustHadFavoriteWaitsForTheOtherPastas(t *testing.T) {
	var catalog []autopilot.Item
	for i := range 8 {
		catalog = append(catalog, meal(fmt.Sprintf("pasta-%d", i), category("pasta")))
	}
	in := input(catalog...)
	in.Ratings = []autopilot.Rating{rate("pasta-0", 5, autopilot.FeedbackMakeAgain)}
	for _, week := range []string{"2026-W30", "2026-W32", "2026-W34", "2026-W36"} {
		in.History = append(in.History, cooked("pasta-0", autopilot.Monday, week), autopilot.Interaction{Kind: autopilot.KindOrdered, ItemID: "pasta-0", Week: week})
	}
	in.History = append(in.History, cooked("pasta-0", autopilot.Monday))
	ready := readiness(t, in)
	if ready["pasta-0"] != -1 || ready["pasta-1"] != 0 {
		t.Fatalf("rotation = %v; want the favorite at -1 and the others at 0", ready)
	}

	// Each other pasta had since brings it a step up, but it stays below
	// the pastas ahead of it until the sixth.
	for i := 1; i <= rotationDepth; i++ {
		in.History = append(in.History, cooked(fmt.Sprintf("pasta-%d", i), autopilot.Days[min(i, 6)]))
		want := -1 + float64(i)/rotationDepth/2
		if i == rotationDepth {
			want = 0
		}
		got := readiness(t, in)["pasta-0"]
		if got != round3(want) {
			t.Errorf("after %d other pastas, rotation = %v, want %v", i, got, round3(want))
		}
		top := rank(t, New(Options{}), in, autopilot.Monday).Items[0].ItemID
		if i < rotationDepth && top == "pasta-0" {
			t.Errorf("after %d other pastas, the favorite already ranks first", i)
		}
		if i == rotationDepth && top != "pasta-0" {
			t.Errorf("on its turn, the five-star favorite ranks below %s", top)
		}
	}
}

// Each kind keeps its own stack, the most specific one the meal has: tacos
// had since don't bring a pasta back, and a pasta had since doesn't bury the
// tacos.
func TestKindsKeepTheirOwnStacks(t *testing.T) {
	in := input(
		meal("pasta-a", category("pasta")), meal("pasta-b", category("pasta")), meal("pasta-c", category("pasta")),
		meal("taco-a", category("tacos"), dishes("taco")), meal("taco-b", category("tacos"), dishes("taco")),
		meal("enchiladas", category("tacos")),
	)
	in.History = []autopilot.Interaction{
		cooked("taco-a", autopilot.Monday), cooked("pasta-a", autopilot.Tuesday),
		cooked("taco-b", autopilot.Wednesday), cooked("taco-a", autopilot.Thursday),
	}
	ready := readiness(t, in)
	if ready["pasta-a"] != -1 {
		t.Errorf("pasta-a = %v; tacos since shouldn't bring it back", ready["pasta-a"])
	}
	// taco-b came after taco-a's first time but before its second.
	if ready["taco-a"] != -1 || ready["taco-b"] != 0 {
		t.Errorf("tacos = %v, %v; want -1 and 0", ready["taco-a"], ready["taco-b"])
	}

	// A small kind cycles through all its meals: with three pastas, the two
	// others bring pasta-a back.
	in.History = append(in.History, cooked("pasta-b", autopilot.Friday), cooked("pasta-c", autopilot.Saturday))
	if got := readiness(t, in)["pasta-a"]; got != 0 {
		t.Errorf("after the other two pastas, pasta-a = %v, want 0", got)
	}
}

// A kind the household rarely has still comes back on time alone.
func TestARarelyHadKindComesBackInTime(t *testing.T) {
	in := input(meal("pasta-a", category("pasta")), meal("pasta-b", category("pasta")))
	in.History = []autopilot.Interaction{cooked("pasta-a", autopilot.Monday, "2026-W30")} // 8 weeks before W38
	if got := readiness(t, in)["pasta-a"]; got != -0.75 {
		t.Errorf("8 weeks later, rotation = %v, want -0.75", got)
	}
	in.History = []autopilot.Interaction{cooked("pasta-a", autopilot.Monday, "2026-W22")}
	if got := readiness(t, in)["pasta-a"]; got != 0 {
		t.Errorf("16 weeks later, rotation = %v, want 0", got)
	}
}

// Weeks after the planned one and skipped meals don't touch the stacks; a
// coming week planned before it does.
func TestOnlyMealsHadByThePlannedWeekCount(t *testing.T) {
	in := input(meal("pasta-a", category("pasta")), meal("pasta-b", category("pasta")), meal("pasta-c", category("pasta")))
	in.Week = "2026-W40"
	in.History = []autopilot.Interaction{
		{Kind: autopilot.KindSkipped, ItemID: "pasta-a", Week: "2026-W39"},
		{Kind: autopilot.KindNearby, ItemID: "pasta-b", Week: "2026-W41"},
		{Kind: autopilot.KindNearby, ItemID: "pasta-c", Week: "2026-W39"},
	}
	ready := readiness(t, in)
	if ready["pasta-a"] != 0 || ready["pasta-b"] != 0 || ready["pasta-c"] >= -0.9 {
		t.Errorf("rotation = %v; want only pasta-c, planned the week before, near the bottom", ready)
	}
}

// Week after week, Autopilot works through the library instead of looping on
// the household's favorites.
func TestWeeksWorkThroughTheLibrary(t *testing.T) {
	kinds := []string{"pasta", "tacos", "soup", "bowl", "curry"}
	var catalog []autopilot.Item
	in := input()
	for _, k := range kinds {
		for i := range 6 {
			id := fmt.Sprintf("%s-%d", k, i)
			catalog = append(catalog, meal(id, category(k)))
			if i == 0 {
				// Each kind's favorite: five stars, ordered every other week.
				in.Ratings = append(in.Ratings, rate(id, 5, autopilot.FeedbackMakeAgain))
				for w := 20; w < 38; w += 2 {
					in.History = append(in.History, autopilot.Interaction{Kind: autopilot.KindOrdered, ItemID: id, Week: fmt.Sprintf("2026-W%02d", w)})
				}
			}
		}
	}
	in.Catalog = catalog
	p := New(Options{})
	seen := map[string]int{}
	lastWeek := map[string]int{}
	for week := 38; week < 50; week++ {
		in.Week = fmt.Sprintf("2026-W%02d", week)
		res := generate(t, p, in)
		for _, id := range pickedIDs(res) {
			if last, ok := lastWeek[id]; ok && week-last < 5 {
				t.Errorf("%s again in %s, %d weeks after the last time", id, in.Week, week-last)
			}
			seen[id]++
			lastWeek[id] = week
			in.History = append(in.History, autopilot.Interaction{Kind: autopilot.KindPlanned, ItemID: id, Week: in.Week, Day: slotFor(res, id)})
		}
	}
	for _, it := range catalog {
		if seen[it.ID] == 0 {
			t.Errorf("%s never suggested in 12 weeks: %v", it.ID, seen)
		}
	}
}

func slotFor(res autopilot.WeekResult, id string) autopilot.Day {
	for _, s := range res.Slots {
		if s.ItemID == id {
			return s.Day
		}
	}
	return ""
}
