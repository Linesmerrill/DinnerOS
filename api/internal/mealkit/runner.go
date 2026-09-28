package mealkit

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// The web process's share of the import work (docs/meal-kit-import.md,
// "Who does the work").
const (
	// DefaultBatchSize is how many recipe pages the web process fetches for
	// a job before it puts the job down. At the polite rate (2.5 s plus up to
	// 40% jitter, about 3 s a page, and about 3.2 s with the import measured
	// in production) 50 pages is about two and a half minutes of work: long
	// enough that the member watches the bar cross a real distance, short
	// enough that another household's queued import never waits long behind
	// it, and far inside DefaultLease even before the lease is extended at
	// each checkpoint of 10.
	DefaultBatchSize = 50
	// DefaultBatchPause is how long a job rests between two of the web
	// process's batches. It is DefaultCapDelay, the same rest the scheduled
	// command gives a job it capped, so the web process averages about one
	// request every 4 s over a long history rather than one every 3.
	DefaultBatchPause = DefaultCapDelay
	// DefaultRunnerPoll is how often an idle runner looks for a due job: one
	// indexed find-and-modify that usually matches nothing. It bounds how
	// late a rested job starts its next batch.
	DefaultRunnerPoll = 20 * time.Second
)

// Kicker is told about a job the moment it is queued, so it can start work on
// it without waiting for a scheduled run. Runner is one; a Service without a
// Kicker simply leaves the job for the scheduled command.
type Kicker interface {
	Kick(jobID string)
}

// RunnerOptions configures a Runner.
type RunnerOptions struct {
	// Worker is the worker the runner drives: the same store, service,
	// publisher, and sources the scheduled command wires. RecipesPerRun,
	// CapDelay, MaxJobs, and ReleaseOnCancel are set by the runner.
	Worker WorkerOptions
	// BatchSize caps the recipe pages one batch fetches. Default
	// DefaultBatchSize.
	BatchSize int
	// BatchPause is the rest between two batches of one job. Default
	// DefaultBatchPause.
	BatchPause time.Duration
	// Poll is how often an idle runner looks for a due job. Default
	// DefaultRunnerPoll.
	Poll time.Duration
}

// Runner is the import worker living inside the web process.
//
// It is not a second code path. It drives a Worker — the same claim, lease,
// checkpoints, per-recipe politeness, and 401/403 stop the scheduled command
// uses — one batch at a time, from one goroutine:
//
//   - When a member queues an import, Kick wakes it and it claims that job
//     by ID and fetches its first batch straight away, so the status moves
//     from queued to running within seconds.
//   - A batch ends on BatchSize; the job is requeued BatchPause out, exactly
//     as the scheduled command requeues a job that hit its per-run cap.
//   - Between kicks it polls for any due job — its own rested ones, one whose
//     worker died (an expired lease), or one the scheduled command put down —
//     so an import finishes even if the Heroku Scheduler job is missing.
//
// One goroutine means one polite conversation per process and never two
// batches for one household from this process; the atomic claim and the lease
// make that true across processes, including a scheduled run racing it.
// Canceling Run's context (a restart, an Eco dyno going to sleep) hands the
// job in hand straight back to the queue.
type Runner struct {
	worker *Worker
	poll   time.Duration
	logger *slog.Logger

	mu sync.Mutex
	// pending are jobs kicked since the runner last looked, oldest first.
	pending []string
	wake    chan struct{}
}

// maxPending bounds the kick list. A kick that doesn't fit is not lost: the
// job is queued and due, so the next poll claims it.
const maxPending = 64

// NewRunner returns a Runner with the defaults filled in.
func NewRunner(opts RunnerOptions) *Runner {
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.BatchPause <= 0 {
		opts.BatchPause = DefaultBatchPause
	}
	if opts.Poll <= 0 {
		opts.Poll = DefaultRunnerPoll
	}
	wo := opts.Worker
	wo.RecipesPerRun = opts.BatchSize
	wo.CapDelay = opts.BatchPause
	wo.MaxJobs = 1
	wo.ReleaseOnCancel = true
	if wo.Owner == "" {
		wo.Owner = fmt.Sprintf("web-%d", time.Now().UnixNano())
	}
	worker := NewWorker(wo)
	return &Runner{
		worker: worker,
		poll:   opts.Poll,
		logger: worker.opts.Logger.With("worker", "web"),
		wake:   make(chan struct{}, 1),
	}
}

// Kick implements Kicker. It never blocks the request that queued the job.
func (r *Runner) Kick(jobID string) {
	if jobID == "" {
		return
	}
	r.mu.Lock()
	if !slices.Contains(r.pending, jobID) && len(r.pending) < maxPending {
		r.pending = append(r.pending, jobID)
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runner) nextPending() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) == 0 {
		return ""
	}
	id := r.pending[0]
	r.pending = r.pending[1:]
	return id
}

// Run works batches until ctx is done. It returns once the batch in hand has
// been handed back.
func (r *Runner) Run(ctx context.Context) {
	r.logger.InfoContext(ctx, "meal-kit import runner started", "owner", r.worker.opts.Owner,
		"batchSize", r.worker.opts.RecipesPerRun, "batchPause", r.worker.opts.CapDelay, "poll", r.poll)
	timer := time.NewTimer(r.poll)
	defer timer.Stop()
	for {
		// Drain: a job someone just queued first, then anything due. Each
		// Step is one batch, so a kick is noticed between batches.
		for ctx.Err() == nil && r.Step(ctx) {
		}
		if ctx.Err() != nil {
			r.logger.InfoContext(context.WithoutCancel(ctx), "meal-kit import runner stopped")
			return
		}
		// Go 1.23+ timers: Reset discards a stale tick, so no drain is needed.
		timer.Reset(r.poll)
		select {
		case <-ctx.Done():
		case <-r.wake:
		case <-timer.C:
		}
	}
}

// Step runs at most one batch: a kicked job if one is runnable, otherwise the
// next due job. It reports whether it ran one. Run loops on it; tests call it
// directly with a controlled clock.
func (r *Runner) Step(ctx context.Context) bool {
	for id := r.nextPending(); id != ""; id = r.nextPending() {
		report, err := r.worker.RunJob(ctx, id)
		if err != nil {
			r.logger.ErrorContext(ctx, "starting a queued meal-kit import failed", "jobId", id, "error", err)
		}
		if report.Claimed > 0 {
			r.logBatch(ctx, report)
			return true
		}
	}
	report, err := r.worker.Run(ctx)
	if err != nil {
		r.logger.ErrorContext(ctx, "claiming a meal-kit import failed", "error", err)
	}
	if report.Claimed == 0 {
		return false
	}
	r.logBatch(ctx, report)
	return true
}

func (r *Runner) logBatch(ctx context.Context, report Report) {
	r.logger.InfoContext(ctx, "meal-kit import batch finished",
		"succeeded", report.Succeeded, "requeued", report.Requeued, "dead", report.Dead, "gone", report.Gone,
		"recipes", report.Recipes, "recipeFailures", report.Failures)
}
