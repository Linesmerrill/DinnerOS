package mealkit

import "context"

// ProgressObserver hears about a job's progress so something outside this
// package can mirror it — the Live Activity on the member's Lock Screen
// (internal/liveactivity, docs/meal-kit-import.md#live-activity).
//
// It is a hook, not a dependency: the worker and the service call it after a
// write has succeeded, never before, and nothing it does can fail the job. A
// nil observer is fine and is what a deployment without APNs runs with.
//
// The job passed in is a snapshot with Status already set to what was just
// stored: JobRunning after a checkpoint, JobQueued when a run put the job down
// (its per-run cap, or a retry), and a terminal status on the way out.
type ProgressObserver interface {
	// ImportProgressed is called after a checkpoint is stored and when a run
	// puts the job back in the queue. It may be called often; rate limiting is
	// the observer's business.
	ImportProgressed(ctx context.Context, job Job)
	// ImportEnded is called once a job reaches a terminal status: succeeded,
	// dead, or canceled.
	ImportEnded(ctx context.Context, job Job)
}

func observeProgress(ctx context.Context, o ProgressObserver, job Job, status JobStatus) {
	if o == nil {
		return
	}
	job.Status = status
	o.ImportProgressed(ctx, job)
}

func observeEnd(ctx context.Context, o ProgressObserver, job Job, status JobStatus) {
	if o == nil {
		return
	}
	job.Status = status
	o.ImportEnded(ctx, job)
}
