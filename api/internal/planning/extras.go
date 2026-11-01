package planning

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// ExtrasSource provides a week's extra grocery lines: items no plan entry's
// recipe asks for, such as a grocery item Autopilot pairs with a meal ("Club
// crackers for Chicken Noodle Soup") or one a member added. Each line carries
// Extra (its ID, origin, and text) and, for a pairing, Sources (the recipes it
// is for). *recommendations.Service (pairings) and *shopping.Service (added
// items) implement it.
type ExtrasSource interface {
	GroceryExtras(ctx context.Context, plan Plan) ([]grocery.Line, error)
}

// extrasSelectionID names the selection that holds extra lines. Its lines
// always carry their own Sources, so it never appears on the list.
const extrasSelectionID = "extras"

// WithExtras makes GroceryList add the week's extra grocery lines from
// sources, and returns s.
func (s *Service) WithExtras(sources ...ExtrasSource) *Service {
	s.extras = append(s.extras, sources...)
	return s
}

// appendExtras adds the week's extra lines as one selection. A failure to
// load them fails the list, like the pantry and specialties. A week with no
// meals still asks: an added item needs none.
func (s *Service) appendExtras(ctx context.Context, p Plan, selections []grocery.RecipeSelection) ([]grocery.RecipeSelection, error) {
	var lines []grocery.Line
	for _, source := range s.extras {
		got, err := source.GroceryExtras(ctx, p)
		if err != nil {
			return selections, err
		}
		lines = append(lines, got...)
	}
	if len(lines) == 0 {
		return selections, nil
	}
	return append(selections, grocery.RecipeSelection{
		RecipeID: extrasSelectionID, RecipeServings: 1, TargetServings: 1, Lines: lines,
	}), nil
}
