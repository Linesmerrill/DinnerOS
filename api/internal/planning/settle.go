package planning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// A planned meal nobody answered for is assumed made (decision 662). Two
// things follow from that, both computed by the server so every phone agrees:
//
//   - When its week ends, the meal is recorded as cooked, implicitly: a
//     recipe.cooked event marked Implicit, which deducts the pantry like any
//     other. Not on the night itself — plans move, and Thursday's dinner is
//     often Saturday's — but by the time the next week is being shopped for.
//   - Until then, the grocery list for a later week already counts it: what
//     the pantry holds is reduced by what this week's unanswered meals will
//     still use, so next week's order doesn't count on food this week eats.
//
// "Didn't make it" is a recipe.skipped event, before the week ends or after:
// it takes the meal out of both, and gives back a deduction already made
// (pantry.Service.UndoCooked).

// ImplicitCookStart is the first date a meal can be assumed made. Weeks
// before it were planned when nothing was assumed, so settling them now would
// take months of meals out of the pantry at once.
const ImplicitCookStart = "2026-11-02"

// settleWeeksBack is how many ended weeks are settled. Every meal nobody
// marked "didn't make it" is assumed made, so a household that stays away
// for weeks still comes back to a pantry and grocery list that count those
// meals as cooked. ImplicitCookStart still bounds how far back that reaches.
const settleWeeksBack = 8

// implicitDinnerHour is the time an implicitly cooked meal is recorded at,
// on its day in the household's time zone.
const implicitDinnerHour = 19

// implicitClientEventID keys the implicit cook of an entry, so two servers
// settling the same week store it once.
func implicitClientEventID(entryID string) string { return "implicit-cook-" + entryID }

// settled remembers the last ended week settled per household in this
// process, so reads after the first in a week cost nothing.
type settled struct {
	mu    sync.Mutex
	weeks map[string]Week
}

func (c *settled) done(householdID string, w Week) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	got, ok := c.weeks[householdID]
	return ok && got == w
}

func (c *settled) mark(householdID string, w Week) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.weeks == nil {
		c.weeks = map[string]Week{}
	}
	c.weeks[householdID] = w
}

// log is the service's logger, or the default one.
func (s *Service) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// householdClock returns the household's current time in its time zone and
// its first day of the week. Without a household source it is UTC.
func (s *Service) householdClock(ctx context.Context, householdID string) (time.Time, Day, error) {
	first, err := s.FirstDay(ctx, householdID)
	if err != nil {
		return time.Time{}, "", err
	}
	loc := time.UTC
	if s.households != nil {
		hh, err := s.households.GetHousehold(ctx, householdID)
		if err != nil {
			return time.Time{}, "", fmt.Errorf("planning: get household: %w", err)
		}
		loc = thawLocation(hh.TimeZone)
	}
	return s.now().In(loc), first, nil
}

// SettleEndedWeeks records every unanswered meal of the household's recently
// ended weeks as cooked, implicitly. It is safe to call on every read: a
// week is settled once per process, an entry with any answer is left alone,
// and the events are keyed per entry so a second server stores nothing new.
// Without an event recorder and lister it does nothing.
func (s *Service) SettleEndedWeeks(ctx context.Context, householdID string) error {
	if s.events == nil || s.outcomes == nil || householdID == "" {
		return nil
	}
	now, first, err := s.householdClock(ctx, householdID)
	if err != nil {
		return err
	}
	last := WeekOfOn(now, first).AddWeeks(-1)
	if s.settled.done(householdID, last) {
		return nil
	}
	var errs []error
	for back := settleWeeksBack - 1; back >= 0; back-- {
		if err := s.settleWeek(ctx, householdID, last.AddWeeks(-back), first, now.Location()); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	s.settled.mark(householdID, last)
	return nil
}

func (s *Service) settleWeek(ctx context.Context, householdID string, w Week, first Day, loc *time.Location) error {
	p, err := s.store.GetPlan(ctx, householdID, w)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("planning: settle %s: %w", w, err)
	}
	p.FirstDay = first
	if len(p.Entries) == 0 {
		return nil
	}
	answered, err := s.entryAnswers(ctx, householdID, p)
	if err != nil {
		return fmt.Errorf("planning: settle %s: %w", w, err)
	}
	lastDay := DaysFrom(first)[6]
	for _, e := range p.Entries {
		if _, ok := answered[e.ID]; ok || e.RecipeID == "" || e.Servings < 1 {
			continue
		}
		date := p.DateOf(e.Day)
		if date == "" {
			date = p.DateOf(lastDay)
		}
		if date < ImplicitCookStart {
			continue
		}
		day, err := time.ParseInLocation(time.DateOnly, date, loc)
		if err != nil {
			continue
		}
		err = s.events.Record(ctx, events.Event{
			HouseholdID: householdID, Type: events.TypeRecipeCooked, RecipeID: e.RecipeID, Week: w.String(),
			Payload:       events.RecipeCooked{EntryID: e.ID, Date: date, Servings: e.Servings, Implicit: true},
			ClientEventID: implicitClientEventID(e.ID),
			OccurredAt:    day.Add(implicitDinnerHour * time.Hour),
		})
		if err != nil {
			return fmt.Errorf("planning: record implicit cook of %s: %w", e.ID, err)
		}
		s.log().InfoContext(ctx, "meal assumed made at the end of its week",
			"householdId", householdID, "week", w.String(), "entryId", e.ID, "recipeId", e.RecipeID)
	}
	return nil
}

// entryAnswers returns each of the plan's entries that has a cooked or
// skipped answer, with the newest one: true for cooked.
func (s *Service) entryAnswers(ctx context.Context, householdID string, p Plan) (map[string]bool, error) {
	start, err := time.Parse(time.DateOnly, p.DateOf(p.First()))
	if err != nil {
		return nil, err
	}
	list, err := s.outcomes.List(ctx, events.Query{
		HouseholdID: householdID, Types: []events.Type{events.TypeRecipeCooked, events.TypeRecipeSkipped},
		Since: start.Add(-outcomeLookback), Newest: true,
	})
	if err != nil {
		return nil, fmt.Errorf("load meal outcomes: %w", err)
	}
	out := map[string]bool{}
	for _, e := range list {
		var id string
		cooked := false
		switch pl := e.Payload.(type) {
		case events.RecipeCooked:
			id, cooked = pl.EntryID, true
		case events.RecipeSkipped:
			id = pl.EntryID
		}
		if _, seen := out[id]; id == "" || seen {
			continue
		}
		out[id] = cooked
	}
	return out, nil
}

// assumedMeals is what the meals of weeks before w that nobody has answered
// yet will still use, once they're made: the weeks from this one up to w,
// at most settleWeeksBack of them. Meals already marked made were taken out
// of the pantry when they were, and skipped ones won't be made, so neither
// counts. Nil when there is nothing to count.
func (s *Service) assumedMeals(ctx context.Context, householdID string, w Week) ([]grocery.Item, error) {
	if s.outcomes == nil {
		return nil, nil
	}
	now, first, err := s.householdClock(ctx, householdID)
	if err != nil {
		return nil, err
	}
	current := WeekOfOn(now, first)
	from := w.AddWeeks(-settleWeeksBack)
	if from.WeeksUntil(current) > 0 {
		from = current
	}
	var items []grocery.Item
	for pw := from; pw.WeeksUntil(w) > 0; pw = pw.AddWeeks(1) {
		p, err := s.store.GetPlan(ctx, householdID, pw)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("planning: load %s: %w", pw, err)
		}
		p.FirstDay = first
		answered, err := s.entryAnswers(ctx, householdID, p)
		if err != nil {
			return nil, fmt.Errorf("planning: %s: %w", pw, err)
		}
		open := p.Entries[:0:0]
		for _, e := range p.Entries {
			if _, ok := answered[e.ID]; !ok {
				open = append(open, e)
			}
		}
		if len(open) == 0 {
			continue
		}
		p.Entries = open
		needs, err := s.weekNeeds(ctx, householdID, p, false)
		if err != nil {
			return nil, err
		}
		list, err := grocery.AggregateWith(needs.selections, grocery.PantrySet{}, needs.skips)
		if err != nil {
			return nil, err
		}
		items = append(items, list.Items...)
	}
	return items, nil
}
