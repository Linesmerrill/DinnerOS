package recipes

import (
	"context"
	"fmt"
)

// IngredientUse says which of a set of catalog ingredients one recipe uses.
type IngredientUse struct {
	RecipeID      string
	IngredientIDs []string
}

// FindIngredientUse returns, for each of the household's recipes that uses
// any of the catalog ingredients ingredientIDs, which of them it uses. Other
// modules (specialty ingredients) count recipes per ingredient with it without
// loading whole recipes. It is one round trip.
func (s *Service) FindIngredientUse(ctx context.Context, householdID string, ingredientIDs []string) ([]IngredientUse, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if len(ingredientIDs) == 0 {
		return nil, nil
	}
	uses, err := s.store.FindIngredientUse(ctx, householdID, ingredientIDs)
	if err != nil {
		return nil, fmt.Errorf("find ingredient use: %w", err)
	}
	return uses, nil
}
