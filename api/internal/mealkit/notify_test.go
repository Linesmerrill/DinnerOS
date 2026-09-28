package mealkit

import (
	"strings"
	"testing"
)

// TestFinishedBody keeps the finished notification short: one idea per
// sentence, failures and older orders only when there are some, and a single
// "tap" sentence at most.
func TestFinishedBody(t *testing.T) {
	failures := func(n int) []FailedRecipe { return make([]FailedRecipe, n) }
	tests := []struct {
		name string
		job  Job
		want string
	}{
		{"all in", Job{Checkpoint: Checkpoint{Imported: 670, Updated: 6}},
			"676 recipes are in your library."},
		{"one", Job{Checkpoint: Checkpoint{Imported: 1}},
			"1 recipe is in your library."},
		{"nothing new", Job{Checkpoint: Checkpoint{Unchanged: 40}},
			"No new recipes this time."},
		{"older orders", Job{Checkpoint: Checkpoint{Imported: 676}, Harvest: HarvestReport{Stopped: HarvestStopCap}},
			"676 recipes are in your library. Older orders are waiting. Tap to import them."},
		{"real failures", Job{Checkpoint: Checkpoint{Imported: 676, Failures: failures(9)}},
			"676 recipes are in your library. 9 couldn't be imported. Tap to see which."},
		{"both", Job{Checkpoint: Checkpoint{Imported: 676, Failures: failures(2)}, Harvest: HarvestReport{Stopped: HarvestStopCap}},
			"676 recipes are in your library. 2 couldn't be imported. Older orders are waiting. Tap for details."},
		// Merged duplicates are counted as already in the library, never as
		// failures.
		{"merged duplicates", Job{Checkpoint: Checkpoint{Imported: 676, Unchanged: 9}},
			"676 recipes are in your library."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := finishedBody(tt.job)
			if got != tt.want {
				t.Errorf("finishedBody = %q, want %q", got, tt.want)
			}
			if strings.Count(got, "Tap") > 1 || strings.Contains(got, "Open Recipe Import") {
				t.Errorf("finishedBody = %q repeats its call to action", got)
			}
		})
	}
}
