package mealkit

import (
	"context"
	"time"
)

// Store persists meal-kit account links and the import job queue.
//
// Link methods are scoped by householdID; implementations filter on it and
// never trust a HouseholdID field on the value passed in. Missing records are
// ErrNotFound. Slices in returned values are nil rather than empty.
type Store interface {
	// UpsertLink stores l as the household's link for l.Source, replacing an
	// existing one (and its encrypted tokens) and keeping its CreatedAt. It
	// returns the stored link.
	UpsertLink(ctx context.Context, l Link) (Link, error)
	// GetLink returns the household's link for source, or ErrNotFound.
	GetLink(ctx context.Context, householdID, source string) (Link, error)
	// GetLinkByID returns one link by its own ID, or ErrNotFound.
	GetLinkByID(ctx context.Context, id string) (Link, error)
	// SetLinkStatus records a new status, and ExpiresAt when non-zero.
	SetLinkStatus(ctx context.Context, id string, status LinkStatus, at time.Time) error
	// SaveLinkTokens replaces a link's encrypted tokens after a refresh and
	// marks it LinkActive.
	SaveLinkTokens(ctx context.Context, id string, secret Envelope, expiresAt, at time.Time) error
	// DeleteLink removes the household's link for source, tokens and all.
	// Deleting a link that does not exist is not an error.
	DeleteLink(ctx context.Context, householdID, source string) error

	// InsertJob stores j, assigning its ID.
	InsertJob(ctx context.Context, j Job) (Job, error)
	// GetJob returns one of the household's jobs.
	GetJob(ctx context.Context, householdID, id string) (Job, error)
	// LatestJob returns the household's newest job for source, or
	// ErrNotFound.
	LatestJob(ctx context.Context, householdID, source string) (Job, error)
	// ActiveJob returns the household's queued, running, or paused job for
	// source, or ErrNotFound. At most one exists at a time.
	ActiveJob(ctx context.Context, householdID, source string) (Job, error)
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
	// FinishJob moves a claimed job to a terminal status (or to
	// JobPausedAuth) and releases the lease. ErrJobGone when the job is no
	// longer claimed by owner.
	FinishJob(ctx context.Context, id, owner string, status JobStatus, checkpoint Checkpoint, jobErr *JobError, at time.Time) error
	// RequeueJob returns a claimed job to the queue, available at
	// availableAt. countAttempt is false when the run stopped on its own
	// per-run cap rather than on an error, so a long history does not
	// dead-letter itself. ErrJobGone when the job is no longer claimed.
	RequeueJob(ctx context.Context, id, owner string, availableAt time.Time, countAttempt bool, jobErr *JobError, at time.Time) error
	// CancelJobs moves every non-terminal job of the household's source to
	// JobCanceled and returns how many were canceled. Unlinking calls it, so
	// a running worker's next conditional write fails with ErrJobGone and it
	// stops without touching the library again.
	CancelJobs(ctx context.Context, householdID, source, reason string, at time.Time) (int, error)
	// ResumePausedJobs makes the household's paused-for-auth jobs runnable
	// again after a re-link, and returns how many.
	ResumePausedJobs(ctx context.Context, householdID, source string, at time.Time) (int, error)
}
