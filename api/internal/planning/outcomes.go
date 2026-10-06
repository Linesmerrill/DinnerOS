package planning

import (
	"context"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
)

// A planned meal's "did you make it?" answer is a household fact: one member
// taps Cooked and every phone should say so. The answer is stored as the
// recipe.cooked or recipe.skipped event the app sends, tied to the entry; the
// plan carries the newest one per entry, so every member's Menu reads it.

// EventLister reads stored events. *events.Service implements it.
type EventLister interface {
	List(ctx context.Context, q events.Query) ([]events.Event, error)
}

// Outcome kinds.
const (
	OutcomeCooked  = "cooked"
	OutcomeSkipped = "skipped"
)

// EntryOutcomeResponse is what happened to a planned meal.
type EntryOutcomeResponse struct {
	// Kind is cooked or skipped.
	Kind string `json:"kind"`
	// Reason is the skip reason, when one was given.
	Reason string `json:"reason,omitempty"`
	// By is the member who answered.
	By string    `json:"by"`
	At time.Time `json:"at"`
}

// outcomeLookback is how far before the week's first day an answer is looked
// for: a meal can be cooked early, and the plan is a week long.
const outcomeLookback = 14 * 24 * time.Hour

// withOutcomes fills each entry's answer from the household's events. A
// failed lookup leaves them out: the plan still loads, and the app keeps what
// it recorded itself.
func (h *Handler) withOutcomes(ctx context.Context, householdID string, resp *PlanResponse) {
	if h.opts.Outcomes == nil || len(resp.Entries) == 0 {
		return
	}
	start, err := time.Parse(time.DateOnly, resp.StartDate)
	if err != nil {
		return
	}
	list, err := h.opts.Outcomes.List(ctx, events.Query{
		HouseholdID: householdID, Types: []events.Type{events.TypeRecipeCooked, events.TypeRecipeSkipped},
		Since: start.Add(-outcomeLookback), Newest: true,
	})
	if err != nil {
		h.logger.WarnContext(ctx, "load meal outcomes", "error", err)
		return
	}
	newest := map[string]EntryOutcomeResponse{}
	for _, e := range list {
		var entryID string
		outcome := EntryOutcomeResponse{By: e.UserID, At: e.OccurredAt.UTC()}
		switch p := e.Payload.(type) {
		case events.RecipeCooked:
			entryID, outcome.Kind = p.EntryID, OutcomeCooked
		case events.RecipeSkipped:
			entryID, outcome.Kind, outcome.Reason = p.EntryID, OutcomeSkipped, p.Reason
		}
		if entryID == "" {
			continue
		}
		if _, seen := newest[entryID]; !seen {
			newest[entryID] = outcome
		}
	}
	for i := range resp.Entries {
		if o, ok := newest[resp.Entries[i].ID]; ok {
			resp.Entries[i].Outcome = &o
		}
	}
}
