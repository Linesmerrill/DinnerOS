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

func seedLink(t *testing.T, s *MongoStore, ctx context.Context, householdID, userID string) Link {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	link, err := s.UpsertLink(ctx, Link{
		HouseholdID: householdID, UserID: userID, Source: SourceHelloFresh, Status: LinkActive,
		AccountLabel: "HelloFresh account", Secret: Envelope{KeyID: "k1", Key: []byte("wrapped"), Ciphertext: []byte("sealed")},
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("UpsertLink() error = %v", err)
	}
	return link
}

func seedJob(t *testing.T, s *MongoStore, ctx context.Context, link Link, availableAt time.Time) Job {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	job, err := s.InsertJob(ctx, Job{
		HouseholdID: link.HouseholdID, UserID: link.UserID, LinkID: link.ID, Source: link.Source,
		Status: JobQueued, MaxAttempts: DefaultMaxAttempts, AvailableAt: availableAt,
		Checkpoint: Checkpoint{Phase: PhaseOrders}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("InsertJob() error = %v", err)
	}
	return job
}

func TestIntegrationMongoLinkRoundTripsAndReplaces(t *testing.T) {
	store, ctx := newMongoStore(t)
	link := seedLink(t, store, ctx, hhAda, userAda)

	got, err := store.GetLink(ctx, hhAda, SourceHelloFresh)
	if err != nil {
		t.Fatalf("GetLink() error = %v", err)
	}
	if got.ID != link.ID || string(got.Secret.Ciphertext) != "sealed" || got.Status != LinkActive {
		t.Fatalf("link = %+v", got)
	}
	if _, err := store.GetLink(ctx, hhBob, SourceHelloFresh); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household's link = %v", err)
	}

	// Re-linking replaces the tokens and keeps the original CreatedAt.
	now := time.Now().UTC().Truncate(time.Millisecond)
	again, err := store.UpsertLink(ctx, Link{
		HouseholdID: hhAda, UserID: userBob, Source: SourceHelloFresh, Status: LinkActive,
		Secret: Envelope{KeyID: "k1", Ciphertext: []byte("sealed-2")}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || again.ID != link.ID || !again.CreatedAt.Equal(link.CreatedAt) {
		t.Fatalf("re-link = %+v, %v", again, err)
	}
	if string(again.Secret.Ciphertext) != "sealed-2" || !again.ExpiresAt.IsZero() {
		t.Errorf("re-linked secret = %+v", again.Secret)
	}

	if err := store.SetLinkStatus(ctx, link.ID, LinkNeedsReauth, now); err != nil {
		t.Fatalf("SetLinkStatus() error = %v", err)
	}
	if err := store.SaveLinkTokens(ctx, link.ID, Envelope{KeyID: "k1", Ciphertext: []byte("sealed-3")}, now.Add(time.Hour), now); err != nil {
		t.Fatalf("SaveLinkTokens() error = %v", err)
	}
	got, _ = store.GetLink(ctx, hhAda, SourceHelloFresh)
	if got.Status != LinkActive || string(got.Secret.Ciphertext) != "sealed-3" {
		t.Errorf("after a refresh = %+v", got)
	}

	if err := store.DeleteLink(ctx, hhAda, SourceHelloFresh); err != nil {
		t.Fatalf("DeleteLink() error = %v", err)
	}
	if _, err := store.GetLink(ctx, hhAda, SourceHelloFresh); !errors.Is(err, ErrNotFound) {
		t.Errorf("the link survived deletion: %v", err)
	}
	// Deleting twice is fine.
	if err := store.DeleteLink(ctx, hhAda, SourceHelloFresh); err != nil {
		t.Errorf("second DeleteLink() error = %v", err)
	}
}

// TestIntegrationClaimJobIsExactlyOnceUnderConcurrency is the property the
// whole queue rests on: however many workers race, one job goes to one worker.
func TestIntegrationClaimJobIsExactlyOnceUnderConcurrency(t *testing.T) {
	store, ctx := newMongoStore(t)
	link := seedLink(t, store, ctx, hhAda, userAda)

	const jobs, workers = 8, 12
	now := time.Now().UTC().Truncate(time.Millisecond)
	for range jobs {
		seedJob(t, store, ctx, link, now.Add(-time.Minute))
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
	link := seedLink(t, store, ctx, hhAda, userAda)
	now := time.Now().UTC().Truncate(time.Millisecond)

	// A job backing off is not claimable until its time comes.
	later := seedJob(t, store, ctx, link, now.Add(time.Hour))
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
	link := seedLink(t, store, ctx, hhAda, userAda)
	now := time.Now().UTC().Truncate(time.Millisecond)
	seedJob(t, store, ctx, link, now.Add(-time.Minute))

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
	ada := seedLink(t, store, ctx, hhAda, userAda)
	bob := seedLink(t, store, ctx, hhBob, userBob)
	now := time.Now().UTC().Truncate(time.Millisecond)
	adaJob := seedJob(t, store, ctx, ada, now)
	seedJob(t, store, ctx, bob, now)

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

	// Paused jobs resume on a re-link, and only for their own household.
	if err := store.FinishJob(ctx, adaJob.ID, "", JobPausedAuth, Checkpoint{}, nil, now); !errors.Is(err, ErrJobGone) {
		t.Errorf("an unclaimed job was finished: %v", err)
	}
	claimed, _ := store.ClaimJob(ctx, "w1", now, now.Add(time.Minute))
	if err := store.FinishJob(ctx, claimed.ID, "w1", JobPausedAuth, Checkpoint{Phase: PhaseRecipes}, &JobError{Code: ErrCodeAuthExpired, Message: "sign in again", At: now}, now); err != nil {
		t.Fatalf("FinishJob() error = %v", err)
	}
	resumed, err := store.ResumePausedJobs(ctx, claimed.HouseholdID, SourceHelloFresh, now)
	if err != nil || resumed != 1 {
		t.Fatalf("ResumePausedJobs() = %d, %v", resumed, err)
	}
	stored, _ := store.GetJob(ctx, claimed.HouseholdID, claimed.ID)
	if stored.Status != JobQueued || stored.LastError == nil || stored.LastError.Code != ErrCodeAuthExpired {
		t.Errorf("resumed job = %+v", stored)
	}
}

func TestIntegrationPurgeRemovesLinksAndJobs(t *testing.T) {
	store, ctx := newMongoStore(t)
	ada := seedLink(t, store, ctx, hhAda, userAda)
	seedJob(t, store, ctx, ada, time.Now().UTC())

	if err := store.PurgeUser(ctx, userAda); err != nil {
		t.Fatalf("PurgeUser() error = %v", err)
	}
	if _, err := store.GetLink(ctx, hhAda, SourceHelloFresh); !errors.Is(err, ErrNotFound) {
		t.Errorf("the user's tokens survived their account deletion: %v", err)
	}
	if err := store.PurgeHousehold(ctx, hhAda); err != nil {
		t.Fatalf("PurgeHousehold() error = %v", err)
	}
	if jobs, _ := store.ListJobs(ctx, hhAda, 20); len(jobs) != 0 {
		t.Errorf("jobs survived the purge: %+v", jobs)
	}
	// Both purges are idempotent and safe with a malformed ID.
	if err := errors.Join(store.PurgeHousehold(ctx, hhAda), store.PurgeUser(ctx, "not-an-id")); err != nil {
		t.Errorf("repeat purge error = %v", err)
	}
}
