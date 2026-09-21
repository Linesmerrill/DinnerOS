package mealkit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

// workerFixture is a linked household with one queued job, a scripted source,
// and a recording publisher.
type workerFixture struct {
	store     *memoryStore
	source    *fakeSource
	publisher *fakePublisher
	notifier  *fakeNotifier
	service   *Service
	job       Job
}

func newWorkerFixture(t *testing.T, orders []OrderedRecipe) *workerFixture {
	t.Helper()
	f := &workerFixture{
		store:     &memoryStore{},
		source:    &fakeSource{orders: orders, validToken: "access", refreshed: "access-2"},
		publisher: &fakePublisher{},
		notifier:  &fakeNotifier{},
	}
	f.source.signIn = Tokens{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
	f.service = NewService(ServiceOptions{
		Store: f.store, Cipher: testCipher(t),
		Sources:  map[string]Source{SourceHelloFresh: f.source},
		Notifier: f.notifier,
	})
	status, err := f.service.Link(context.Background(), LinkRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh,
		Email: "cook@example.com", Password: "pw", StartImport: true,
	})
	if err != nil {
		t.Fatalf("Link() error = %v", err)
	}
	f.job = *status.Job
	return f
}

func (f *workerFixture) worker(t *testing.T, mutate ...func(*WorkerOptions)) *Worker {
	t.Helper()
	opts := WorkerOptions{
		Store: f.store, Service: f.service, Publisher: f.publisher,
		Sources:  map[string]Source{SourceHelloFresh: f.source},
		Notifier: f.notifier, Owner: "worker-test",
		Random: func() float64 { return 0 },
	}
	for _, m := range mutate {
		m(&opts)
	}
	return NewWorker(opts)
}

func (f *workerFixture) reload(t *testing.T) Job {
	t.Helper()
	job, err := f.store.GetJob(context.Background(), hhAda, f.job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	return job
}

func orders(n int) []OrderedRecipe {
	out := make([]OrderedRecipe, 0, n)
	for i := range n {
		id := fmt.Sprintf("recipe-%02d", i)
		out = append(out, OrderedRecipe{
			SourceRecipeID: id, Name: "Dish " + id,
			URL: "https://www.hellofresh.com/recipes/" + id, Weeks: []string{"2026-W30"},
		})
	}
	return out
}

func TestWorkerImportsTheWholeOrderHistoryThroughTheImportPipeline(t *testing.T) {
	f := newWorkerFixture(t, orders(3))
	report, err := f.worker(t).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Claimed != 1 || report.Succeeded != 1 || report.Recipes != 3 {
		t.Fatalf("report = %+v", report)
	}
	if got := f.publisher.imported(); len(got) != 3 {
		t.Errorf("imported = %v", got)
	}
	// Every recipe went through the shared contract, not a second path.
	for _, file := range f.publisher.files {
		if file.Version != 1 || file.Source != SourceHelloFresh {
			t.Errorf("import file = version %d source %q", file.Version, file.Source)
		}
	}
	job := f.reload(t)
	if job.Status != JobSucceeded || job.Checkpoint.Phase != PhaseDone || job.RecipesDone() != 3 || job.Checkpoint.Imported != 3 {
		t.Errorf("job = %+v", job)
	}
	if types := f.notifier.types(); !slices.Contains(types, notifications.TypeRecipeImportFinished) {
		t.Errorf("notifications = %v, want a finished one", types)
	}
}

func TestWorkerResumesFromItsCheckpointInsteadOfRefetching(t *testing.T) {
	f := newWorkerFixture(t, orders(30))
	// A clock the test advances between runs, the way the scheduler does.
	clock := time.Now().UTC()
	capped := func(o *WorkerOptions) {
		o.RecipesPerRun = 10
		o.Now = func() time.Time { return clock }
	}
	// A small per-run cap makes the first run stop partway.
	report, err := f.worker(t, capped).Run(context.Background())
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if report.Requeued != 1 || report.Succeeded != 0 {
		t.Fatalf("first report = %+v", report)
	}
	job := f.reload(t)
	if job.Status != JobQueued || job.RecipesDone() != 10 {
		t.Fatalf("job after the capped run = status %q done %d", job.Status, job.RecipesDone())
	}
	// It put the job down for the next scheduled run rather than picking it
	// straight back up.
	if !job.AvailableAt.After(clock) {
		t.Errorf("a capped job is immediately claimable again (available %v)", job.AvailableAt)
	}
	// A run stopped by its own cap does not spend an attempt.
	if job.Attempts != 0 {
		t.Errorf("attempts after a capped run = %d, want 0", job.Attempts)
	}

	// The next scheduled runs finish it, and nothing is fetched twice.
	for range 2 {
		clock = clock.Add(10 * time.Minute)
		if _, err := f.worker(t, capped).Run(context.Background()); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	}
	job = f.reload(t)
	if job.Status != JobSucceeded || job.RecipesDone() != 30 {
		t.Fatalf("job = status %q done %d", job.Status, job.RecipesDone())
	}
	fetched := f.source.fetchedIDs()
	if len(fetched) != 30 || len(slices.Compact(slices.Sorted(slices.Values(fetched)))) != 30 {
		t.Errorf("fetched %d recipes, with repeats: %v", len(fetched), fetched)
	}
	// The order history was read once, in the first run.
	if got := f.reload(t).RecipesFound(); got != 30 {
		t.Errorf("recipes found = %d", got)
	}
}

func TestWorkerPausesForSignInWhenTheSessionCannotBeRefreshed(t *testing.T) {
	f := newWorkerFixture(t, orders(2))
	f.source.validToken = "something-else" // the stored token no longer works
	f.source.refreshed = ""                // and the refresh token is spent too

	report, err := f.worker(t).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Paused != 1 {
		t.Fatalf("report = %+v", report)
	}
	job := f.reload(t)
	if job.Status != JobPausedAuth || job.LastError == nil || job.LastError.Code != ErrCodeAuthExpired {
		t.Fatalf("job = %+v (error %+v)", job.Status, job.LastError)
	}
	if !strings.Contains(job.LastError.Message, "Sign in again") {
		t.Errorf("message = %q", job.LastError.Message)
	}
	link, _ := f.store.GetLink(context.Background(), hhAda, SourceHelloFresh)
	if link.Status != LinkNeedsReauth {
		t.Errorf("link status = %q, want %q", link.Status, LinkNeedsReauth)
	}
	if types := f.notifier.types(); !slices.Contains(types, notifications.TypeRecipeImportAttention) {
		t.Errorf("notifications = %v, want an attention one", types)
	}
	// A paused job is not claimable, so a later run does not burn attempts.
	if _, err := f.store.ClaimJob(context.Background(), "other", time.Now(), time.Now().Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Errorf("a paused job was claimable: %v", err)
	}
}

func TestWorkerRefreshesAnExpiredSessionAndCarriesOn(t *testing.T) {
	f := newWorkerFixture(t, orders(2))
	// The stored session is spent, but the refresh token still works.
	f.source.validToken = "access-2"
	f.source.refreshed = "access-2"

	report, err := f.worker(t).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Succeeded != 1 {
		t.Fatalf("report = %+v", report)
	}
	if f.source.refreshes == 0 {
		t.Error("the worker never refreshed the session")
	}
	// The new session was stored, encrypted, for the next run.
	link, _ := f.store.GetLink(context.Background(), hhAda, SourceHelloFresh)
	tokens, err := f.service.Tokens(link)
	if err != nil || tokens.AccessToken != "access-2" {
		t.Errorf("stored tokens = %+v, %v", tokens, err)
	}
	if link.Status != LinkActive {
		t.Errorf("link status = %q", link.Status)
	}
}

func TestWorkerStopsWhenTheAccountIsUnlinkedMidRun(t *testing.T) {
	f := newWorkerFixture(t, orders(30))
	unlinked := false
	f.source.beforeRecipe = func(OrderedRecipe) {
		// Unlink once, partway through the run, exactly as the member would.
		if !unlinked {
			unlinked = true
			if _, err := f.service.Unlink(context.Background(), hhAda, SourceHelloFresh); err != nil {
				t.Errorf("Unlink() error = %v", err)
			}
		}
	}

	report, err := f.worker(t, func(o *WorkerOptions) { o.RecipesPerRun = 30 }).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Gone != 1 {
		t.Fatalf("report = %+v, want one job gone", report)
	}
	job := f.reload(t)
	if job.Status != JobCanceled {
		t.Errorf("job = %q, want %q", job.Status, JobCanceled)
	}
	// It stopped at the first checkpoint after the unlink rather than
	// importing the whole history.
	if got := len(f.publisher.imported()); got >= 30 {
		t.Errorf("imported %d recipes after the unlink", got)
	}
	if _, err := f.store.GetLink(context.Background(), hhAda, SourceHelloFresh); !errors.Is(err, ErrNotFound) {
		t.Errorf("the tokens survived the unlink")
	}
}

func TestWorkerFailsCleanlyWhenTheSourceLayoutChanged(t *testing.T) {
	f := newWorkerFixture(t, orders(3))
	layoutChange := &ParseError{
		Subject: "the order history",
		Detail:  "HelloFresh answered with something this build cannot read",
	}
	f.source.ordersErr = layoutChange

	report, err := f.worker(t).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Dead != 1 || report.Requeued != 0 {
		t.Fatalf("report = %+v; a layout change must not be retried", report)
	}
	job := f.reload(t)
	if job.Status != JobDead || job.LastError == nil || job.LastError.Code != ErrCodeParse {
		t.Fatalf("job = %q, error = %+v", job.Status, job.LastError)
	}
	if !strings.Contains(job.LastError.Message, "Nothing was changed in your recipes") {
		t.Errorf("message = %q", job.LastError.Message)
	}
	if got := f.publisher.imported(); len(got) != 0 {
		t.Errorf("a failed parse still wrote %v to the library", got)
	}
	if types := f.notifier.types(); !slices.Contains(types, notifications.TypeRecipeImportAttention) {
		t.Errorf("notifications = %v", types)
	}
}

func TestWorkerRecordsOneUnreadableRecipeAndFinishesTheRest(t *testing.T) {
	f := newWorkerFixture(t, orders(3))
	f.source.recipeErr = map[string]error{
		"recipe-01": &ParseError{Subject: "recipe recipe-01", Detail: "the page carried no recipe"},
	}

	report, err := f.worker(t, func(o *WorkerOptions) { o.RecipesPerRun = 3 }).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Succeeded != 1 || report.Failures != 1 {
		t.Fatalf("report = %+v", report)
	}
	job := f.reload(t)
	if job.Status != JobSucceeded || len(job.Checkpoint.Failures) != 1 {
		t.Fatalf("job = %q failures %+v", job.Status, job.Checkpoint.Failures)
	}
	if got := job.Checkpoint.Failures[0]; got.SourceRecipeID != "recipe-01" || got.Reason == "" {
		t.Errorf("failure = %+v", got)
	}
	if got := f.publisher.imported(); len(got) != 2 {
		t.Errorf("imported = %v, want the two readable recipes", got)
	}
}

func TestWorkerRetriesATransientFailureThenDeadLetters(t *testing.T) {
	f := newWorkerFixture(t, orders(1))
	f.source.recipeErr = map[string]error{"recipe-00": errors.New("connection reset")}

	now := time.Now().UTC()
	for attempt := 1; attempt <= 5; attempt++ {
		clock := now
		w := f.worker(t, func(o *WorkerOptions) {
			o.Now = func() time.Time { return clock }
			o.Owner = fmt.Sprintf("worker-%d", attempt)
		})
		if _, err := w.Run(context.Background()); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		job := f.reload(t)
		if attempt < 5 {
			if job.Status != JobQueued || !job.AvailableAt.After(clock) {
				t.Fatalf("attempt %d: job = %q available %v", attempt, job.Status, job.AvailableAt)
			}
			now = job.AvailableAt
			continue
		}
		if job.Status != JobDead || job.LastError == nil || job.LastError.Code != ErrCodeNetwork {
			t.Fatalf("attempt %d: job = %q error %+v", attempt, job.Status, job.LastError)
		}
	}
}

func TestWorkerRecordsRecipesTheImportPipelineRejected(t *testing.T) {
	f := newWorkerFixture(t, orders(2))
	f.publisher.reject = map[string]string{"recipe-01": "name is required"}

	if _, err := f.worker(t).Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	job := f.reload(t)
	if job.Status != JobSucceeded || len(job.Checkpoint.Failures) != 1 {
		t.Fatalf("job = %q failures %+v", job.Status, job.Checkpoint.Failures)
	}
	if job.Checkpoint.Failures[0].Reason != "name is required" {
		t.Errorf("failure reason = %q; the pipeline's own reason should reach the member", job.Checkpoint.Failures[0].Reason)
	}
	if job.Checkpoint.Imported != 1 {
		t.Errorf("imported = %d, want only the accepted recipe", job.Checkpoint.Imported)
	}
}

func TestWorkerStopsOutrightWhenTheServiceRefusesUs(t *testing.T) {
	f := newWorkerFixture(t, orders(3))
	f.source.ordersErr = ErrBlocked

	report, err := f.worker(t).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Dead != 1 || report.Requeued != 0 {
		t.Fatalf("report = %+v; a refusal must not be retried", report)
	}
	if job := f.reload(t); job.LastError == nil || job.LastError.Code != ErrCodeBlocked {
		t.Errorf("error = %+v", job.LastError)
	}
}

func TestWorkerTakesOverAJobWhoseLeaseExpired(t *testing.T) {
	f := newWorkerFixture(t, orders(2))
	// A worker that died mid-run: running, with a lease in the past.
	f.store.jobs[0].Status = JobRunning
	f.store.jobs[0].LeaseOwner = "dead-worker"
	f.store.jobs[0].LeaseExpiresAt = time.Now().Add(-time.Hour)
	f.store.jobs[0].Checkpoint = Checkpoint{Phase: PhaseRecipes, Orders: orders(2), Done: []string{"recipe-00"}}

	report, err := f.worker(t).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Claimed != 1 || report.Succeeded != 1 {
		t.Fatalf("report = %+v", report)
	}
	// Only the recipe the dead run had not finished was fetched again.
	if got := f.source.fetchedIDs(); len(got) != 1 || got[0] != "recipe-01" {
		t.Errorf("fetched = %v, want only the unfinished recipe", got)
	}
}
