// Package liveactivity keeps a meal-kit import's Live Activity current while
// the app is closed (docs/meal-kit-import.md#live-activity).
//
// The app starts the activity itself when the member queues an import, with
// pushType .token, and hands the activity's push token to
// PUT .../imports/{jobId}/live-activity. The token is stored on the import
// job, never logged, and removed when the job ends. Pusher implements
// mealkit.ProgressObserver: after a checkpoint it sends an `update`, rate
// limited (Limiter), and when the job ends it sends an `end` carrying the
// final state and a dismissal date.
//
// What the activity may show is deliberately narrow: the service's name and
// recipe counts. Nothing about the meal-kit account, no recipe names, no
// household details — the Lock Screen is readable by anyone holding the phone.
package liveactivity

import (
	"errors"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
)

// Phase is what the activity says the import is doing. It must match
// MealKitImportActivityAttributes.ContentState.Phase in the app
// (ios/Shared/MealKitImportActivityAttributes.swift).
type Phase string

// Phases.
const (
	// PhaseImporting: a worker is fetching recipe pages right now.
	PhaseImporting Phase = "importing"
	// PhaseWaiting: the job is queued — not started yet, between polite
	// batches, or waiting out a retry. The activity says "Next batch soon".
	PhaseWaiting Phase = "waiting"
	// PhaseDone: finished. The activity turns green.
	PhaseDone Phase = "done"
	// PhaseFailed: the import gave up (dead-lettered).
	PhaseFailed Phase = "failed"
	// PhaseCanceled: someone stopped it.
	PhaseCanceled Phase = "canceled"
)

// ContentState is the activity's dynamic content: exactly the JSON the app's
// ContentState decodes. It is four small fields — well under the 4 KB Live
// Activity payload limit — and carries counts only.
type ContentState struct {
	Phase Phase `json:"phase"`
	// Done is recipes handled so far; on PhaseDone it is recipes now in the
	// library from this history (new, refreshed, or already there).
	Done int `json:"done"`
	// Total is recipes on the submitted order history.
	Total int `json:"total"`
	// Failed is recipes that could not be imported.
	Failed int `json:"failed"`
}

// StateFor maps a job to what its activity shows.
func StateFor(job mealkit.Job) ContentState {
	s := ContentState{
		Done: job.RecipesDone(), Total: job.RecipesFound(), Failed: len(job.Checkpoint.Failures),
	}
	switch job.Status {
	case mealkit.JobSucceeded:
		s.Phase = PhaseDone
		s.Done = job.Checkpoint.Imported + job.Checkpoint.Updated + job.Checkpoint.Unchanged
	case mealkit.JobDead:
		s.Phase = PhaseFailed
	case mealkit.JobCanceled:
		s.Phase = PhaseCanceled
	case mealkit.JobQueued:
		s.Phase = PhaseWaiting
	default:
		s.Phase = PhaseImporting
	}
	if s.Done > s.Total && s.Total > 0 {
		s.Done = s.Total
	}
	return s
}

// How long each kind of ending stays on the Lock Screen. Apple keeps an ended
// activity at most four hours, so a finished import is left the whole of
// that: a three-hour import often finishes while nobody is looking.
const (
	DoneLinger   = 4 * time.Hour
	FailedLinger = time.Hour
	// A canceled import was stopped by a member on purpose; it goes at once.
	CanceledLinger = 0
)

// StaleAfter is how long an update is current. A batch lands every half
// minute or so while a run is going and the next scheduled run starts within
// about ten minutes; half an hour without word means something is wrong, and
// the activity says it is waiting for an update rather than showing a stale
// count as live.
const StaleAfter = 30 * time.Minute

// Errors.
var (
	// ErrNotFound: no active import job with that ID in this household, or no
	// token stored for it.
	ErrNotFound = errors.New("liveactivity: not found")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
