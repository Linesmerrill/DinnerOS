package events

import (
	"context"
	"log/slog"
	"time"
)

// Recorder records server-observed events. *Service implements it. Modules
// that produce behavior (ratings, recipe import, and later planning and
// pantry) depend on this interface, not on the events store.
type Recorder interface {
	// Record validates and stores e. Source defaults to SourceAPI and
	// OccurredAt to now. It returns an error wrapping ErrInvalidEvent for an
	// invalid event.
	Record(ctx context.Context, e Event) error
}

// RecordTimeout bounds how long RecordOrLog may delay the action that produced
// the event.
const RecordTimeout = 2 * time.Second

// RecordOrLog records e on a best-effort basis: a failure is logged and
// otherwise ignored, so recording never fails the user's action. It records
// even when ctx is already canceled (the client went away after the action
// succeeded), bounded by RecordTimeout. A nil Recorder does nothing.
func RecordOrLog(ctx context.Context, r Recorder, logger *slog.Logger, e Event) {
	if r == nil {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RecordTimeout)
	defer cancel()
	if err := r.Record(recordCtx, e); err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.WarnContext(ctx, "record event failed",
			"type", e.Type, "householdId", e.HouseholdID, "recipeId", e.RecipeID, "error", err)
	}
}
