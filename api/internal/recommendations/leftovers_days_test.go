package recommendations

import (
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// Second meals go on the days still open from today, not all on the week's
// first open day: every pick used to say Sunday, even on a Tuesday.
func TestOpenDaysSkipTakenAndPastDays(t *testing.T) {
	plan := planning.Plan{
		Week: mustWeek(t, "2026-W40"), FirstDay: "sun",
		Entries: []planning.Entry{{RecipeID: "a", Day: "mon"}, {RecipeID: "b", Day: "thu"}},
	}
	today := plan.DateOf("tue")
	got := openDays(plan, "sun", today)
	want := []autopilot.Day{"tue", "wed", "fri", "sat"}
	if !slices.Equal(got, want) {
		t.Fatalf("open days on %s = %v, want %v", today, got, want)
	}
	if all := openDays(plan, "sun", plan.DateOf("sun")); all[0] != "sun" || len(all) != 5 {
		t.Errorf("open days on Sunday = %v, want Sunday first and 5 days", all)
	}
}

// Each recipe goes on the day Autopilot scores it best: tacos on taco
// Tuesday even though Monday is open first, and spaghetti on Monday.
func TestPlacePicksPutsEachRecipeOnItsBestDay(t *testing.T) {
	rec := func(id string, score float64) autopilot.Recommendation {
		return autopilot.Recommendation{ItemID: id, Score: score}
	}
	days := []autopilot.Day{"mon", "tue", "wed"}
	ranked := map[autopilot.Day][]autopilot.Recommendation{
		"mon": {rec("tacos", 2.0), rec("spaghetti", 1.8), rec("stir-fry", 1.0)},
		"tue": {rec("tacos", 3.5), rec("spaghetti", 1.2), rec("stir-fry", 0.9)},
		"wed": {rec("tacos", 1.9), rec("stir-fry", 1.5), rec("spaghetti", 1.1)},
	}
	got := placePicks(days, ranked, 3)
	want := []struct{ day, id string }{{"mon", "spaghetti"}, {"tue", "tacos"}, {"wed", "stir-fry"}}
	if len(got) != len(want) {
		t.Fatalf("picks = %+v", got)
	}
	for i, w := range want {
		if string(got[i].day) != w.day || got[i].item.ItemID != w.id {
			t.Errorf("pick %d = %s on %s, want %s on %s", i, got[i].item.ItemID, got[i].day, w.id, w.day)
		}
	}

	// One open day: the three best recipes all go on it.
	one := placePicks([]autopilot.Day{"wed"}, ranked, 3)
	if len(one) != 3 || one[0].item.ItemID != "tacos" {
		t.Errorf("one open day = %+v, want three picks, tacos first", one)
	}
}
