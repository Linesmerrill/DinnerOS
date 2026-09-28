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
