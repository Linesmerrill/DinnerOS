package planning

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// ExtrasSource provides a week's extra grocery lines: items no plan entry's
// recipe asks for, such as a grocery item Autopilot pairs with a meal ("Club
// crackers for Chicken Noodle Soup"). Each line carries Sources (the recipes
// it is for) and Extra (its ID, origin, and text).
// *recommendations.Service implements it.
type ExtrasSource interface {
	GroceryExtras(ctx context.Context, plan Plan) ([]grocery.Line, error)
}

// extrasSelectionID names the selection that holds extra lines. Its lines
// always carry their own Sources, so it never appears on the list.
const extrasSelectionID = "extras"

// WithExtras makes GroceryList add the week's extra grocery lines from source,
// and returns s.
func (s *Service) WithExtras(source ExtrasSource) *Service {
	s.extras = source
	return s
}

// appendExtras adds the week's extra lines as one selection. A failure to
// load them fails the list, like the pantry and specialties.
func (s *Service) appendExtras(ctx context.Context, p Plan, selections []grocery.RecipeSelection) ([]grocery.RecipeSelection, error) {
	if s.extras == nil || len(p.Entries) == 0 {
		return selections, nil
	}
	lines, err := s.extras.GroceryExtras(ctx, p)
	if err != nil || len(lines) == 0 {
		return selections, err
	}
	return append(selections, grocery.RecipeSelection{
		RecipeID: extrasSelectionID, RecipeServings: 1, TargetServings: 1, Lines: lines,
	}), nil
}
