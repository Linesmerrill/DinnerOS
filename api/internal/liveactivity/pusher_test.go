package liveactivity

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
)

func job(status mealkit.JobStatus, done, total int) mealkit.Job {
	j := mealkit.Job{ID: jobAda, HouseholdID: hhAda, Source: mealkit.SourceHelloFresh, Status: status}
	for i := range total {
		id := "recipe-" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676))
		j.Checkpoint.Orders = append(j.Checkpoint.Orders, mealkit.OrderedRecipe{SourceRecipeID: id})
		if i < done {
			j.Checkpoint.Done = append(j.Checkpoint.Done, id)
		}
	}
	return j
}

type pusherFixture struct {
	store  *memoryStore
	sender *fakeSender
	pusher *Pusher
	logs   *bytes.Buffer
	now    time.Time
}

func newPusherFixture(t *testing.T) *pusherFixture {
	t.Helper()
	f := &pusherFixture{
		store: newMemoryStore(), sender: &fakeSender{}, logs: &bytes.Buffer{},
		now: time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC),
	}
	f.pusher = NewPusher(PusherOptions{
		Store: f.store, Sender: f.sender,
		Logger: slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:    func() time.Time { return f.now },
	})
	return f
}

func (f *pusherFixture) register(t *testing.T) {
	t.Helper()
	if err := f.store.Register(context.Background(), hhAda, "hellofresh", jobAda, token1, push.EnvironmentSandbox, f.now); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
}

func TestNoPushWithoutAToken(t *testing.T) {
	f := newPusherFixture(t)
	f.pusher.ImportProgressed(context.Background(), job(mealkit.JobRunning, 10, 740))
	f.pusher.ImportEnded(context.Background(), job(mealkit.JobSucceeded, 740, 740))
	if len(f.sender.sent) != 0 {
		t.Fatalf("sent %d pushes to a job with no activity", len(f.sender.sent))
	}
}

func TestProgressSendsALowPriorityUpdateToTheStoredToken(t *testing.T) {
	f := newPusherFixture(t)
	f.register(t)
	f.pusher.ImportProgressed(context.Background(), job(mealkit.JobRunning, 40, 740))
	if len(f.sender.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(f.sender.sent))
	}
	m := f.sender.sent[0]
	if m.Token != token1 || m.Environment != push.EnvironmentSandbox || m.Event != push.LiveActivityUpdate || !m.LowPriority {
		t.Errorf("message = %+v", m)
	}
	if m.ContentState != (ContentState{Phase: PhaseImporting, Done: 40, Total: 740}) {
		t.Errorf("content state = %+v", m.ContentState)
	}
	if !m.StaleDate.Equal(f.now.Add(StaleAfter)) || !m.DismissalDate.IsZero() {
		t.Errorf("stale = %v, dismissal = %v", m.StaleDate, m.DismissalDate)
	}
	reg, _ := f.store.Get(context.Background(), jobAda)
	if !reg.LastSentAt.Equal(f.now) || reg.LastSent.Done != 40 {
		t.Errorf("registration after send = %+v", reg)
	}
}

func TestProgressIsRateLimited(t *testing.T) {
	f := newPusherFixture(t)
	f.register(t)
	ctx := context.Background()
	f.pusher.ImportProgressed(ctx, job(mealkit.JobRunning, 10, 740))
	f.now = f.now.Add(10 * time.Second)
	f.pusher.ImportProgressed(ctx, job(mealkit.JobRunning, 20, 740)) // too soon
	f.now = f.now.Add(21 * time.Second)
	f.pusher.ImportProgressed(ctx, job(mealkit.JobRunning, 30, 740)) // 31 s after the first
	f.now = f.now.Add(15 * time.Second)
	f.pusher.ImportProgressed(ctx, job(mealkit.JobQueued, 30, 740)) // a phase change may go sooner
	var dones []int
	for _, m := range f.sender.sent {
		dones = append(dones, m.ContentState.(ContentState).Done)
	}
	if len(f.sender.sent) != 3 || f.sender.sent[2].ContentState.(ContentState).Phase != PhaseWaiting {
		t.Fatalf("sent %v (%d pushes), want 10, 30, then waiting", dones, len(f.sender.sent))
	}
}

func TestEndSendsTheGreenFinalStateWithADismissalDateAndClearsTheToken(t *testing.T) {
	f := newPusherFixture(t)
	f.register(t)
	j := job(mealkit.JobSucceeded, 740, 740)
	j.Checkpoint.Imported, j.Checkpoint.Updated, j.Checkpoint.Unchanged = 700, 30, 7
	j.Checkpoint.Failures = []mealkit.FailedRecipe{{SourceRecipeID: "x"}, {SourceRecipeID: "y"}, {SourceRecipeID: "z"}}
	f.pusher.ImportEnded(context.Background(), j)
	if len(f.sender.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(f.sender.sent))
	}
	m := f.sender.sent[0]
	if m.Event != push.LiveActivityEnd || m.LowPriority {
		t.Errorf("end = %+v", m)
	}
	if m.ContentState != (ContentState{Phase: PhaseDone, Done: 737, Total: 740, Failed: 3}) {
		t.Errorf("content state = %+v", m.ContentState)
	}
	if !m.DismissalDate.Equal(f.now.Add(DoneLinger)) {
		t.Errorf("dismissal = %v", m.DismissalDate)
	}
	if _, err := f.store.Get(context.Background(), jobAda); err != ErrNotFound {
		t.Errorf("token still stored after the job ended: %v", err)
	}
	// And nothing more goes to it.
	f.pusher.ImportProgressed(context.Background(), job(mealkit.JobRunning, 1, 740))
	if len(f.sender.sent) != 1 {
		t.Errorf("pushed after the end")
	}
}

func TestStoppedAndCanceledEndings(t *testing.T) {
	for _, tc := range []struct {
		status  mealkit.JobStatus
		phase   Phase
		lingers time.Duration
	}{
		{mealkit.JobDead, PhaseFailed, FailedLinger},
		{mealkit.JobCanceled, PhaseCanceled, CanceledLinger},
	} {
		f := newPusherFixture(t)
		f.register(t)
		f.pusher.ImportEnded(context.Background(), job(tc.status, 120, 740))
		m := f.sender.sent[0]
		if m.ContentState.(ContentState).Phase != tc.phase || !m.DismissalDate.Equal(f.now.Add(tc.lingers)) {
			t.Errorf("%s: sent %+v", tc.status, m)
		}
	}
}

func TestTheTokenIsClearedEvenWhenTheEndPushFails(t *testing.T) {
	f := newPusherFixture(t)
	f.register(t)
	f.sender.err = &push.APNsError{Status: 500, Reason: "InternalServerError"}
	f.pusher.ImportEnded(context.Background(), job(mealkit.JobSucceeded, 3, 3))
	if _, err := f.store.Get(context.Background(), jobAda); err != ErrNotFound {
		t.Fatalf("token kept after the job ended: %v", err)
	}
}

func TestAnInvalidTokenIsRemovedAndNeverLogged(t *testing.T) {
	f := newPusherFixture(t)
	f.register(t)
	f.sender.err = &push.APNsError{Status: 410, Reason: "Unregistered"}
	f.pusher.ImportProgressed(context.Background(), job(mealkit.JobRunning, 10, 740))
	if _, err := f.store.Get(context.Background(), jobAda); err != ErrNotFound {
		t.Fatalf("a token APNs refused is still stored: %v", err)
	}
	f.sender.err = &push.APNsError{Status: 500, Reason: "InternalServerError"}
	f.register(t)
	f.pusher.ImportProgressed(context.Background(), job(mealkit.JobRunning, 20, 740))
	if strings.Contains(f.logs.String(), token1) {
		t.Fatalf("the activity token reached the log:\n%s", f.logs.String())
	}
	if !strings.Contains(f.logs.String(), "live activity push failed") {
		t.Errorf("a failed push was not logged:\n%s", f.logs.String())
	}
}

func TestStateForMapsEveryStatus(t *testing.T) {
	for status, want := range map[mealkit.JobStatus]Phase{
		mealkit.JobQueued: PhaseWaiting, mealkit.JobRunning: PhaseImporting, mealkit.JobSucceeded: PhaseDone,
		mealkit.JobDead: PhaseFailed, mealkit.JobCanceled: PhaseCanceled,
	} {
		if got := StateFor(job(status, 0, 5)).Phase; got != want {
			t.Errorf("%s -> %s, want %s", status, got, want)
		}
	}
}

func TestTheContentStateIsSmallAndCarriesOnlyCounts(t *testing.T) {
	body, err := push.LiveActivityPayload(push.LiveActivityMessage{
		Event: push.LiveActivityEnd, ContentState: ContentState{Phase: PhaseDone, Done: 1000, Total: 1000, Failed: 100},
		Timestamp: time.Now(), DismissalDate: time.Now().Add(DoneLinger),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 512 {
		t.Errorf("payload is %d bytes; Live Activities allow 4096 and this should be tiny", len(body))
	}
	var decoded struct {
		APS struct {
			ContentState map[string]any `json:"content-state"`
		} `json:"aps"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	for key := range decoded.APS.ContentState {
		switch key {
		case "phase", "done", "total", "failed":
		default:
			t.Errorf("content-state carries %q; it may carry counts only", key)
		}
	}
}

func TestLimiter(t *testing.T) {
	l := DefaultLimiter
	t0 := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	importing := func(done int) ContentState { return ContentState{Phase: PhaseImporting, Done: done, Total: 740} }
	cases := []struct {
		name   string
		last   ContentState
		lastAt time.Time
		next   ContentState
		after  time.Duration
		want   bool
	}{
		{"first update", ContentState{}, time.Time{}, importing(10), 0, true},
		{"nothing new", importing(10), t0, importing(10), time.Hour, false},
		{"same phase too soon", importing(10), t0, importing(20), 29 * time.Second, false},
		{"same phase after the interval", importing(10), t0, importing(20), 30 * time.Second, true},
		{"phase change too soon", importing(10), t0, ContentState{Phase: PhaseWaiting, Done: 10, Total: 740}, 14 * time.Second, false},
		{"phase change", importing(10), t0, ContentState{Phase: PhaseWaiting, Done: 10, Total: 740}, 15 * time.Second, true},
	}
	for _, tc := range cases {
		if got := l.Allow(tc.last, tc.lastAt, tc.next, t0.Add(tc.after)); got != tc.want {
			t.Errorf("%s: Allow = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTheLimiterNeverAllowsMoreThanAHandfulAMinute(t *testing.T) {
	// A worst case: a checkpoint every second, flipping phase every time.
	l := DefaultLimiter
	t0 := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	var last ContentState
	var lastAt time.Time
	sent := 0
	for s := range 60 {
		next := ContentState{Phase: PhaseImporting, Done: s, Total: 740}
		if s%2 == 1 {
			next.Phase = PhaseWaiting
		}
		now := t0.Add(time.Duration(s) * time.Second)
		if l.Allow(last, lastAt, next, now) {
			sent++
			last, lastAt = next, now
		}
	}
	if sent > 4 {
		t.Fatalf("%d updates in one minute; the budget is at most 4", sent)
	}
}
