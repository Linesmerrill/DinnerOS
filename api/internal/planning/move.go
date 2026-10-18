package planning

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// MovedEntry is the result of MoveEntryToWeek: both weeks as they are after
// the move, and the meal in its new week.
type MovedEntry struct {
	From  Plan
	To    Plan
	Entry Entry
}

// MoveEntryToWeek moves an entry from week to toWeek, for a meal that didn't
// get made and should be made later (decision 645). The meal keeps its
// servings, note, customizations, and origin. Add-ons on the meal's day go
// with it.
//
// day nil keeps the meal's weekday when toWeek has no meal that night, and
// otherwise leaves it without a day ("Pick a Day"). A day given takes that
// night as UpdateEntry does; "" leaves the meal without one.
//
// It records nothing: the meal was neither turned down nor newly chosen, and
// recipe.unplanned would read an Autopilot meal's move as a rejection. The
// entry gets a new ID, so a "skipped" answer on the old one stays with last
// week. Either week finalized fails with ErrFinalized.
func (s *Service) MoveEntryToWeek(ctx context.Context, householdID, week, entryID, toWeek string, day *Day) (MovedEntry, error) {
	from, err := s.parse(householdID, week)
	if err != nil {
		return MovedEntry{}, err
	}
	to, err := ParseWeek(toWeek)
	if err != nil {
		return MovedEntry{}, err
	}
	if from == to {
		return MovedEntry{}, fmt.Errorf("%w: the meal is already in that week", ErrInvalidEntry)
	}
	if day != nil && *day != "" {
		d, err := ParseDay(string(*day))
		if err != nil {
			return MovedEntry{}, err
		}
		day = &d
	}
	source, err := s.store.GetPlan(ctx, householdID, from)
	if err != nil {
		return MovedEntry{}, err
	}
	entry, ok := source.entry(entryID)
	if !ok {
		return MovedEntry{}, ErrNotFound
	}
	if source.Status == StatusFinalized {
		return MovedEntry{}, ErrFinalized
	}
	target, err := s.store.GetPlan(ctx, householdID, to)
	switch {
	case errors.Is(err, ErrNotFound):
		target = Plan{HouseholdID: householdID, Week: to, Status: StatusDraft}
	case err != nil:
		return MovedEntry{}, err
	}
	if target.Status == StatusFinalized {
		return MovedEntry{}, ErrFinalized
	}

	addon, err := s.addons(ctx, householdID, append(slices.Clone(source.Entries), target.Entries...))
	if err != nil {
		return MovedEntry{}, err
	}
	group := []Entry{entry}
	if entry.Day != "" && !addon(entry) {
		for _, e := range source.Entries {
			if e.ID != entry.ID && e.Day == entry.Day && addon(e) {
				group = append(group, e)
			}
		}
	}
	newDay := entry.Day
	if day != nil {
		newDay = *day
	} else if !addon(entry) && slices.ContainsFunc(target.Entries, func(e Entry) bool {
		return e.Day == newDay && !addon(e)
	}) {
		newDay = ""
	}

	now := s.now().UTC()
	moving := make([]Entry, 0, len(group))
	for _, e := range group {
		e.ID, e.Day, e.AddedAt = "", newDay, now
		e.Customizations = slices.Clone(e.Customizations)
		moving = append(moving, e)
	}
	var added []string
	p, err := s.written(ctx, householdID, func() (Plan, error) {
		p, ids, err := s.store.AddEntries(ctx, householdID, to, moving, MaxEntriesPerWeek, now)
		added = ids
		if err != nil {
			return p, err
		}
		return s.oneMealADay(ctx, householdID, to, p, ids[:1])
	})
	if err != nil {
		if len(added) > 0 {
			s.undoAdd(ctx, householdID, to, added, now)
		}
		return MovedEntry{}, err
	}
	for _, e := range group {
		if _, err := s.store.DeleteEntry(ctx, householdID, from, e.ID, now); err != nil && !errors.Is(err, ErrNotFound) {
			// Two copies of a meal is worse than none moved: take the new ones back out.
			s.undoAdd(ctx, householdID, to, added, now)
			return MovedEntry{}, err
		}
	}
	left, err := s.getOrEmpty(ctx, householdID, from)
	if err != nil {
		return MovedEntry{}, err
	}
	moved, ok := p.entry(added[0])
	if !ok {
		return MovedEntry{}, fmt.Errorf("planning: moved entry %s missing from plan", added[0])
	}
	return MovedEntry{From: left, To: p, Entry: moved}, nil
}

// undoAdd removes entries a failed move added. It's best effort: the move
// already failed, and that error is the one to report.
func (s *Service) undoAdd(ctx context.Context, householdID string, w Week, ids []string, now time.Time) {
	for _, id := range ids {
		if _, err := s.store.DeleteEntry(ctx, householdID, w, id, now); err != nil && s.logger != nil {
			s.logger.WarnContext(ctx, "planning: undo a failed move", "householdId", householdID, "entryId", id, "error", err)
		}
	}
}

// addons returns whether an entry is an add-on, by its own mark or, for
// entries stored before add-ons were marked, by its recipe.
func (s *Service) addons(ctx context.Context, householdID string, entries []Entry) (func(Entry) bool, error) {
	var ids []string
	for _, e := range entries {
		if !e.RecipeIsAddon && !slices.Contains(ids, e.RecipeID) {
			ids = append(ids, e.RecipeID)
		}
	}
	addon := map[string]bool{}
	if len(ids) > 0 {
		live, err := s.recipes.GetMany(ctx, householdID, ids)
		if err != nil {
			return nil, fmt.Errorf("planning: load recipes: %w", err)
		}
		for _, r := range live {
			addon[r.ID] = r.IsAddon
		}
	}
	return func(e Entry) bool { return e.RecipeIsAddon || addon[e.RecipeID] }, nil
}
