package pantry

import (
	"context"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
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

// LeftOutSource says what a household leaves out of a recipe when it cooks
// it (package skips): the ingredients it skipped for that recipe or for every
// recipe. A week skip is about buying and doesn't count.
type LeftOutSource interface {
	RecipeLeftOut(ctx context.Context, householdID, recipeID string) (grocery.LeftOutSet, error)
}

// SetLeftOut makes cook deductions skip the ingredients the household leaves
// out of a recipe, and returns s: cilantro left out of the curry was never
// used, so the pantry shouldn't think it was.
func (s *Service) SetLeftOut(l LeftOutSource) *Service {
	s.leftOut = l
	return s
}

// withoutLeftOut drops the ingredients the household leaves out of r. A
// failure fails the deduction, for the same reason adjustCooked does.
func (s *Service) withoutLeftOut(ctx context.Context, householdID string, r recipes.Recipe) (recipes.Recipe, error) {
	if s.leftOut == nil {
		return r, nil
	}
	set, err := s.leftOut.RecipeLeftOut(ctx, householdID, r.ID)
	if err != nil {
		return recipes.Recipe{}, fmt.Errorf("load left-out ingredients: %w", err)
	}
	if len(set) == 0 {
		return r, nil
	}
	kept := make([]recipes.RecipeIngredient, 0, len(r.Ingredients))
	for _, ing := range r.Ingredients {
		name := "name:" + ingredients.NormalizeName(ing.Name)
		if _, ok := set.Lookup(ing.IngredientID, name); ok {
			continue
		}
		kept = append(kept, ing)
	}
	r.Ingredients = kept
	return r, nil
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
