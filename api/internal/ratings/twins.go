package ratings

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// A member's rating is about the dish, not the household: rating Rigatoni in
// one household shows the same rating on Rigatoni in every other household
// they belong to. Each household keeps its own copy of a recipe, so the copies
// are matched by catalog key (recipes.CatalogKey).

// MembershipLister lists the households a user belongs to.
// *households.Service implements it.
type MembershipLister interface {
	ListForUser(ctx context.Context, userID string) ([]households.UserHousehold, error)
}

// RecipeMatcher finds a household's copy of another household's recipe.
// *recipes.Service implements it.
type RecipeMatcher interface {
	SameRecipeIn(ctx context.Context, fromHouseholdID, recipeID, householdID string) (string, error)
}

// recipeRef is one household's copy of a recipe.
type recipeRef struct{ householdID, recipeID string }

// twins are the actor's other households' copies of recipeID, where their role
// lets them rate. Failures are logged and skipped: the rating in the
// household they're in has already been saved.
func (s *Service) twins(ctx context.Context, actor households.Membership, recipeID string) []recipeRef {
	if s.memberships == nil || s.matcher == nil {
		return nil
	}
	list, err := s.memberships.ListForUser(ctx, actor.UserID)
	if err != nil {
		s.logger.WarnContext(ctx, "list households for rating sync", "error", err)
		return nil
	}
	var out []recipeRef
	for _, uh := range list {
		if uh.Household.ID == actor.HouseholdID || authorize(uh.Membership) != nil {
			continue
		}
		id, err := s.matcher.SameRecipeIn(ctx, actor.HouseholdID, recipeID, uh.Household.ID)
		if err != nil {
			s.logger.WarnContext(ctx, "match recipe for rating sync", "household", uh.Household.ID, "error", err)
			continue
		}
		if id != "" {
			out = append(out, recipeRef{householdID: uh.Household.ID, recipeID: id})
		}
	}
	return out
}
