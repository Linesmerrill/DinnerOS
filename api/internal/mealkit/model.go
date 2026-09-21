package mealkit

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Errors returned by stores, the service, and the worker.
var (
	ErrNotFound = errors.New("mealkit: not found")
	// ErrNoLink means the household has no linked meal-kit account.
	ErrNoLink = errors.New("mealkit: no linked account")
	// ErrJobGone means a claimed job is no longer the caller's to write: it
	// was canceled (an unlink) or its lease was taken over. The worker stops.
	ErrJobGone = errors.New("mealkit: job is no longer claimed by this worker")
	// ErrDisabled means the feature is off (no encryption key configured).
	ErrDisabled = errors.New("mealkit: recipe import is not enabled")
	// ErrAuthExpired means the stored tokens no longer work; the member has to
	// sign in again. The job pauses rather than burning attempts.
	ErrAuthExpired = errors.New("mealkit: the meal-kit session expired")
	// ErrBlocked means the source refused us (HTTP 403). The run stops
	// immediately and never retries around an access control.
	ErrBlocked = errors.New("mealkit: the meal-kit service refused the request")

	errHouseholdRequired = errors.New("mealkit: household id is required")
)

// ParseError means the source's response did not look the way this build
// expects — a layout change, not a transient failure. A job that hits one
// fails cleanly with the message and is not retried, so nothing half-read
// reaches the library.
type ParseError struct {
	// What was being read, e.g. "order history" or "recipe 6512...".
	Subject string
	// Detail is safe to show a member: it never contains fetched page content.
	Detail string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("mealkit: could not read %s: %s", e.Subject, e.Detail)
}

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// SourceHelloFresh is the only meal-kit service implemented today. It matches
// the "hellofresh" source of the recipe import contract.
const SourceHelloFresh = "hellofresh"

// KnownSource reports whether s is a meal-kit service this build can import.
func KnownSource(s string) bool { return s == SourceHelloFresh }

// LinkStatus is the state of a member's meal-kit account link.
type LinkStatus string

// Link statuses.
const (
	// LinkActive: the stored tokens are believed good.
	LinkActive LinkStatus = "active"
	// LinkNeedsReauth: the tokens expired or were rejected. Jobs pause here
	// until the member signs in again.
	LinkNeedsReauth LinkStatus = "needs_reauth"
)

// Link is one household member's connection to a meal-kit account.
//
// Tokens are never in plaintext in this struct: Secret holds the envelope
// ciphertext and travels only between the store and the Cipher.
type Link struct {
	ID          string
	HouseholdID string
	// UserID is the member who linked; only they (or an admin unlinking) act
	// on it, and their account deletion removes it.
	UserID string
	Source string
	Status LinkStatus
	// AccountLabel is a non-identifying hint shown in the app, such as
	// "HelloFresh account". It never holds the member's email address.
	AccountLabel string
	// Secret is the envelope-encrypted session and refresh tokens.
	Secret Envelope
	// ExpiresAt is when the session token stops working, as the source
	// reported it. Zero when the source did not say.
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	// LastUsedAt is when a worker last read the tokens.
	LastUsedAt time.Time
}

// Tokens are a meal-kit account's session credentials. They exist only in
// memory: the Cipher encrypts them before they reach the store, and nothing
// logs them. Tokens deliberately has no String or LogValue method that could
// print a value.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Empty reports whether there is nothing worth storing.
func (t Tokens) Empty() bool { return t.AccessToken == "" && t.RefreshToken == "" }

// LogValue implements slog.LogValuer, so tokens passed to a logger by
// accident print as "redacted" instead of as themselves.
func (t Tokens) LogValue() slog.Value { return slog.StringValue("redacted") }

// JobStatus is where an import job is in its lifecycle
// (docs/meal-kit-import.md#job-lifecycle).
type JobStatus string

// Job statuses. Queued and Running are the working states; the rest are what
// a member sees.
const (
	// JobQueued: waiting for a worker. AvailableAt is when it may be claimed.
	JobQueued JobStatus = "queued"
	// JobRunning: a worker holds the lease. A worker that dies leaves the
	// lease to expire, and the next run takes it over from the checkpoint.
	JobRunning JobStatus = "running"
	// JobPausedAuth: the tokens expired. It waits for the member to sign in
	// again, which requeues it. Attempts are not spent here.
	JobPausedAuth JobStatus = "paused_auth"
	// JobSucceeded: the whole order history was imported. Some individual
	// recipes may still have failed; Stats and Failures say which.
	JobSucceeded JobStatus = "succeeded"
	// JobDead: dead-lettered after MaxAttempts, or stopped by something that
	// retrying cannot fix (a layout change, a refusal). LastError says why.
	JobDead JobStatus = "dead"
	// JobCanceled: the member unlinked, or a newer job replaced this one.
	JobCanceled JobStatus = "canceled"
)

// Terminal reports whether the job will never run again.
func (s JobStatus) Terminal() bool {
	return s == JobSucceeded || s == JobDead || s == JobCanceled
}

// Active reports whether the job still counts as in flight for the member.
func (s JobStatus) Active() bool {
	return s == JobQueued || s == JobRunning || s == JobPausedAuth
}

// Phase is how far a run got. It is the resume point: a restart re-enters at
// the phase the checkpoint records instead of starting over.
type Phase string

// Phases, in order.
const (
	// PhaseOrders: reading the account's own order history.
	PhaseOrders Phase = "orders"
	// PhaseRecipes: fetching the recipes those orders contain.
	PhaseRecipes Phase = "recipes"
	// PhaseImport: handing the normalized recipes to the import pipeline.
	PhaseImport Phase = "import"
	// PhaseDone: finished.
	PhaseDone Phase = "done"
)

// FailedRecipe is one recipe of the order history that could not be imported.
// Reason is written for a member to read and never quotes fetched page
// content.
type FailedRecipe struct {
	SourceRecipeID string
	Name           string
	Reason         string
}

// MaxFailuresKept caps the failure list stored on a job, so one bad run can
// never grow an unbounded document.
const MaxFailuresKept = 100

// Checkpoint is the resumable state of a run. A worker writes it after every
// unit of work, so a restart (or the next scheduled run) re-fetches nothing it
// already has.
type Checkpoint struct {
	Phase Phase
	// Orders are the recipes the account ordered, discovered in PhaseOrders.
	Orders []OrderedRecipe
	// Done lists the source recipe IDs already fetched and handed to the
	// import pipeline.
	Done []string
	// Failures are recipes that could not be fetched or normalized. The run
	// still finishes; the member sees them in the result.
	Failures []FailedRecipe
	// Imported, Updated, and Unchanged come from the import pipeline.
	Imported    int
	Updated     int
	Unchanged   int
	ReviewItems int
}

// Remaining returns the ordered recipes not yet done or failed, in order.
func (c Checkpoint) Remaining() []OrderedRecipe {
	done := make(map[string]bool, len(c.Done)+len(c.Failures))
	for _, id := range c.Done {
		done[id] = true
	}
	for _, f := range c.Failures {
		done[f.SourceRecipeID] = true
	}
	out := make([]OrderedRecipe, 0, len(c.Orders))
	for _, o := range c.Orders {
		if !done[o.SourceRecipeID] {
			out = append(out, o)
		}
	}
	return out
}

// JobError is the last thing that went wrong, ready to show a member.
type JobError struct {
	// Code is a stable machine-readable reason: "auth_expired", "parse",
	// "blocked", "network", "import", or "internal".
	Code string
	// Message is one sentence for a person. It never contains tokens,
	// cookies, URLs with credentials, or fetched page content.
	Message string
	At      time.Time
}

// Error codes.
const (
	ErrCodeAuthExpired = "auth_expired"
	ErrCodeParse       = "parse"
	ErrCodeBlocked     = "blocked"
	ErrCodeNetwork     = "network"
	ErrCodeImport      = "import"
	ErrCodeInternal    = "internal"
)

// Job is one import run for a household.
type Job struct {
	ID          string
	HouseholdID string
	// UserID is the member who started it: who gets asked to sign in again.
	UserID string
	LinkID string
	Source string
	Status JobStatus
	// Attempts counts claims. MaxAttempts of them dead-letters the job.
	Attempts    int
	MaxAttempts int
	// AvailableAt is the earliest a worker may claim it (the backoff).
	AvailableAt time.Time
	// LeaseOwner identifies the worker run holding it; LeaseExpiresAt is the
	// visibility timeout after which another run may take it over.
	LeaseOwner     string
	LeaseExpiresAt time.Time
	Checkpoint     Checkpoint
	LastError      *JobError
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      time.Time
	FinishedAt     time.Time
}

// RecipesFound is how many recipes the account's orders contain, or 0 before
// the order history has been read.
func (j Job) RecipesFound() int { return len(j.Checkpoint.Orders) }

// RecipesDone is how many of them have been handed to the import pipeline.
func (j Job) RecipesDone() int { return len(j.Checkpoint.Done) }

// OrderedRecipe is one recipe on the account's own order history: what the
// household actually received, and the ISO weeks they received it.
type OrderedRecipe struct {
	// SourceRecipeID is the meal kit's ID for the delivered recipe.
	SourceRecipeID string
	Name           string
	// URL is the source page to fetch. The Source checks it against its own
	// allow-list before requesting it, so a URL the account data carries can
	// never send us somewhere else.
	URL string
	// Weeks are ISO weeks ("2026-W30") this recipe was delivered.
	Weeks []string
	// IsAddon marks sides and extras rather than a main meal.
	IsAddon bool
}

// Defaults for the queue and the worker. Every one is overridable in
// WorkerOptions; these are what production runs with.
const (
	// DefaultMaxAttempts dead-letters a job after this many claims.
	DefaultMaxAttempts = 5
	// DefaultLease is the visibility timeout. It is comfortably longer than a
	// capped run takes and shorter than the scheduler interval, so a dyno
	// killed mid-run is picked up by the next one.
	DefaultLease = 8 * time.Minute
	// DefaultBackoffBase is the first retry delay; it doubles per attempt.
	DefaultBackoffBase = 2 * time.Minute
	// DefaultBackoffMax caps the delay.
	DefaultBackoffMax = 2 * time.Hour
	// DefaultCapDelay is how long a job that hit its per-run cap waits before
	// it is claimable again: long enough that the run that put it down does
	// not pick it straight back up, short enough that the next scheduled run
	// continues it.
	DefaultCapDelay = 1 * time.Minute
	// DefaultRecipesPerRun is the real per-run cap on source requests. A run
	// that reaches it checkpoints and requeues itself, so a 400-recipe
	// history spreads over several scheduled runs instead of one long
	// hammering session.
	DefaultRecipesPerRun = 40
)

// Backoff returns how long to wait before the next attempt, doubling per
// attempt and capped. jitter is a value in [0,1) that spreads retries of
// jobs that failed together.
func Backoff(attempt int, base, max time.Duration, jitter float64) time.Duration {
	if base <= 0 {
		base = DefaultBackoffBase
	}
	if max <= 0 {
		max = DefaultBackoffMax
	}
	if attempt < 1 {
		attempt = 1
	}
	d := base
	for i := 1; i < attempt && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	// Up to a quarter of the delay again, so a batch of jobs that failed in
	// the same run does not retry in lockstep.
	return d + time.Duration(float64(d)*0.25*jitter)
}
