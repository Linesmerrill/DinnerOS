package recipes

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Deleting a recipe (decision 695) is a soft delete: the recipe leaves the
// library, search, the menu's catalog, recommendations, Autopilot, Shared
// Recipes, and this week and later weeks, but stays readable by ID, so past
// weeks and cooked history still show what was made. A restore within
// RestoreWindow puts it back, with the meals the delete took off the plan.
// After the window the recipe stays deleted for good; it is never hard
// deleted, because history reads it, and it goes with the household
// (MongoStore.PurgeHousehold) like every other recipe.

// RestoreWindow is how long a deleted recipe can be brought back.
const RestoreWindow = 30 * 24 * time.Hour

// ErrRestoreExpired means a deleted recipe is past RestoreWindow.
var ErrRestoreExpired = errors.New("recipes: too late to restore")

// UpcomingPlans changes the household's plans for a recipe delete.
// *planning.Service implements it.
type UpcomingPlans interface {
	// RemoveRecipeFromUpcoming takes the recipe off this week (from today
	// on) and every later week, and returns what it removed.
	RemoveRecipeFromUpcoming(ctx context.Context, householdID, userID, recipeID string) ([]RemovedPlanEntry, error)
	// RestoreRecipeEntries puts back meals a delete removed, skipping weeks
	// that have ended and weeks that are full.
	RestoreRecipeEntries(ctx context.Context, householdID, userID, recipeID string, entries []RemovedPlanEntry) error
}

// WithPlans wires the plans a delete changes and returns s.
func (s *Service) WithPlans(p UpcomingPlans) *Service {
	s.plans = p
	return s
}

// DeleteResult is what a delete did.
type DeleteResult struct {
	Recipe Recipe
	// RemovedFromWeeks are the ISO weeks ("2026-W47") the recipe was taken
	// off, in order, without repeats.
	RemovedFromWeeks []string
	// UndoUntil is the last moment Restore brings it back.
	UndoUntil time.Time
}

// Delete deletes one of the household's recipes. Deleting a deleted recipe
// changes nothing and answers the same. Callers authorize first: the HTTP
// route requires households.PermRecipesEdit.
func (s *Service) Delete(ctx context.Context, householdID, userID, recipeID string) (DeleteResult, error) {
	if householdID == "" {
		return DeleteResult{}, errHouseholdRequired
	}
	r, err := s.store.GetRecipe(ctx, householdID, recipeID)
	if err != nil {
		return DeleteResult{}, err
	}
	if !r.DeletedAt.IsZero() {
		return DeleteResult{Recipe: r, RemovedFromWeeks: weeksOf(r.RemovedPlanEntries), UndoUntil: r.DeletedAt.Add(RestoreWindow)}, nil
	}
	now := s.now().UTC()
	if r.SharedToCatalog {
		// Not shared again. The catalog entry stays, as when a household
		// stops sharing one recipe (Share): the catalog doesn't record who
		// shared it, and other households may share the same recipe.
		if _, err := s.store.SetRecipeShared(ctx, householdID, recipeID, false, now); err != nil {
			return DeleteResult{}, fmt.Errorf("stop sharing deleted recipe: %w", err)
		}
	}
	var removed []RemovedPlanEntry
	if s.plans != nil {
		if removed, err = s.plans.RemoveRecipeFromUpcoming(ctx, householdID, userID, recipeID); err != nil {
			return DeleteResult{}, fmt.Errorf("take deleted recipe off the plan: %w", err)
		}
	}
	r, err = s.store.SetRecipeDeleted(ctx, householdID, recipeID, now, removed, now)
	if err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{Recipe: r, RemovedFromWeeks: weeksOf(removed), UndoUntil: now.Add(RestoreWindow)}, nil
}

// Restore brings back a recipe the household deleted, and the meals the
// delete took off weeks that haven't ended. Restoring a recipe that isn't
// deleted changes nothing. Sharing stays off until the household turns it
// back on.
func (s *Service) Restore(ctx context.Context, householdID, userID, recipeID string) (Recipe, error) {
	if householdID == "" {
		return Recipe{}, errHouseholdRequired
	}
	r, err := s.store.GetRecipe(ctx, householdID, recipeID)
	if err != nil {
		return Recipe{}, err
	}
	if r.DeletedAt.IsZero() {
		return s.Get(ctx, householdID, recipeID)
	}
	now := s.now().UTC()
	if now.After(r.DeletedAt.Add(RestoreWindow)) {
		return Recipe{}, ErrRestoreExpired
	}
	if _, err := s.store.SetRecipeDeleted(ctx, householdID, recipeID, time.Time{}, nil, now); err != nil {
		return Recipe{}, err
	}
	if s.plans != nil && len(r.RemovedPlanEntries) > 0 {
		if err := s.plans.RestoreRecipeEntries(ctx, householdID, userID, recipeID, r.RemovedPlanEntries); err != nil {
			return Recipe{}, fmt.Errorf("put restored recipe back on the plan: %w", err)
		}
	}
	return s.Get(ctx, householdID, recipeID)
}

func weeksOf(entries []RemovedPlanEntry) []string {
	weeks := make([]string, 0, len(entries))
	for _, e := range entries {
		weeks = append(weeks, e.Week)
	}
	return sortedSet(weeks, "")
}
