package mealkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// importBatchSize is how many fetched recipes are handed to the import
// pipeline at once. Small batches keep the checkpoint close behind the work,
// so a dyno killed mid-run re-fetches at most this many recipes.
const importBatchSize = 10

// WorkerOptions configures a Worker.
type WorkerOptions struct {
	Store Store
	// Service resolves the source for a job.
	Service *Service
	// Publisher is the seam into the recipe library (publisher.go).
	Publisher RecipePublisher
	// Sources are the meal-kit clients, keyed by name. One Source is one
	// polite conversation, so a Worker uses at most one at a time.
	Sources  map[string]Source
	Notifier Notifier
	Logger   *slog.Logger
	// Observer, when set, hears about progress and the outcome (observer.go).
	Observer ProgressObserver

	// Owner identifies this worker run in job leases. Empty means a random
	// one is generated.
	Owner string
	// Lease is the visibility timeout. Default DefaultLease.
	Lease time.Duration
	// RecipesPerRun caps source requests for recipes in one run. Default
	// DefaultRecipesPerRun.
	RecipesPerRun int
	// MaxJobs caps how many jobs one run claims. Default 5.
	MaxJobs int
	// CapDelay is how long a job that stopped on its own per-run cap waits
	// before it is claimable again. It keeps one run from immediately
	// re-claiming the job it just put down, so a long history really does
	// spread across scheduled runs. Default DefaultCapDelay.
	CapDelay time.Duration
	// BackoffBase and BackoffMax shape the retry delay.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// ReleaseOnCancel hands a job back to the queue, due at once, when ctx is
	// canceled mid-run, instead of leaving its lease to expire. The web
	// process sets it (runner.go): a dyno restart or an Eco dyno going to
	// sleep cancels the runner, and releasing means the next worker resumes
	// straight away rather than after DefaultLease of a bar that doesn't move.
	// The scheduled command leaves it off, so its run timeout keeps the
	// lease-expiry behavior the rest of this file documents. A process that
	// dies without being told (SIGKILL, a crash) still falls back on the lease.
	ReleaseOnCancel bool

	Now    func() time.Time
	Random func() float64
}

// Worker claims import jobs and runs them. One Worker is one run of
// cmd/importmealkit: it drains what it can inside its budget, checkpoints,
// and exits.
type Worker struct {
	opts WorkerOptions
}

// NewWorker returns a Worker with the defaults filled in.
func NewWorker(opts WorkerOptions) *Worker {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Lease <= 0 {
		opts.Lease = DefaultLease
	}
	if opts.RecipesPerRun <= 0 {
		opts.RecipesPerRun = DefaultRecipesPerRun
	}
	if opts.MaxJobs <= 0 {
		opts.MaxJobs = 5
	}
	if opts.CapDelay <= 0 {
		opts.CapDelay = DefaultCapDelay
	}
	if opts.BackoffBase <= 0 {
		opts.BackoffBase = DefaultBackoffBase
	}
	if opts.BackoffMax <= 0 {
		opts.BackoffMax = DefaultBackoffMax
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Random == nil {
		opts.Random = rand.Float64
	}
	if opts.Owner == "" {
		opts.Owner = fmt.Sprintf("worker-%d-%04d", time.Now().UnixNano(), int(opts.Random()*10000))
	}
	return &Worker{opts: opts}
}

// Report summarizes a run, for the command's one log line.
type Report struct {
	// Claimed is how many jobs this run took.
	Claimed int
	// Succeeded, Requeued, Dead, and Gone are their outcomes. Gone counts
	// jobs that were canceled or taken over while running, which is the
	// expected shape of a member stopping the import mid-run.
	Succeeded int
	Requeued  int
	Dead      int
	Gone      int
	// Recipes is how many recipes reached the library across every job.
	Recipes int
	// Failures is how many individual recipes could not be imported.
	Failures int
}

// Run claims and runs jobs until there is nothing runnable, the job budget is
// spent, or ctx is done. It returns the report and the first error that was
// not a job's own failure.
func (w *Worker) Run(ctx context.Context) (Report, error) {
	var report Report
	for report.Claimed < w.opts.MaxJobs {
		if err := ctx.Err(); err != nil {
			return report, nil
		}
		now := w.now()
		job, err := w.opts.Store.ClaimJob(ctx, w.opts.Owner, now, now.Add(w.opts.Lease))
		if errors.Is(err, ErrNotFound) {
			return report, nil
		}
		if err != nil {
			return report, fmt.Errorf("claim import job: %w", err)
		}
		report.Claimed++
		w.runJob(ctx, job, &report)
	}
	return report, nil
}

// RunJob claims one named job, if it is runnable, and runs it to its next
// resting point: finished, failed, or stopped on RecipesPerRun. It uses the
// same atomic claim as Run with the job's ID added (Store.ClaimJobByID), so a
// scheduled worker racing for the same job gets it or doesn't, never both.
// Claimed is 0 when the job was not runnable — someone else holds its lease,
// it is waiting out a pause or a backoff, or it has finished.
func (w *Worker) RunJob(ctx context.Context, id string) (Report, error) {
	var report Report
	if err := ctx.Err(); err != nil {
		return report, nil
	}
	now := w.now()
	job, err := w.opts.Store.ClaimJobByID(ctx, id, w.opts.Owner, now, now.Add(w.opts.Lease))
	if errors.Is(err, ErrNotFound) {
		return report, nil
	}
	if err != nil {
		return report, fmt.Errorf("claim import job: %w", err)
	}
	report.Claimed++
	w.runJob(ctx, job, &report)
	return report, nil
}

// runJob executes one claimed job to its next resting point. It never returns
// an error: every outcome is recorded on the job, which is what the member
// sees.
func (w *Worker) runJob(ctx context.Context, job Job, report *Report) {
	log := w.opts.Logger.With("jobId", job.ID, "householdId", job.HouseholdID, "source", job.Source, "attempt", job.Attempts)

	outcome, err := w.execute(ctx, &job, log)
	switch {
	case errors.Is(err, ErrJobGone):
		// The member unlinked (which cancels), or another worker took the
		// lease over. Either way this run stops touching the job.
		report.Gone++
		log.InfoContext(ctx, "meal-kit import job is no longer ours; stopping")
		return
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The dyno is going away. By default the lease is left to expire and
		// the next run resumes from the checkpoint; the web process hands the
		// job straight back instead (ReleaseOnCancel).
		if w.opts.ReleaseOnCancel {
			w.release(ctx, job, log)
			return
		}
		log.InfoContext(ctx, "meal-kit import interrupted; the lease will expire and the next run resumes")
		return
	}

	report.Recipes += job.Checkpoint.Imported + job.Checkpoint.Updated
	report.Failures += len(job.Checkpoint.Failures)

	switch outcome {
	case outcomeDone:
		report.Succeeded++
		w.finish(ctx, job, JobSucceeded, nil, log)
		w.notifyFinished(ctx, job)
		observeEnd(ctx, w.opts.Observer, job, JobSucceeded)
	case outcomeCapped:
		report.Requeued++
		w.requeue(ctx, job, false, nil, log)
		observeProgress(ctx, w.opts.Observer, job, JobQueued)
	case outcomeFailed:
		w.fail(ctx, job, err, report, log)
	}
}

type outcome int

const (
	outcomeDone outcome = iota
	outcomeCapped
	outcomeFailed
)

// execute runs the job to its next resting point, saving a checkpoint after
// each unit of work.
//
// There is no order-history phase and no session: the history arrived with the
// request that queued this job, harvested in the member's own browser, and
// every page below is public.
func (w *Worker) execute(ctx context.Context, job *Job, log *slog.Logger) (outcome, error) {
	src, err := w.opts.Service.Source(job.Source)
	if err != nil {
		return outcomeFailed, &ParseError{Subject: "the import", Detail: "this server cannot import from " + job.Source}
	}
	if job.Checkpoint.Phase == "" {
		job.Checkpoint.Phase = PhaseRecipes
	}

	fetched := 0
	batch := make([]recipes.ImportRecipe, 0, importBatchSize)
	var reviews []recipes.ImportReviewItem
	for _, o := range job.Checkpoint.Remaining() {
		if err := ctx.Err(); err != nil {
			return outcomeFailed, err
		}
		if fetched >= w.opts.RecipesPerRun {
			// The per-run cap. Flush what we have, then let the next
			// scheduled run continue: a long history spreads out instead of
			// becoming one long hammering session.
			if err := w.flush(ctx, job, &batch, &reviews); err != nil {
				return outcomeFailed, err
			}
			log.InfoContext(ctx, "meal-kit import reached its per-run cap", "cap", w.opts.RecipesPerRun, "done", len(job.Checkpoint.Done))
			return outcomeCapped, nil
		}

		recipe, review, err := src.Recipe(ctx, o)
		fetched++
		switch {
		case err == nil:
		case errors.Is(err, ErrBlocked):
			_ = w.flush(ctx, job, &batch, &reviews)
			return outcomeFailed, err
		default:
			var parse *ParseError
			if errors.As(err, &parse) && len(job.Checkpoint.Done) == 0 && len(batch) == 0 {
				// Nothing has parsed yet this job, so this is the layout
				// itself, not one odd recipe. Stop, and say so, rather than
				// filling the library with half-read recipes.
				return outcomeFailed, err
			}
			if errors.As(err, &parse) {
				// The detail is for the log; the member gets one plain
				// sentence (reasons.go).
				log.InfoContext(ctx, "meal-kit recipe could not be read", "sourceRecipeId", o.SourceRecipeID, "detail", parse.Detail, "status", parse.Status)
				job.Checkpoint.Failures = appendFailure(job.Checkpoint.Failures, FailedRecipe{
					SourceRecipeID: o.SourceRecipeID, Name: o.Name, Reason: fetchFailureReason(job.Source, err),
				})
				if err := w.save(ctx, job); err != nil {
					return outcomeFailed, err
				}
				continue
			}
			// Transient: flush progress and retry the job later.
			_ = w.flush(ctx, job, &batch, &reviews)
			return outcomeFailed, err
		}

		batch = append(batch, recipe)
		reviews = append(reviews, review...)
		if len(batch) >= importBatchSize {
			if err := w.flush(ctx, job, &batch, &reviews); err != nil {
				return outcomeFailed, err
			}
			if err := w.opts.Store.ExtendLease(ctx, job.ID, w.opts.Owner, w.now().Add(w.opts.Lease), w.now()); err != nil {
				return outcomeFailed, err
			}
		}
	}

	if err := w.flush(ctx, job, &batch, &reviews); err != nil {
		return outcomeFailed, err
	}
	job.Checkpoint.Phase = PhaseDone
	return outcomeDone, nil
}

// flush hands a batch of fetched recipes to the import pipeline and records
// them as done. Nothing is marked done before the import returns, so a crash
// re-fetches the batch rather than losing it.
func (w *Worker) flush(ctx context.Context, job *Job, batch *[]recipes.ImportRecipe, reviews *[]recipes.ImportReviewItem) error {
	if len(*batch) == 0 {
		*reviews = nil
		return nil
	}
	file := recipes.ImportFile{
		Version:     recipes.ImportVersion,
		Source:      job.Source,
		GeneratedAt: w.now(),
		Recipes:     *batch,
		Review:      *reviews,
	}
	res, err := w.opts.Publisher.Import(ctx, job.HouseholdID, file)
	if err != nil {
		return fmt.Errorf("import fetched recipes: %w", err)
	}
	merged, err := w.mergeDuplicates(ctx, job, *batch, res.Errors)
	if err != nil {
		return err
	}
	for _, r := range *batch {
		job.Checkpoint.Done = append(job.Checkpoint.Done, r.SourceRecipeID)
	}
	// Any other recipe the shared pipeline rejected is a failure the member
	// can see. Its problems are the pipeline's own wording, with field names
	// ("name is required"), so they go to the log and the member reads one
	// plain sentence. It is already excluded from the counts.
	for _, e := range res.Errors {
		if e.Duplicate {
			continue
		}
		w.opts.Logger.InfoContext(ctx, "the recipe library refused a meal-kit recipe",
			"jobId", job.ID, "sourceRecipeId", e.SourceRecipeID, "problems", strings.Join(e.Problems, "; "))
		job.Checkpoint.Failures = appendFailure(job.Checkpoint.Failures, FailedRecipe{
			SourceRecipeID: e.SourceRecipeID, Name: e.Name, Reason: reasonIncomplete(),
		})
	}
	job.Checkpoint.Imported += res.Created
	job.Checkpoint.Updated += res.Updated
	// A merged duplicate is a recipe that is already in the library.
	job.Checkpoint.Unchanged += res.Unchanged + merged
	job.Checkpoint.ReviewItems += res.ReviewItems
	*batch = (*batch)[:0]
	*reviews = nil
	return w.save(ctx, job)
}

// mergeDuplicates handles the recipes the pipeline refused only because
// another recipe in the batch is the same stored recipe.
//
// That is a real thing on a meal-kit order history: the same dish arrives
// under more than one recipe id (a re-release, or an add-on ordered in
// different weeks), and the library's identity rule rightly keeps one recipe
// for it. So it is not a failure. The duplicate's delivery weeks belong to
// that one recipe, and they are carried onto it with a second, small import
// of the recipe it matched; the duplicate itself is counted as already in the
// library. It returns how many were merged.
func (w *Worker) mergeDuplicates(ctx context.Context, job *Job, batch []recipes.ImportRecipe, errs []recipes.RecipeError) (int, error) {
	merged := 0
	carried := map[int]recipes.ImportRecipe{} // winner index -> winner with the extra weeks
	for _, e := range errs {
		if !e.Duplicate {
			continue
		}
		merged++
		if e.Index < 0 || e.Index >= len(batch) || e.DuplicateOf < 0 || e.DuplicateOf >= len(batch) {
			continue
		}
		winner, ok := carried[e.DuplicateOf]
		if !ok {
			winner = batch[e.DuplicateOf]
			winner.OrderWeeks = slices.Clone(winner.OrderWeeks)
		}
		before := len(winner.OrderWeeks)
		for _, week := range batch[e.Index].OrderWeeks {
			if !slices.Contains(winner.OrderWeeks, week) {
				winner.OrderWeeks = append(winner.OrderWeeks, week)
			}
		}
		if ok || len(winner.OrderWeeks) > before {
			carried[e.DuplicateOf] = winner
		}
	}
	if len(carried) == 0 {
		return merged, nil
	}
	file := recipes.ImportFile{Version: recipes.ImportVersion, Source: job.Source, GeneratedAt: w.now()}
	for _, i := range slices.Sorted(maps.Keys(carried)) {
		file.Recipes = append(file.Recipes, carried[i])
	}
	// Its counts are about recipes this batch already counted, so only an
	// error matters here.
	if _, err := w.opts.Publisher.Import(ctx, job.HouseholdID, file); err != nil {
		return 0, fmt.Errorf("import fetched recipes: %w", err)
	}
	return merged, nil
}

func appendFailure(list []FailedRecipe, f FailedRecipe) []FailedRecipe {
	if len(list) >= MaxFailuresKept {
		return list
	}
	return append(list, f)
}

func (w *Worker) save(ctx context.Context, job *Job) error {
	if err := w.opts.Store.SaveCheckpoint(ctx, job.ID, w.opts.Owner, job.Checkpoint, w.now()); err != nil {
		return err
	}
	observeProgress(ctx, w.opts.Observer, *job, JobRunning)
	return nil
}

func (w *Worker) finish(ctx context.Context, job Job, status JobStatus, jobErr *JobError, log *slog.Logger) {
	if err := w.opts.Store.FinishJob(ctx, job.ID, w.opts.Owner, status, job.Checkpoint, jobErr, w.now()); err != nil && !errors.Is(err, ErrJobGone) {
		log.ErrorContext(ctx, "recording the import outcome failed", "status", status, "error", err)
	}
}

func (w *Worker) requeue(ctx context.Context, job Job, countAttempt bool, jobErr *JobError, log *slog.Logger) {
	at := w.now()
	available := at.Add(w.opts.CapDelay)
	if countAttempt {
		available = at.Add(Backoff(job.Attempts, w.opts.BackoffBase, w.opts.BackoffMax, w.opts.Random()))
	}
	if err := w.opts.Store.RequeueJob(ctx, job.ID, w.opts.Owner, available, countAttempt, jobErr, at); err != nil && !errors.Is(err, ErrJobGone) {
		log.ErrorContext(ctx, "requeueing the import job failed", "error", err)
	}
}

// release puts a job this worker was interrupted in back on the queue, due
// now, without spending an attempt. Every recipe that reached the library is
// already in the last checkpoint; the few fetched since are fetched again.
// It runs on a short context of its own, because ctx is the one that was just
// canceled.
func (w *Worker) release(ctx context.Context, job Job, log *slog.Logger) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	at := w.now()
	err := w.opts.Store.RequeueJob(releaseCtx, job.ID, w.opts.Owner, at, false, nil, at)
	switch {
	case err == nil:
		log.InfoContext(releaseCtx, "meal-kit import interrupted; handed back to the queue for the next worker")
	case errors.Is(err, ErrJobGone):
	default:
		log.WarnContext(releaseCtx, "handing back an interrupted import failed; its lease will expire instead", "error", err)
	}
}

// releaseTimeout bounds the one write release makes during shutdown, well
// inside the web server's own shutdown budget.
const releaseTimeout = 5 * time.Second

// fail decides between another attempt and the dead-letter, and tells the
// member either way.
func (w *Worker) fail(ctx context.Context, job Job, err error, report *Report, log *slog.Logger) {
	retryable := IsRetryable(err) && job.Attempts < job.MaxAttempts
	jobErr := describe(err, job.Source, retryable, w.now())
	if retryable {
		report.Requeued++
		log.WarnContext(ctx, "meal-kit import will retry", "code", jobErr.Code, "attempts", job.Attempts, "maxAttempts", job.MaxAttempts)
		w.requeue(ctx, job, true, jobErr, log)
		observeProgress(ctx, w.opts.Observer, job, JobQueued)
		return
	}
	report.Dead++
	log.ErrorContext(ctx, "meal-kit import gave up", "code", jobErr.Code, "attempts", job.Attempts)
	if errors.Is(err, ErrBlocked) {
		// Back off hard rather than letting the member queue another 740
		// pages at the service that just turned us away. The cursor carries
		// the cooldown; StartImport refuses inside it and says so.
		if err := w.opts.Store.MarkCursorBlocked(ctx, job.HouseholdID, job.Source, w.now()); err != nil {
			log.ErrorContext(ctx, "recording the refusal failed", "error", err)
		}
	}
	w.finish(ctx, job, JobDead, jobErr, log)
	w.notifyAttention(ctx, job, jobErr)
	observeEnd(ctx, w.opts.Observer, job, JobDead)
}

// describe turns an error into what the member reads: short, plain, and
// saying what happens next. It never includes fetched page content, a URL, a
// recipe id, a code, a token, or a cookie; the error itself goes to the log.
//
// retrying says whether another attempt is coming, so a job that has used its
// last one never promises "we'll try again".
func describe(err error, source string, retrying bool, at time.Time) *JobError {
	name := displayName(source)
	next := " We'll try again."
	if !retrying {
		next = " We stopped. Try again later."
	}
	if errors.Is(err, ErrBlocked) {
		return &JobError{Code: ErrCodeBlocked, Message: name + " turned us away, so we stopped. Try again later.", At: at}
	}
	var parse *ParseError
	if errors.As(err, &parse) {
		return &JobError{Code: ErrCodeParse, Message: "We couldn't read " + name + "'s recipe pages. Nothing was changed in your recipes.", At: at}
	}
	if strings.Contains(err.Error(), "import fetched recipes") {
		return &JobError{Code: ErrCodeImport, Message: "Saving your recipes failed." + next, At: at}
	}
	return &JobError{Code: ErrCodeNetwork, Message: "We couldn't reach " + name + "." + next, At: at}
}

// notifyFinished is the "it's done" notification. The push sweep delivers it.
func (w *Worker) notifyFinished(ctx context.Context, job Job) {
	if w.opts.Notifier == nil {
		return
	}
	imported := job.Checkpoint.Imported + job.Checkpoint.Updated
	body := fmt.Sprintf("%d of your HelloFresh recipes are in your library.", imported)
	if job.Source != SourceHelloFresh {
		body = fmt.Sprintf("%d of your meal-kit recipes are in your library.", imported)
	}
	if n := len(job.Checkpoint.Failures); n > 0 {
		body += fmt.Sprintf(" %d couldn't be imported. Open Recipe Import to see which.", n)
	}
	// A harvest that stopped on its page cap read part of the history, not
	// all of it, and only the member's own browser session can read the rest.
	// Saying so is the difference between a finished import and a silent one.
	if job.Harvest.Stopped.MoreToFetch() {
		body += " You have older orders too. Open Recipe Import to get them."
	}
	w.create(ctx, notifications.New{
		HouseholdID: job.HouseholdID,
		Type:        notifications.TypeRecipeImportFinished,
		Title:       "Your recipes are ready",
		Body:        body,
		Subject:     notifications.Subject{Kind: notifications.SubjectRecipeImport, ID: job.ID},
		DedupeKey:   "recipe_import.finished:" + job.ID,
	})
}

// notifyAttention is the other notification: something needs a person.
func (w *Worker) notifyAttention(ctx context.Context, job Job, jobErr *JobError) {
	if w.opts.Notifier == nil || jobErr == nil {
		return
	}
	w.create(ctx, notifications.New{
		HouseholdID: job.HouseholdID,
		Type:        notifications.TypeRecipeImportAttention,
		Title:       "Recipe import needs you",
		Body:        jobErr.Message,
		Subject:     notifications.Subject{Kind: notifications.SubjectRecipeImport, ID: job.ID},
		// One notification per job per reason, so a retry that fails the
		// same way does not nag twice.
		DedupeKey: "recipe_import.attention:" + job.ID + ":" + jobErr.Code,
	})
}

func (w *Worker) create(ctx context.Context, in notifications.New) {
	if _, _, err := w.opts.Notifier.Create(ctx, in); err != nil {
		w.opts.Logger.WarnContext(ctx, "raising the import notification failed", "type", in.Type, "error", err)
	}
}

func (w *Worker) now() time.Time { return w.opts.Now().UTC().Truncate(time.Millisecond) }
