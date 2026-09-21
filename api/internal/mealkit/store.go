package mealkit

import (
	"context"
	"time"
)

// Store persists the import job queue and the per-household harvest cursor.
//
// There is nothing else to persist: no account link, no token, no cookie. The
// cursor is two ISO weeks and a flag (cursor.go), which is what makes a long
// order history resumable without keeping anything about the account. Job
// methods are scoped by householdID where a household could otherwise read
// another's; implementations filter on it and never trust a HouseholdID field
// on the value passed in. Missing records are ErrNotFound. Slices in returned
// values are nil rather than empty.
type Store interface {
	// InsertJob stores j, assigning its ID.
	InsertJob(ctx context.Context, j Job) (Job, error)
	// GetJob returns one of the household's jobs.
	GetJob(ctx context.Context, householdID, id string) (Job, error)
	// LatestJob returns the household's newest job for source, or
	// ErrNotFound.
	LatestJob(ctx context.Context, householdID, source string) (Job, error)
	// ActiveJob returns the household's queued or running job for source, or
	// ErrNotFound. At most one exists at a time.
	ActiveJob(ctx context.Context, householdID, source string) (Job, error)
	// AnyActiveJob returns the household's queued or running job on any
	// source, or ErrNotFound. It is the single-flight check: one household is
	// one polite conversation, so a second import is never started next to a
	// first.
	AnyActiveJob(ctx context.Context, householdID string) (Job, error)
	// ListJobs returns the household's jobs, newest first, at most limit.
	ListJobs(ctx context.Context, householdID string, limit int) ([]Job, error)

	// ClaimJob atomically takes the next runnable job for owner: one that is
	// queued and due, or one whose running lease expired. It returns
	// ErrNotFound when there is nothing to do. The claim counts an attempt
	// and sets the lease to expire at leaseUntil.
	//
	// This one find-and-modify is what makes two workers safe: MongoDB
	// applies the matching update to a single document, so a job is claimed
	// exactly once however many workers race.
	ClaimJob(ctx context.Context, owner string, now, leaseUntil time.Time) (Job, error)
	// ExtendLease pushes a claimed job's visibility timeout out. It returns
	// ErrJobGone when the job is no longer running under owner.
	ExtendLease(ctx context.Context, id, owner string, leaseUntil, at time.Time) error
	// SaveCheckpoint stores progress for a job still claimed by owner, so a
	// restart resumes instead of re-fetching. ErrJobGone when it is not.
	SaveCheckpoint(ctx context.Context, id, owner string, c Checkpoint, at time.Time) error
	// FinishJob moves a claimed job to a terminal status and releases the
	// lease. ErrJobGone when the job is no longer claimed by owner.
	FinishJob(ctx context.Context, id, owner string, status JobStatus, checkpoint Checkpoint, jobErr *JobError, at time.Time) error
	// RequeueJob returns a claimed job to the queue, available at
	// availableAt. countAttempt is false when the run stopped on its own
	// per-run cap rather than on an error, so a long history does not
	// dead-letter itself. ErrJobGone when the job is no longer claimed.
	RequeueJob(ctx context.Context, id, owner string, availableAt time.Time, countAttempt bool, jobErr *JobError, at time.Time) error
	// CancelJobs moves every non-terminal job of the household's source to
	// JobCanceled and returns how many were canceled. Stopping an import
	// calls it, so a running worker's next conditional write fails with
	// ErrJobGone and it stops without touching the library again.
	CancelJobs(ctx context.Context, householdID, source, reason string, at time.Time) (int, error)

	// GetCursor returns where this household's harvests of source have
	// reached, or ErrNotFound when none has run.
	GetCursor(ctx context.Context, householdID, source string) (Cursor, error)
	// SaveCursor stores c, keyed by household and source. It is an upsert:
	// the caller merges (Cursor.Merge) and writes the result.
	SaveCursor(ctx context.Context, c Cursor) error
	// MarkCursorBlocked records that the source refused us, so a new import
	// is refused for BlockedCooldown instead of queued. It creates the cursor
	// when there is none.
	MarkCursorBlocked(ctx context.Context, householdID, source string, at time.Time) error
}
