package mealkit

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// newRunner builds the web process's runner over the fixture's queue, with a
// clock the test moves. The scheduled command is not involved unless a test
// builds one with f.worker.
func (f *workerFixture) runner(t *testing.T, clock *time.Time, mutate ...func(*RunnerOptions)) *Runner {
	t.Helper()
	opts := RunnerOptions{
		Worker: WorkerOptions{
			Store: f.store, Service: f.service, Publisher: f.publisher,
			Notifier: f.notifier, Owner: "web-test",
			Random: func() float64 { return 0 },
		},
	}
	if clock != nil {
		opts.Worker.Now = func() time.Time { return *clock }
	}
	for _, m := range mutate {
		m(&opts)
	}
	return NewRunner(opts)
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The production failure this exists for: a member queued 740 recipes and the
// screen sat on "queued" because nothing but a (missing) Scheduler job ever
// drained the queue. Queueing now starts work in the web process itself.
func TestQueueingStartsTheFirstBatchWithoutTheScheduler(t *testing.T) {
	store, source, publisher := &memoryStore{}, &fakeSource{}, &fakePublisher{}
	service := NewService(ServiceOptions{
		Store: store, Enabled: true, Sources: map[string]Source{SourceHelloFresh: source},
	})
	runner := NewRunner(RunnerOptions{
		Worker: WorkerOptions{Store: store, Service: service, Publisher: publisher, Owner: "web-test"},
		// A poll far longer than the test proves the kick, not a poll, is
		// what started the work.
		Poll: time.Hour,
	})
	service.SetKicker(runner)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	// Let the runner finish its start-up look at the empty queue and go idle
	// on its hour-long poll, so only the kick can start what is queued next.
	time.Sleep(50 * time.Millisecond)

	job, err := service.StartImport(ctx, ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: orders(120),
	})
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	if job.Status != JobQueued {
		t.Fatalf("the request answered %q; queueing itself must stay quick and say queued", job.Status)
	}

	// No scheduled run anywhere, and the first batch lands anyway.
	waitFor(t, "the first batch", func() bool {
		j, _ := store.GetJob(context.Background(), hhAda, job.ID)
		return j.RecipesDone() == DefaultBatchSize && j.Status == JobQueued
	})
	got, _ := store.GetJob(context.Background(), hhAda, job.ID)
	if n := len(source.fetchedIDs()); n != DefaultBatchSize {
		t.Errorf("fetched %d pages in the first batch, want %d", n, DefaultBatchSize)
	}
	// It rests before the next batch instead of carrying straight on.
	if rest := got.AvailableAt.Sub(got.UpdatedAt); rest < DefaultBatchPause-time.Second {
		t.Errorf("the job rests %v before its next batch, want about %v", rest, DefaultBatchPause)
	}
}

func TestTheFirstBatchStopsAtItsCapCheckpointsAsItGoesAndSaysRunning(t *testing.T) {
	f := newWorkerFixture(t, orders(120))
	clock := time.Now().UTC().Truncate(time.Millisecond)
	r := f.runner(t, &clock)

	// What the app would read at each page fetch.
	type seen struct {
		status JobStatus
		done   int
	}
	var observed []seen
	f.source.beforeRecipe = func(OrderedRecipe) {
		j := f.reload(t)
		observed = append(observed, seen{j.Status, j.RecipesDone()})
	}

	r.Kick(f.job.ID)
	if !r.Step(context.Background()) {
		t.Fatal("the kicked job was not started")
	}

	if len(observed) != DefaultBatchSize {
		t.Fatalf("fetched %d pages, want the batch cap %d", len(observed), DefaultBatchSize)
	}
	// While it is being worked the member sees running, and the count climbs
	// by 10 at every checkpoint: 0 for the first ten pages, 10 for the next…
	for i, s := range observed {
		if s.status != JobRunning {
			t.Fatalf("page %d: status %q while the batch was working", i, s.status)
		}
		if want := (i / importBatchSize) * importBatchSize; s.done != want {
			t.Fatalf("page %d: recipesDone = %d, want %d (a checkpoint every %d)", i, s.done, want, importBatchSize)
		}
	}

	job := f.reload(t)
	if job.Status != JobQueued || job.RecipesDone() != DefaultBatchSize {
		t.Fatalf("after the batch: status %q done %d", job.Status, job.RecipesDone())
	}
	if !job.AvailableAt.Equal(clock.Add(DefaultBatchPause)) {
		t.Errorf("next batch at %v, want %v", job.AvailableAt, clock.Add(DefaultBatchPause))
	}
	// Stopping on the batch cap is the per-run cap path: no attempt spent.
	if job.Attempts != 0 || job.LeaseOwner != "" {
		t.Errorf("after the batch: attempts %d, lease %q", job.Attempts, job.LeaseOwner)
	}
	// The response the app polls says when the next batch is due.
	resp := newJobResponse(job)
	if resp.NextRunAt == nil || !resp.NextRunAt.Equal(job.AvailableAt) || resp.RecipesDone != DefaultBatchSize {
		t.Errorf("status response = next %v done %d", resp.NextRunAt, resp.RecipesDone)
	}
}

func TestTheRunnerKeepsGoingBatchByBatchUntilTheImportIsDone(t *testing.T) {
	f := newWorkerFixture(t, orders(120))
	clock := time.Now().UTC().Truncate(time.Millisecond)
	r := f.runner(t, &clock)
	r.Kick(f.job.ID)

	var batches []int
	for range 10 {
		if r.Step(context.Background()) {
			batches = append(batches, f.reload(t).RecipesDone())
			// Resting: nothing is due until the pause has passed.
			if f.reload(t).Status == JobQueued && r.Step(context.Background()) {
				t.Fatal("a job was picked straight back up without its pause")
			}
		}
		if f.reload(t).Status.Terminal() {
			break
		}
		clock = clock.Add(DefaultBatchPause)
	}

	if want := []int{50, 100, 120}; !slices.Equal(batches, want) {
		t.Fatalf("recipesDone after each batch = %v, want %v", batches, want)
	}
	job := f.reload(t)
	if job.Status != JobSucceeded || job.Checkpoint.Imported != 120 {
		t.Fatalf("job = status %q imported %d", job.Status, job.Checkpoint.Imported)
	}
	if fetched := f.source.fetchedIDs(); len(fetched) != 120 || len(slices.Compact(slices.Sorted(slices.Values(fetched)))) != 120 {
		t.Errorf("fetched %d pages, with repeats", len(fetched))
	}
	if !slices.Contains(f.notifier.types(), notifications.TypeRecipeImportFinished) {
		t.Errorf("notifications = %v, want the finished one", f.notifier.types())
	}
	// Once it's done nothing more is resting in the queue.
	if resp := newJobResponse(job); resp.NextRunAt != nil {
		t.Errorf("a finished job still says it runs next at %v", resp.NextRunAt)
	}
}

// The web runner and the scheduled command drain one queue. Whichever claims
// a job holds it; the other gets nothing and fetches nothing.
func TestAScheduledRunCannotClaimAJobTheRunnerIsWorking(t *testing.T) {
	f := newWorkerFixture(t, orders(30))
	clock := time.Now().UTC().Truncate(time.Millisecond)
	r := f.runner(t, &clock)

	var scheduled Report
	raced := false
	f.source.beforeRecipe = func(OrderedRecipe) {
		if raced {
			return
		}
		raced = true
		// Mid-batch, Heroku Scheduler fires /importmealkit.
		var err error
		scheduled, err = f.worker(t, func(o *WorkerOptions) {
			o.Owner = "scheduler"
			o.Now = func() time.Time { return clock }
		}).Run(context.Background())
		if err != nil {
			t.Errorf("scheduled Run() error = %v", err)
		}
	}

	r.Kick(f.job.ID)
	if !r.Step(context.Background()) {
		t.Fatal("the runner did not start the job")
	}
	if scheduled.Claimed != 0 {
		t.Fatalf("the scheduled run claimed a job the runner held: %+v", scheduled)
	}
	job := f.reload(t)
	if job.Status != JobSucceeded || job.RecipesDone() != 30 {
		t.Fatalf("job = status %q done %d", job.Status, job.RecipesDone())
	}
	if fetched := f.source.fetchedIDs(); len(fetched) != 30 {
		t.Errorf("fetched %d pages for 30 recipes; something was processed twice", len(fetched))
	}
}

func TestTheRunnerCannotClaimAJobTheSchedulerIsWorking(t *testing.T) {
	f := newWorkerFixture(t, orders(30))
	clock := time.Now().UTC().Truncate(time.Millisecond)
	r := f.runner(t, &clock)

	var ran []bool
	f.source.beforeRecipe = func(OrderedRecipe) {
		if len(ran) > 0 {
			return
		}
		// Mid scheduled run, a member taps Import again (which kicks) and
		// the runner's own poll comes round.
		r.Kick(f.job.ID)
		ran = append(ran, r.Step(context.Background()))
	}
	report, err := f.worker(t, func(o *WorkerOptions) {
		o.Owner = "scheduler"
		o.Now = func() time.Time { return clock }
	}).Run(context.Background())
	if err != nil || report.Claimed != 1 {
		t.Fatalf("scheduled Run() = %+v, %v", report, err)
	}
	if len(ran) != 1 || ran[0] {
		t.Fatalf("the runner ran a batch while the scheduler held the job: %v", ran)
	}
	if fetched := f.source.fetchedIDs(); len(fetched) != 30 {
		t.Errorf("fetched %d pages for 30 recipes", len(fetched))
	}
}

// A web dyno killed without warning leaves its lease behind. Nobody touches
// the job until the lease expires; then the next worker — the runner in a new
// dyno here — resumes from the checkpoint.
func TestTheRunnerTakesOverALeaseHeldByADeadProcess(t *testing.T) {
	f := newWorkerFixture(t, orders(20))
	clock := time.Now().UTC().Truncate(time.Millisecond)
	f.store.jobs[0].Status = JobRunning
	f.store.jobs[0].LeaseOwner = "web-that-died"
	f.store.jobs[0].LeaseExpiresAt = clock.Add(3 * time.Minute)
	f.store.jobs[0].Checkpoint.Done = []string{"recipe-00", "recipe-01", "recipe-02", "recipe-03", "recipe-04",
		"recipe-05", "recipe-06", "recipe-07", "recipe-08", "recipe-09"}
	r := f.runner(t, &clock)

	// The lease is still live: the job is not the runner's to take.
	r.Kick(f.job.ID)
	if r.Step(context.Background()) {
		t.Fatal("the runner took a job whose lease had not expired")
	}

	clock = clock.Add(4 * time.Minute)
	if !r.Step(context.Background()) {
		t.Fatal("the runner did not take over the expired lease")
	}
	job := f.reload(t)
	if job.Status != JobSucceeded || job.RecipesDone() != 20 {
		t.Fatalf("job = status %q done %d", job.Status, job.RecipesDone())
	}
	// Only the ten the dead process had not checkpointed were fetched.
	if got := f.source.fetchedIDs(); len(got) != 10 || got[0] != "recipe-10" {
		t.Errorf("fetched %v, want only recipe-10 onwards", got)
	}
}

// A graceful restart or an Eco dyno going to sleep cancels the runner. It
// hands the job back at once instead of leaving a bar frozen for a lease.
func TestTheRunnerHandsItsBatchBackWhenTheDynoStops(t *testing.T) {
	f := newWorkerFixture(t, orders(40))
	clock := time.Now().UTC().Truncate(time.Millisecond)
	r := f.runner(t, &clock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fetches := 0
	f.source.beforeRecipe = func(OrderedRecipe) {
		fetches++
		if fetches == 15 {
			cancel() // SIGTERM, mid-batch
		}
	}
	src := &cancelingSource{fakeSource: f.source}
	f.service.sources[SourceHelloFresh] = src

	r.Kick(f.job.ID)
	r.Step(ctx)

	job := f.reload(t)
	if job.Status != JobQueued || job.LeaseOwner != "" || job.AvailableAt.After(clock) {
		t.Fatalf("after stopping: status %q lease %q available %v (now %v)", job.Status, job.LeaseOwner, job.AvailableAt, clock)
	}
	// The checkpoint of 10 survived (the in-memory store also lets the
	// partial flush land, which a canceled Mongo write would not); being
	// stopped cost no attempt.
	if job.RecipesDone() < 10 || job.RecipesDone() >= 15 || job.Attempts != 0 {
		t.Errorf("after stopping: done %d attempts %d", job.RecipesDone(), job.Attempts)
	}
}

// cancelingSource makes a canceled context fail the fetch, the way the real
// Fetcher's polite sleep does.
type cancelingSource struct{ *fakeSource }

func (c *cancelingSource) Recipe(ctx context.Context, o OrderedRecipe) (recipes.ImportRecipe, []recipes.ImportReviewItem, error) {
	r, rev, err := c.fakeSource.Recipe(ctx, o)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return recipes.ImportRecipe{}, nil, ctxErr
	}
	return r, rev, err
}

// The scheduled command keeps its old behavior: an interrupted run leaves the
// lease to expire (its run timeout is shorter than the lease on purpose).
func TestTheScheduledWorkerStillLeavesAnInterruptedLeaseToExpire(t *testing.T) {
	f := newWorkerFixture(t, orders(20))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.source.beforeRecipe = func(OrderedRecipe) { cancel() }
	f.service.sources[SourceHelloFresh] = &cancelingSource{fakeSource: f.source}

	if _, err := f.worker(t).Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if job := f.reload(t); job.Status != JobRunning || job.LeaseOwner != "worker-test" {
		t.Errorf("after an interrupted scheduled run: status %q lease %q", job.Status, job.LeaseOwner)
	}
}

// One goroutine is one polite conversation: however many households queue at
// once, the web process fetches one page at a time, and never runs two
// batches together.
func TestTheRunnerNeverFetchesForTwoBatchesAtOnce(t *testing.T) {
	store, publisher := &memoryStore{}, &fakePublisher{}
	src := &countingSource{fakeSource: &fakeSource{}}
	service := NewService(ServiceOptions{
		Store: store, Enabled: true, Sources: map[string]Source{SourceHelloFresh: src},
	})
	runner := NewRunner(RunnerOptions{
		Worker: WorkerOptions{Store: store, Service: service, Publisher: publisher},
		Poll:   5 * time.Millisecond,
	})
	service.SetKicker(runner)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	var jobs []Job
	for _, hh := range []string{hhAda, hhBob} {
		job, err := service.StartImport(ctx, ImportRequest{
			HouseholdID: hh, UserID: userAda, Source: SourceHelloFresh, Orders: orders(20),
		})
		if err != nil {
			t.Fatalf("StartImport(%s) error = %v", hh, err)
		}
		jobs = append(jobs, job)
		// Kicking the same job again, as a double tap would, is harmless.
		runner.Kick(job.ID)
	}
	waitFor(t, "both imports", func() bool {
		for _, j := range jobs {
			got, _ := store.GetJob(context.Background(), j.HouseholdID, j.ID)
			if got.Status != JobSucceeded {
				return false
			}
		}
		return true
	})
	if peak := src.peak.Load(); peak != 1 {
		t.Errorf("%d pages were being fetched at once, want 1", peak)
	}
	if n := len(src.fetchedIDs()); n != 40 {
		t.Errorf("fetched %d pages for two 20-recipe imports", n)
	}
}

// countingSource records how many Recipe calls overlap.
type countingSource struct {
	*fakeSource
	inFlight atomic.Int32
	peak     atomic.Int32
	mu       sync.Mutex
}

func (c *countingSource) Recipe(ctx context.Context, o OrderedRecipe) (recipes.ImportRecipe, []recipes.ImportReviewItem, error) {
	n := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	c.mu.Lock()
	if n > c.peak.Load() {
		c.peak.Store(n)
	}
	c.mu.Unlock()
	time.Sleep(200 * time.Microsecond)
	return c.fakeSource.Recipe(ctx, o)
}

// The web batches in numbers, like the scheduled budget in fetch_test.go:
// faster than one batch per Scheduler interval, still the same polite rate
// per request, and still far inside the lease.
func TestTheWebBatchesFinishALongHistoryInAboutAnHourAtThePoliteRate(t *testing.T) {
	average := DefaultMinInterval + time.Duration(float64(DefaultMinInterval)*DefaultJitter/2)

	perBatch := time.Duration(DefaultBatchSize) * average
	if perBatch > DefaultLease/2 {
		t.Errorf("a web batch spends %v fetching; keep it well inside the %v lease", perBatch, DefaultLease)
	}
	// The first checkpoint — the first visible movement — lands within a
	// minute of queueing.
	if first := time.Duration(importBatchSize) * average; first > time.Minute {
		t.Errorf("the first progress checkpoint takes %v", first)
	}

	const measured = 740
	batches := (measured + DefaultBatchSize - 1) / DefaultBatchSize
	if batches != 15 {
		t.Errorf("a %d-recipe history is %d web batches; the docs say 15", measured, batches)
	}
	// Each rest is the pause plus up to one poll before the runner notices.
	rest := DefaultBatchPause + DefaultRunnerPoll/2
	wall := time.Duration(batches)*perBatch + time.Duration(batches-1)*rest
	if wall < 40*time.Minute || wall > 75*time.Minute {
		t.Errorf("end to end is %v; the docs say about an hour", wall)
	}
	// Averaged over the whole import, pauses included, it is gentler than
	// the per-request floor, never faster.
	if avg := wall / measured; avg < DefaultMinInterval || avg < 3500*time.Millisecond {
		t.Errorf("one request every %v on average", avg)
	}
}
