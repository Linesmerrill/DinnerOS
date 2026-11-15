package planning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// upcomingWeeks is how far ahead a recipe delete looks for planned meals.
const upcomingWeeks = 104

// RemoveRecipeFromUpcoming takes a deleted recipe off the household's plan
// (decision 695): this week from today on, and every later week. Meals on
// days already past stay, because that's what the household made, and so
// do meals in a finalized week, whose list has gone to the store. The
// week's grocery list follows, since it is worked out from the plan. It
// implements recipes.UpcomingPlans.
func (s *Service) RemoveRecipeFromUpcoming(ctx context.Context, householdID, userID, recipeID string) ([]recipes.RemovedPlanEntry, error) {
	now, first, err := s.householdClock(ctx, householdID)
	if err != nil {
		return nil, err
	}
	current := WeekOfOn(now, first)
	today := now.Format(time.DateOnly)
	plans, err := s.store.ListPlans(ctx, householdID, current, current.AddWeeks(upcomingWeeks))
	if err != nil {
		return nil, fmt.Errorf("planning: list upcoming plans: %w", err)
	}
	var removed []recipes.RemovedPlanEntry
	for _, p := range plans {
		p.FirstDay = first
		if p.Status == StatusFinalized {
			continue
		}
		for _, e := range p.Entries {
			if e.RecipeID != recipeID {
				continue
			}
			if date := p.DateOf(e.Day); date != "" && date < today {
				continue
			}
			_, err := s.DeleteEntry(ctx, householdID, userID, p.Week.String(), e.ID)
			switch {
			case errors.Is(err, ErrNotFound), errors.Is(err, ErrFinalized):
				continue
			case err != nil:
				return removed, err
			}
			removed = append(removed, recipes.RemovedPlanEntry{
				Week: p.Week.String(), Day: string(e.Day), Servings: e.Servings, Note: e.Note, Origin: string(e.Origin),
			})
		}
	}
	return removed, nil
}

// RestoreRecipeEntries puts back the meals a recipe delete took off, for
// Undo. A meal whose week has ended, whose day has passed, or whose week is
// full or finalized now is left off. It implements recipes.UpcomingPlans.
func (s *Service) RestoreRecipeEntries(ctx context.Context, householdID, userID, recipeID string, entries []recipes.RemovedPlanEntry) error {
	now, first, err := s.householdClock(ctx, householdID)
	if err != nil {
		return err
	}
	current := WeekOfOn(now, first)
	today := now.Format(time.DateOnly)
	for _, e := range entries {
		w, err := ParseWeek(e.Week)
		if err != nil || current.WeeksUntil(w) < 0 {
			continue
		}
		if e.Day != "" {
			if d, err := ParseDay(e.Day); err != nil || w.DateOn(first, d) < today {
				continue
			}
		}
		origin := Origin(e.Origin)
		if _, err := parseOrigin(origin); err != nil {
			origin = OriginManual
		}
		_, _, err = s.AddEntries(ctx, householdID, userID, e.Week, []NewEntry{{
			RecipeID: recipeID, Day: e.Day, Servings: e.Servings, Note: e.Note, Origin: origin,
		}})
		switch {
		case errors.Is(err, ErrPlanFull), errors.Is(err, ErrFinalized), errors.Is(err, ErrInvalidEntry):
			continue
		case err != nil:
			return err
		}
	}
	return nil
}
