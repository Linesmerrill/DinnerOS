package recommendations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// historyWeeks is how far back cooked and skipped events are read. Plans are
// read for planning.MaxRangeWeeks weeks; order history is read in full from
// the recipes (it is small).
const historyWeeks = 52

// Learning reads the household's feedback events for learningWeeks (the
// provider's window) and at most learningEventLimit of them, newest first.
const (
	learningWeeks      = 26
	learningEventLimit = 5000
)

// learningEventTypes are the events learning reads, besides cooked and
// skipped meals.
var learningEventTypes = []events.Type{
	events.TypeMealSwapped, events.TypeMealRejected, events.TypeRecipePlanned, events.TypeRecipeUnplanned,
	events.TypeRecipeViewed, events.TypeRecipeRated, events.TypeAutopilotLearningReset,
}

// inputData keeps what the adapter needs to turn provider results back into
// proposals.
type inputData struct {
	byID map[string]recipes.Recipe
	// context is the context the week was planned with.
	context ProposalContext
	// resetAt and resetBy are the household's last learning reset within
	// the learning window.
	resetAt time.Time
	resetBy string
}

// slot converts a provider slot into a proposal slot.
func (d inputData) slot(s autopilot.Slot) Slot {
	r := d.byID[s.ItemID]
	out := Slot{
		ID: string(s.Day), Day: string(s.Day), RecipeID: s.ItemID, RecipeName: r.Name, RecipeImageURL: r.ImageURL,
		CookMinutes: r.CookMinutes(), TimeBand: string(s.TimeBand), Servings: s.Servings, Score: s.Score, Signals: s.Signals,
	}
	for _, reason := range s.Reasons {
		out.Reasons = append(out.Reasons, Reason{Code: reason.Code, Text: reason.Text})
	}
	return out
}

// buildInput loads the household's data and translates it into the provider's
// generic input. plan is the week being planned; its entries become fixed
// meals. device are the signals a member's device sent for the week.
func (s *Service) buildInput(ctx context.Context, householdID string, w planning.Week, profile Profile, wc WeekContext, plan planning.Plan, device DeviceSignals) (autopilot.Input, inputData, error) {
	household, err := s.households.GetHousehold(ctx, householdID)
	if err != nil {
		return autopilot.Input{}, inputData{}, fmt.Errorf("load household: %w", err)
	}
	catalog, err := s.recipes.Catalog(ctx, householdID)
	if err != nil {
		return autopilot.Input{}, inputData{}, fmt.Errorf("load catalog: %w", err)
	}
	overrideList, err := s.store.ListOverrides(ctx, householdID)
	if err != nil {
		return autopilot.Input{}, inputData{}, fmt.Errorf("load recipe overrides: %w", err)
	}
	ratingList, err := s.ratings.HouseholdRatings(ctx, householdID)
	if err != nil {
		return autopilot.Input{}, inputData{}, fmt.Errorf("load ratings: %w", err)
	}
	// Learning's reads run alongside the rest, so they add little latency.
	type learningRead struct {
		feedback []events.Event
		busy     []string
		err      error
	}
	learningDone := make(chan learningRead, 1)
	go func() {
		var r learningRead
		r.feedback, r.err = s.events.List(ctx, events.Query{
			HouseholdID: householdID, Types: learningEventTypes,
			Since: w.Monday().AddDate(0, 0, -7*learningWeeks), Limit: learningEventLimit, Newest: true,
		})
		if r.err != nil {
			r.err = fmt.Errorf("load feedback events: %w", r.err)
		} else if r.busy, r.err = s.store.BusyWeeks(ctx, householdID, w.AddWeeks(-historyWeeks).String(), w.AddWeeks(-1).String()); r.err != nil {
			r.err = fmt.Errorf("load busy weeks: %w", r.err)
		}
		learningDone <- r
	}()
	plans, err := s.plans.ListPlans(ctx, householdID, w.AddWeeks(-(planning.MaxRangeWeeks - 1)).String(), w.String())
	if err != nil {
		<-learningDone
		return autopilot.Input{}, inputData{}, fmt.Errorf("load plans: %w", err)
	}
	outcomes, err := s.events.List(ctx, events.Query{
		HouseholdID: householdID, Types: []events.Type{events.TypeRecipeCooked, events.TypeRecipeSkipped},
		Since: w.Monday().AddDate(0, 0, -7*historyWeeks), Limit: events.MaxListLimit,
	})
	if err != nil {
		<-learningDone
		return autopilot.Input{}, inputData{}, fmt.Errorf("load cooked and skipped events: %w", err)
	}
	learningRes := <-learningDone
	if learningRes.err != nil {
		return autopilot.Input{}, inputData{}, learningRes.err
	}
	feedback, busyList := learningRes.feedback, learningRes.busy
	busy := make(map[string]bool, len(busyList))
	for _, week := range busyList {
		busy[week] = true
	}

	overrides := make(map[string]RecipeOverride, len(overrideList))
	for _, o := range overrideList {
		overrides[o.RecipeID] = o
	}
	in := autopilot.Input{
		HouseholdID: householdID,
		Week:        w.String(),
		Preferences: profile.preferences(household.DefaultServings),
		Context:     wc.providerContext(s.pantryLow(ctx, householdID)),
	}
	data := inputData{byID: make(map[string]recipes.Recipe, len(catalog)), context: weekContext(w, household.TimeZone, household.OrderDay, device)}
	data.context.apply(&in.Context)
	bands := profile.bands()
	addonIDs := map[string]bool{}
	for _, r := range catalog {
		if r.IsAddon {
			addonIDs[r.ID] = true
			continue
		}
		var o *RecipeOverride
		if found, ok := overrides[r.ID]; ok {
			o = &found
		}
		in.Catalog = append(in.Catalog, attributes(r, o, bands).item(r))
		data.byID[r.ID] = r
		for _, week := range r.OrderWeeks {
			in.History = append(in.History, autopilot.Interaction{ItemID: r.ID, Kind: autopilot.KindOrdered, Week: week})
		}
	}
	for _, rating := range ratingList {
		tags := make([]string, 0, len(rating.Tags))
		for _, t := range rating.Tags {
			tags = append(tags, string(t))
		}
		in.Ratings = append(in.Ratings, autopilot.Rating{ItemID: rating.RecipeID, MemberID: rating.UserID, Score: rating.Score, Tags: tags})
	}
	for _, p := range plans {
		if p.Week == w {
			continue
		}
		for _, e := range p.Entries {
			in.History = append(in.History, autopilot.Interaction{ItemID: e.RecipeID, Kind: autopilot.KindPlanned, Week: p.Week.String(), Day: autopilot.Day(e.Day)})
		}
	}
	loc, err := time.LoadLocation(household.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	for _, e := range outcomes {
		kind, date, reason := autopilot.KindCooked, "", ""
		switch payload := e.Payload.(type) {
		case events.RecipeCooked:
			date = payload.Date
		case events.RecipeSkipped:
			kind, date, reason = autopilot.KindSkipped, payload.Date, payload.Reason
		}
		week := e.Week
		if week == "" {
			day, err := time.Parse(time.DateOnly, date)
			if err != nil {
				day = e.OccurredAt.In(loc)
			}
			week = planning.WeekOf(day).String()
		}
		in.History = append(in.History, autopilot.Interaction{ItemID: e.RecipeID, Kind: kind, Week: week, Reason: reason, At: e.OccurredAt, Busy: busy[week]})
	}
	s.addFeedback(&in, &data, feedback, loc)
	for _, e := range plan.Entries {
		// Add-ons (a pairing's garlic bread) go with a meal; they don't take
		// its day or count as a meal.
		if addonIDs[e.RecipeID] {
			continue
		}
		in.Fixed = append(in.Fixed, autopilot.Assignment{ItemID: e.RecipeID, Day: autopilot.Day(e.Day)})
	}
	return in, data, nil
}

// addFeedback turns the household's feedback events into interactions the
// provider learns from, and finds the last learning reset. Events are newest
// first; when the read hit its limit, learning starts at the oldest event
// read, so a reset that fell past the limit can't let older feedback back in.
func (s *Service) addFeedback(in *autopilot.Input, data *inputData, list []events.Event, loc *time.Location) {
	if len(list) >= learningEventLimit {
		in.LearningSince = list[len(list)-1].OccurredAt
	}
	for _, e := range list {
		week := e.Week
		if week == "" {
			week = planning.WeekOf(e.OccurredAt.In(loc)).String()
		}
		h := autopilot.Interaction{ItemID: e.RecipeID, Week: week, At: e.OccurredAt}
		switch payload := e.Payload.(type) {
		case events.AutopilotLearningReset:
			if data.resetAt.IsZero() {
				data.resetAt, data.resetBy = e.OccurredAt, e.UserID
				if e.OccurredAt.After(in.LearningSince) {
					in.LearningSince = e.OccurredAt
				}
			}
			continue
		case events.MealSwapped:
			// The event's recipe is the one Autopilot offered next; only the
			// meal swapped out was the member's choice.
			h.ItemID, h.Kind = payload.PreviousRecipeID, autopilot.KindSwappedOut
		case events.MealRejected:
			h.Kind = autopilot.KindRejected
		case events.RecipePlanned:
			if payload.Origin != string(planning.OriginAutopilot) {
				continue
			}
			h.Kind = autopilot.KindAccepted
		case events.RecipeUnplanned:
			if payload.Origin != string(planning.OriginAutopilot) {
				continue
			}
			h.Kind = autopilot.KindRejected
		case events.RecipeViewed:
			// Seeing a meal on the plan isn't interest in it.
			if payload.Surface == "plan" {
				continue
			}
			h.Kind = autopilot.KindViewed
		case events.RecipeRated:
			h.Kind, h.Score = autopilot.KindRated, payload.Score
		default:
			continue
		}
		if h.ItemID != "" {
			in.History = append(in.History, h)
		}
	}
}

// pantryLow returns the names of pantry items running low. The pantry is an
// optional, cheap signal: failures are logged and ignored.
func (s *Service) pantryLow(ctx context.Context, householdID string) []string {
	if s.pantry == nil {
		return nil
	}
	items, err := s.pantry.List(ctx, householdID, pantry.ListQuery{Status: string(pantry.StatusLow)})
	if err != nil {
		s.logger.WarnContext(ctx, "load low pantry items for autopilot", "householdId", householdID, "error", err)
		return nil
	}
	var names []string
	for _, it := range items {
		if name := normalizeValue(it.DisplayName); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// inputsHash fingerprints a provider request: identical inputs, attempt, and
// avoid list give the same hash.
func inputsHash(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// newID returns a random 24-hex-digit identifier, the same shape as the
// ObjectIDs used elsewhere in the API.
func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
