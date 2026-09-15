package pantry

import (
	"context"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// CookAdjuster turns a planned meal's recipe into what the household cooked:
// package customize swaps or doubles the protein lines a member customized.
// It returns r unchanged when the entry isn't customized or can't be found.
type CookAdjuster interface {
	AdjustCookedRecipe(ctx context.Context, householdID, entryID string, occurredAt time.Time, r recipes.Recipe) (recipes.Recipe, error)
}

// SetCookAdjuster makes cook deductions of planned meals use their
// customizations through a, and returns s.
func (s *Service) SetCookAdjuster(a CookAdjuster) *Service {
	s.adjuster = a
	return s
}

// adjustCooked applies the adjuster to a planned meal. A failure fails the
// deduction instead of deducting what may not have been cooked.
func (s *Service) adjustCooked(ctx context.Context, m CookedMeal, r recipes.Recipe) (recipes.Recipe, error) {
	if s.adjuster == nil || m.EntryID == "" {
		return r, nil
	}
	adjusted, err := s.adjuster.AdjustCookedRecipe(ctx, m.HouseholdID, m.EntryID, m.OccurredAt, r)
	if err != nil {
		return recipes.Recipe{}, fmt.Errorf("apply meal customizations: %w", err)
	}
	return adjusted, nil
}
