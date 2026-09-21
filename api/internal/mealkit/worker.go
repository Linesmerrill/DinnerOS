package mealkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
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
	// Service supplies the link's decrypted tokens. It is the only thing
	// that can open an envelope.
	Service *Service
	// Publisher is the seam into the recipe library (publisher.go).
	Publisher RecipePublisher
	// Sources are the meal-kit clients, keyed by name. One Source is one
	// polite conversation, so a Worker uses at most one at a time.
	Sources  map[string]Source
	Notifier Notifier
	Logger   *slog.Logger

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
	// Succeeded, Paused, Requeued, Dead, and Gone are their outcomes. Gone
	// counts jobs that were canceled or taken over while running, which is
	// the expected shape of an unlink mid-run.
	Succeeded int
	Paused    int
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
	case errors.Is(err, ErrAuthExpired):
		// The stored session is spent and could not be refreshed. Pause and
		// ask the member to sign in again rather than spending attempts on
		// something no retry can fix.
		outcome = outcomePaused
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The dyno is going away. Leave the lease to expire; the next run
		// resumes from the checkpoint.
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
	case outcomePaused:
		report.Paused++
		jobErr := &JobError{Code: ErrCodeAuthExpired, Message: "Your meal-kit sign-in expired. Sign in again to finish importing.", At: w.now()}
		w.finish(ctx, job, JobPausedAuth, jobErr, log)
		w.markLinkNeedsReauth(ctx, job)
		w.notifyAttention(ctx, job, jobErr)
	case outcomeCapped:
		report.Requeued++
		w.requeue(ctx, job, false, nil, log)
	case outcomeFailed:
		w.fail(ctx, job, err, report, log)
	}
}

type outcome int

const (
	outcomeDone outcome = iota
	outcomePaused
	outcomeCapped
	outcomeFailed
)

// execute runs the job's phases, saving a checkpoint after each unit of work.
func (w *Worker) execute(ctx context.Context, job *Job, log *slog.Logger) (outcome, error) {
	src, err := w.opts.Service.Source(job.Source)
	if err != nil {
		return outcomeFailed, &ParseError{Subject: "the import", Detail: "this server cannot import from " + job.Source}
	}
	link, err := w.opts.Store.GetLinkByID(ctx, job.LinkID)
	if errors.Is(err, ErrNotFound) {
		// The tokens are gone: an unlink that raced this claim.
		return outcomeFailed, ErrNoLink
	}
	if err != nil {
		return outcomeFailed, err
	}
	tokens, err := w.opts.Service.Tokens(link)
	if err != nil {
		return outcomeFailed, err
	}

	// A session that is already spent is refreshed before any work, so an
	// expired token costs one request rather than a whole failed run.
	if tokens.ExpiresAt.IsZero() || !tokens.ExpiresAt.After(w.now()) {
		tokens, err = w.refresh(ctx, src, link, tokens)
		if err != nil {
			return outcomeFailed, err
		}
	}

	if job.Checkpoint.Phase == "" || job.Checkpoint.Phase == PhaseOrders {
		orders, err := src.OrderHistory(ctx, tokens)
		if errors.Is(err, ErrAuthExpired) {
			if tokens, err = w.refresh(ctx, src, link, tokens); err != nil {
				return outcomeFailed, err
			}
			orders, err = src.OrderHistory(ctx, tokens)
		}
		if err != nil {
			return outcomeFailed, err
		}
		job.Checkpoint.Orders = orders
		job.Checkpoint.Phase = PhaseRecipes
		if err := w.save(ctx, job); err != nil {
			return outcomeFailed, err
		}
		log.InfoContext(ctx, "meal-kit order history read", "recipes", len(orders))
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

		recipe, review, err := src.Recipe(ctx, tokens, o)
		fetched++
		if errors.Is(err, ErrAuthExpired) {
			if tokens, err = w.refresh(ctx, src, link, tokens); err != nil {
				_ = w.flush(ctx, job, &batch, &reviews)
				return outcomeFailed, err
			}
			recipe, review, err = src.Recipe(ctx, tokens, o)
		}
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
				job.Checkpoint.Failures = appendFailure(job.Checkpoint.Failures, FailedRecipe{
					SourceRecipeID: o.SourceRecipeID, Name: o.Name, Reason: parse.Detail,
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
	for _, r := range *batch {
		job.Checkpoint.Done = append(job.Checkpoint.Done, r.SourceRecipeID)
	}
	// A recipe the shared pipeline rejected is a failure the member can see,
	// with the pipeline's own reason. It is already excluded from the counts.
	for _, e := range res.Errors {
		job.Checkpoint.Failures = appendFailure(job.Checkpoint.Failures, FailedRecipe{
			SourceRecipeID: e.SourceRecipeID, Name: e.Name, Reason: strings.Join(e.Problems, "; "),
		})
	}
	job.Checkpoint.Imported += res.Created
	job.Checkpoint.Updated += res.Updated
	job.Checkpoint.Unchanged += res.Unchanged
	job.Checkpoint.ReviewItems += res.ReviewItems
	*batch = (*batch)[:0]
	*reviews = nil
	return w.save(ctx, job)
}

func appendFailure(list []FailedRecipe, f FailedRecipe) []FailedRecipe {
	if len(list) >= MaxFailuresKept {
		return list
	}
	return append(list, f)
}

// refresh exchanges the refresh token for a new session and stores it. A
// refresh that fails is ErrAuthExpired, which pauses the job.
func (w *Worker) refresh(ctx context.Context, src Source, link Link, tokens Tokens) (Tokens, error) {
	fresh, err := src.Refresh(ctx, tokens)
	if err != nil {
		return Tokens{}, ErrAuthExpired
	}
	if fresh.Empty() {
		return Tokens{}, ErrAuthExpired
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = tokens.RefreshToken
	}
	sealed, err := w.opts.Service.cipher.SealTokens(fresh)
	if err != nil {
		return Tokens{}, err
	}
	if err := w.opts.Store.SaveLinkTokens(ctx, link.ID, sealed, fresh.ExpiresAt, w.now()); err != nil {
		return Tokens{}, err
	}
	return fresh, nil
}

func (w *Worker) save(ctx context.Context, job *Job) error {
	return w.opts.Store.SaveCheckpoint(ctx, job.ID, w.opts.Owner, job.Checkpoint, w.now())
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

// fail decides between another attempt and the dead-letter, and tells the
// member either way.
func (w *Worker) fail(ctx context.Context, job Job, err error, report *Report, log *slog.Logger) {
	jobErr := describe(err, w.now())
	retryable := IsRetryable(err) && job.Attempts < job.MaxAttempts
	if retryable {
		report.Requeued++
		log.WarnContext(ctx, "meal-kit import will retry", "code", jobErr.Code, "attempts", job.Attempts, "maxAttempts", job.MaxAttempts)
		w.requeue(ctx, job, true, jobErr, log)
		return
	}
	report.Dead++
	log.ErrorContext(ctx, "meal-kit import gave up", "code", jobErr.Code, "attempts", job.Attempts)
	w.finish(ctx, job, JobDead, jobErr, log)
	w.notifyAttention(ctx, job, jobErr)
}

// describe turns an error into what the member reads. It never includes
// fetched page content, a URL, a token, or a cookie.
func describe(err error, at time.Time) *JobError {
	switch {
	case errors.Is(err, ErrAuthExpired):
		return &JobError{Code: ErrCodeAuthExpired, Message: "Your meal-kit sign-in expired. Sign in again to finish importing.", At: at}
	case errors.Is(err, ErrBlocked):
		return &JobError{Code: ErrCodeBlocked, Message: "The meal-kit service refused our requests, so the import stopped. Try again later.", At: at}
	case errors.Is(err, ErrNoLink):
		return &JobError{Code: ErrCodeInternal, Message: "The meal-kit account was unlinked, so the import stopped.", At: at}
	}
	var parse *ParseError
	if errors.As(err, &parse) {
		return &JobError{Code: ErrCodeParse, Message: "We could not read " + parse.Subject + ": " + parse.Detail + " Nothing was changed in your recipes.", At: at}
	}
	if strings.Contains(err.Error(), "import fetched recipes") {
		return &JobError{Code: ErrCodeImport, Message: "Saving the imported recipes failed. Nothing was half-written; the import will be retried.", At: at}
	}
	return &JobError{Code: ErrCodeNetwork, Message: "We could not reach the meal-kit service. The import will be retried.", At: at}
}

func (w *Worker) markLinkNeedsReauth(ctx context.Context, job Job) {
	if err := w.opts.Store.SetLinkStatus(ctx, job.LinkID, LinkNeedsReauth, w.now()); err != nil && !errors.Is(err, ErrNotFound) {
		w.opts.Logger.WarnContext(ctx, "marking the meal-kit link for re-authentication failed", "error", err)
	}
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
		body += fmt.Sprintf(" %d could not be imported — open Recipe Import to see why.", n)
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
	title := "Recipe import needs you"
	if jobErr.Code == ErrCodeAuthExpired {
		title = "Sign in to finish importing"
	}
	w.create(ctx, notifications.New{
		HouseholdID: job.HouseholdID,
		Type:        notifications.TypeRecipeImportAttention,
		Title:       title,
		Body:        jobErr.Message,
		Subject:     notifications.Subject{Kind: notifications.SubjectRecipeImport, ID: job.ID},
		// One notification per job per reason: a job that pauses, resumes,
		// and pauses again does not nag twice for the same thing.
		DedupeKey: "recipe_import.attention:" + job.ID + ":" + jobErr.Code,
	})
}

func (w *Worker) create(ctx context.Context, in notifications.New) {
	if _, _, err := w.opts.Notifier.Create(ctx, in); err != nil {
		w.opts.Logger.WarnContext(ctx, "raising the import notification failed", "type", in.Type, "error", err)
	}
}

func (w *Worker) now() time.Time { return w.opts.Now().UTC().Truncate(time.Millisecond) }
