package cooking

import (
	"context"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

const hh = "66e5a1f2c3b4a5d6e7f80a01"

func newStore(t *testing.T) *Store {
	t.Helper()
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
		t.Fatal(err)
	}
	return NewStore(client.Database())
}

// Two devices checking things off at once both land; the step and the day's
// timers are shared; another dish has its own checks but the same timers.
func TestIntegrationSessionsMergeAcrossDevices(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	date := "2026-09-28"

	if got, err := s.Get(ctx, hh, "tacos", date); err != nil || len(got.Checked) != 0 || got.CurrentStep != nil {
		t.Fatalf("empty session = %+v, %v", got, err)
	}
	if _, err := s.Apply(ctx, hh, "tacos", date, []Op{{Kind: "check", IDs: []string{"a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	// The other device, which hadn't seen a or b.
	step := 3
	got, err := s.Apply(ctx, hh, "tacos", date, []Op{{Kind: "check", IDs: []string{"c"}}, {Kind: "uncheck", IDs: []string{"a"}}, {Kind: "step", Step: &step}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Checked) != 2 || got.CurrentStep == nil || *got.CurrentStep != 3 {
		t.Fatalf("merged = %+v, want b and c checked, step 3", got)
	}

	ends := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Millisecond)
	timer := Timer{ID: "t1", Label: "Pasta", TotalSeconds: 600, EndsAt: &ends}
	if _, err := s.Apply(ctx, hh, "tacos", date, []Op{{Kind: "timer", Timer: &timer}}); err != nil {
		t.Fatal(err)
	}
	other, err := s.Get(ctx, hh, "bread", date)
	if err != nil || len(other.Checked) != 0 || len(other.Timers) != 1 || !other.Timers[0].EndsAt.Equal(ends) {
		t.Fatalf("another dish = %+v, %v; want no checks and the same timer", other, err)
	}
	// Updating a timer replaces it; removing it removes it.
	timer.Finished, timer.EndsAt = true, nil
	if got, _ = s.Apply(ctx, hh, "bread", date, []Op{{Kind: "timer", Timer: &timer}}); len(got.Timers) != 1 || !got.Timers[0].Finished {
		t.Errorf("updated timer = %+v", got.Timers)
	}
	if got, _ = s.Apply(ctx, hh, "bread", date, []Op{{Kind: "removeTimer", ID: "t1"}}); len(got.Timers) != 0 {
		t.Errorf("after remove = %+v", got.Timers)
	}
	if _, err := s.Apply(ctx, hh, "tacos", "not-a-date", []Op{{Kind: "check", IDs: []string{"x"}}}); err == nil {
		t.Error("a bad date should be rejected")
	}
}
