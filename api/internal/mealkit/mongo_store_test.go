package mealkit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newMongoStore(t *testing.T) (*MongoStore, context.Context) {
	t.Helper()
	client := mongotest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := client.EnsureIndexes(ctx, Indexes()...); err != nil {
		t.Fatalf("EnsureIndexes() error = %v", err)
	}
	return NewMongoStore(client.Database()), ctx
}

// seedJob inserts a queued run for householdID. There is nothing else to seed:
// this package stores jobs and nothing else.
func seedJob(t *testing.T, s *MongoStore, ctx context.Context, householdID, userID string, availableAt time.Time) Job {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	job, err := s.InsertJob(ctx, Job{
		HouseholdID: householdID, UserID: userID, Source: SourceHelloFresh,
		Status: JobQueued, MaxAttempts: DefaultMaxAttempts, AvailableAt: availableAt,
		Checkpoint: Checkpoint{Phase: PhaseRecipes}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("InsertJob() error = %v", err)
	}
	return job
}

func TestIntegrationClaimJobIsExactlyOnceUnderConcurrency(t *testing.T) {
	store, ctx := newMongoStore(t)

	const jobs, workers = 8, 12
	now := time.Now().UTC().Truncate(time.Millisecond)
	for range jobs {
		seedJob(t, store, ctx, hhAda, userAda, now.Add(-time.Minute))
	}

	var (
		mu      sync.Mutex
		claimed = map[string]string{} // job ID -> the worker that claimed it
		doubles []string
		start   = make(chan struct{})
		wg      sync.WaitGroup
	)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			owner := "worker-" + string(rune('a'+w))
			<-start
			for {
				job, err := store.ClaimJob(ctx, owner, now, now.Add(time.Minute))
				if errors.Is(err, ErrNotFound) {
					return
				}
				if err != nil {
					t.Errorf("ClaimJob() error = %v", err)
					return
				}
				mu.Lock()
				if other, dup := claimed[job.ID]; dup {
					doubles = append(doubles, job.ID+" claimed by "+other+" and "+owner)
				}
				claimed[job.ID] = owner
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(doubles) > 0 {
		t.Fatalf("a job was claimed twice: %v", doubles)
	}
	if len(claimed) != jobs {
		t.Fatalf("claimed %d of %d jobs", len(claimed), jobs)
	}
	for id, owner := range claimed {
		job, err := store.GetJob(ctx, hhAda, id)
		if err != nil {
			t.Fatalf("GetJob() error = %v", err)
		}
		if job.Status != JobRunning || job.LeaseOwner != owner || job.Attempts != 1 {
			t.Errorf("job %s = status %q owner %q attempts %d", id, job.Status, job.LeaseOwner, job.Attempts)
		}
	}
}

func TestIntegrationClaimRespectsBackoffAndTakesOverAnExpiredLease(t *testing.T) {
	store, ctx := newMongoStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	// A job backing off is not claimable until its time comes.
	later := seedJob(t, store, ctx, hhAda, userAda, now.Add(time.Hour))
	if _, err := store.ClaimJob(ctx, "w1", now, now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a backing-off job was claimed: %v", err)
	}
	if _, err := store.ClaimJob(ctx, "w1", now.Add(2*time.Hour), now.Add(3*time.Hour)); err != nil {
		t.Fatalf("ClaimJob() after the backoff error = %v", err)
	}

	// Its lease is held: another worker gets nothing until it expires.
	if _, err := store.ClaimJob(ctx, "w2", now.Add(2*time.Hour), now.Add(3*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("a leased job was claimed by a second worker: %v", err)
	}
	taken, err := store.ClaimJob(ctx, "w2", now.Add(4*time.Hour), now.Add(5*time.Hour))
	if err != nil || taken.ID != later.ID || taken.LeaseOwner != "w2" || taken.Attempts != 2 {
		t.Fatalf("lease takeover = %+v, %v", taken, err)
	}
	// The worker that lost the lease can no longer write to the job.
	if err := store.SaveCheckpoint(ctx, later.ID, "w1", Checkpoint{Phase: PhaseRecipes}, now); !errors.Is(err, ErrJobGone) {
		t.Errorf("the displaced worker could still write: %v", err)
	}
}

func TestIntegrationJobWritesAreConditionalOnTheLease(t *testing.T) {
	store, ctx := newMongoStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	seedJob(t, store, ctx, hhAda, userAda, now.Add(-time.Minute))

	job, err := store.ClaimJob(ctx, "w1", now, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimJob() error = %v", err)
	}
	cp := Checkpoint{
		Phase:  PhaseRecipes,
		Orders: []OrderedRecipe{{SourceRecipeID: "r1", Name: "Dish", URL: "https://www.hellofresh.com/recipes/r1", Weeks: []string{"2026-W30"}}},
		Done:   []string{"r1"}, Imported: 1,
		Failures: []FailedRecipe{{SourceRecipeID: "r2", Name: "Other", Reason: "the page carried no recipe"}},
	}
	if err := store.SaveCheckpoint(ctx, job.ID, "w1", cp, now); err != nil {
		t.Fatalf("SaveCheckpoint() error = %v", err)
	}
	stored, _ := store.GetJob(ctx, hhAda, job.ID)
	if stored.RecipesFound() != 1 || stored.RecipesDone() != 1 || len(stored.Checkpoint.Failures) != 1 {
		t.Fatalf("checkpoint = %+v", stored.Checkpoint)
	}
	if got := stored.Checkpoint.Remaining(); len(got) != 0 {
		t.Errorf("Remaining() = %+v", got)
	}

	// The cap path requeues without spending the attempt.
	if err := store.RequeueJob(ctx, job.ID, "w1", now.Add(time.Minute), false, nil, now); err != nil {
		t.Fatalf("RequeueJob() error = %v", err)
	}
	stored, _ = store.GetJob(ctx, hhAda, job.ID)
	if stored.Status != JobQueued || stored.Attempts != 0 || stored.LeaseOwner != "" {
		t.Fatalf("requeued job = %+v", stored)
	}

	// An unlink cancels, and the worker's next write fails.
	job, _ = store.ClaimJob(ctx, "w1", now.Add(2*time.Minute), now.Add(10*time.Minute))
	canceled, err := store.CancelJobs(ctx, hhAda, SourceHelloFresh, "the meal-kit account was unlinked", now)
	if err != nil || canceled != 1 {
		t.Fatalf("CancelJobs() = %d, %v", canceled, err)
	}
	for name, write := range map[string]func() error{
		"checkpoint": func() error { return store.SaveCheckpoint(ctx, job.ID, "w1", cp, now) },
		"lease":      func() error { return store.ExtendLease(ctx, job.ID, "w1", now.Add(time.Hour), now) },
		"finish":     func() error { return store.FinishJob(ctx, job.ID, "w1", JobSucceeded, cp, nil, now) },
		"requeue":    func() error { return store.RequeueJob(ctx, job.ID, "w1", now, true, nil, now) },
	} {
		if err := write(); !errors.Is(err, ErrJobGone) {
			t.Errorf("%s after cancel = %v, want ErrJobGone", name, err)
		}
	}
	stored, _ = store.GetJob(ctx, hhAda, job.ID)
	if stored.Status != JobCanceled || stored.FinishedAt.IsZero() {
		t.Errorf("canceled job = %+v", stored)
	}
}

func TestIntegrationJobLookupsAreScopedToTheHousehold(t *testing.T) {
	store, ctx := newMongoStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	adaJob := seedJob(t, store, ctx, hhAda, userAda, now)
	seedJob(t, store, ctx, hhBob, userBob, now)

	if _, err := store.GetJob(ctx, hhBob, adaJob.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household read the job: %v", err)
	}
	latest, err := store.LatestJob(ctx, hhAda, SourceHelloFresh)
	if err != nil || latest.ID != adaJob.ID {
		t.Fatalf("LatestJob() = %+v, %v", latest, err)
	}
	if list, err := store.ListJobs(ctx, hhAda, 20); err != nil || len(list) != 1 {
		t.Errorf("ListJobs() = %d jobs, %v", len(list), err)
	}

	// Only a claimed job can be finished, and the reason is stored with it.
	if err := store.FinishJob(ctx, adaJob.ID, "", JobDead, Checkpoint{}, nil, now); !errors.Is(err, ErrJobGone) {
		t.Errorf("an unclaimed job was finished: %v", err)
	}
	claimed, _ := store.ClaimJob(ctx, "w1", now, now.Add(time.Minute))
	if err := store.FinishJob(ctx, claimed.ID, "w1", JobDead, Checkpoint{Phase: PhaseRecipes}, &JobError{Code: ErrCodeBlocked, Message: "they refused us", At: now}, now); err != nil {
		t.Fatalf("FinishJob() error = %v", err)
	}
	stored, _ := store.GetJob(ctx, claimed.HouseholdID, claimed.ID)
	if stored.Status != JobDead || stored.LastError == nil || stored.LastError.Code != ErrCodeBlocked {
		t.Errorf("finished job = %+v", stored)
	}
}

func TestIntegrationPurgeRemovesAHouseholdsJobs(t *testing.T) {
	store, ctx := newMongoStore(t)
	seedJob(t, store, ctx, hhAda, userAda, time.Now().UTC())

	if err := store.PurgeHousehold(ctx, hhAda); err != nil {
		t.Fatalf("PurgeHousehold() error = %v", err)
	}
	if jobs, _ := store.ListJobs(ctx, hhAda, 20); len(jobs) != 0 {
		t.Errorf("jobs survived the purge: %+v", jobs)
	}
	// The purge is idempotent and safe with a malformed ID.
	if err := errors.Join(store.PurgeHousehold(ctx, hhAda), store.PurgeHousehold(ctx, "not-an-id")); err != nil {
		t.Errorf("repeat purge error = %v", err)
	}
}

// The cursor is what makes a four-year history finishable: 740 recipes in 40
// pages was one measured harvest, and it stopped on the cap. What survives
// that run is the earliest week it reached.
func TestIntegrationTheCursorSurvivesAndResumesTheHarvest(t *testing.T) {
	store, ctx := newMongoStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, err := store.GetCursor(ctx, hhAda, SourceHelloFresh); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a household that never harvested = %v, want ErrNotFound", err)
	}

	capped := Cursor{HouseholdID: hhAda, Source: SourceHelloFresh}.Merge(HarvestReport{
		EarliestWeek: "2023-W30", LatestWeek: "2026-W38", Pages: 40, Weeks: 160, Stopped: HarvestStopCap,
	}, now)
	if err := store.SaveCursor(ctx, capped); err != nil {
		t.Fatalf("SaveCursor() error = %v", err)
	}
	stored, err := store.GetCursor(ctx, hhAda, SourceHelloFresh)
	if err != nil {
		t.Fatalf("GetCursor() error = %v", err)
	}
	if stored.ResumeFrom() != "2023-W30" || stored.Complete || !stored.MoreToFetch() {
		t.Fatalf("stored cursor = %+v", stored)
	}

	// The second harvest resumes below that floor and reaches the start of
	// the history. Saving again upserts the one document rather than adding
	// a second, which the unique index would refuse anyway.
	finished := stored.Merge(HarvestReport{
		EarliestWeek: "2022-W05", LatestWeek: "2023-W29", Stopped: HarvestStopEnd,
	}, now)
	if err := store.SaveCursor(ctx, finished); err != nil {
		t.Fatalf("second SaveCursor() error = %v", err)
	}
	stored, err = store.GetCursor(ctx, hhAda, SourceHelloFresh)
	if err != nil {
		t.Fatalf("GetCursor() error = %v", err)
	}
	if !stored.Complete || stored.EarliestWeek != "2022-W05" || stored.LatestWeek != "2026-W38" {
		t.Fatalf("stored cursor after finishing = %+v", stored)
	}

	// Another household's cursor is its own.
	if _, err := store.GetCursor(ctx, hhBob, SourceHelloFresh); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household read Ada's cursor: %v", err)
	}

	// A refusal is recorded on the cursor and clears with the next save.
	if err := store.MarkCursorBlocked(ctx, hhAda, SourceHelloFresh, now); err != nil {
		t.Fatalf("MarkCursorBlocked() error = %v", err)
	}
	stored, _ = store.GetCursor(ctx, hhAda, SourceHelloFresh)
	if !stored.Blocked(now) || !stored.Complete {
		t.Errorf("cursor after a refusal = %+v", stored)
	}
	stored.BlockedAt = time.Time{}
	if err := store.SaveCursor(ctx, stored); err != nil {
		t.Fatalf("SaveCursor() error = %v", err)
	}
	stored, _ = store.GetCursor(ctx, hhAda, SourceHelloFresh)
	if stored.Blocked(now) {
		t.Errorf("the refusal outlived the cooldown: %+v", stored)
	}

	// A harvest report is stored with the job that carried it, so the member
	// can be told there is more history.
	job := seedJob(t, store, ctx, hhAda, userAda, now)
	if job.Harvest.Stopped != "" {
		t.Errorf("a seeded job invented a harvest report: %+v", job.Harvest)
	}

	// Deleting the household takes the cursor with it.
	if err := store.PurgeHousehold(ctx, hhAda); err != nil {
		t.Fatalf("PurgeHousehold() error = %v", err)
	}
	if _, err := store.GetCursor(ctx, hhAda, SourceHelloFresh); !errors.Is(err, ErrNotFound) {
		t.Errorf("the cursor survived the purge: %v", err)
	}
}

// One household, one import at a time — whichever source asks.
func TestIntegrationAnyActiveJobIsTheSingleFlightCheck(t *testing.T) {
	store, ctx := newMongoStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, err := store.AnyActiveJob(ctx, hhAda); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AnyActiveJob() with nothing queued = %v", err)
	}
	job := seedJob(t, store, ctx, hhAda, userAda, now)
	active, err := store.AnyActiveJob(ctx, hhAda)
	if err != nil || active.ID != job.ID {
		t.Fatalf("AnyActiveJob() = %+v, %v", active, err)
	}
	if _, err := store.AnyActiveJob(ctx, hhBob); !errors.Is(err, ErrNotFound) {
		t.Errorf("Ada's run blocked Bob's household: %v", err)
	}
	if _, err := store.CancelJobs(ctx, hhAda, SourceHelloFresh, "stopped", now); err != nil {
		t.Fatalf("CancelJobs() error = %v", err)
	}
	if _, err := store.AnyActiveJob(ctx, hhAda); !errors.Is(err, ErrNotFound) {
		t.Errorf("a canceled run still counts as active: %v", err)
	}
}
