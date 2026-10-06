package pantry

import (
	"context"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// CookSpecialties resolves a cooked recipe's specialty ingredients to the
// household's choices, the way the grocery list does. *substitutes.Service
// implements it.
type CookSpecialties interface {
	GrocerySpecialties(ctx context.Context, householdID string, lines []grocery.Line) (grocery.Specialties, error)
}

// SetCookSpecialties makes cook deductions follow the household's specialty
// choices: a Tex-Mex Paste bought as its store alternative deducts the
// tomato paste and bouillon base that went into it. It's optional.
func (s *Service) SetCookSpecialties(c CookSpecialties) *Service {
	s.specialties = c
	return s
}

// swapSpecialties replaces each need for a specialty the household buys as a
// store alternative with that alternative's ingredients, scaled to the
// recipe's amount. A batch or as-is choice keeps the need: the batch's pantry
// item is matched by name. A failed lookup keeps every need, logged; cooking
// still deducts what it can.
func (s *Service) swapSpecialties(ctx context.Context, householdID string, needs []recipeNeed) []recipeNeed {
	if s.specialties == nil || len(needs) == 0 {
		return needs
	}
	lines := make([]grocery.Line, len(needs))
	for i, n := range needs {
		lines[i] = needLine(n)
	}
	specs, err := s.specialties.GrocerySpecialties(ctx, householdID, lines)
	if err != nil {
		s.logger.Warn("cook deduction: resolve specialties", "householdId", householdID, "error", err)
		return needs
	}
	if len(specs) == 0 {
		return needs
	}
	out := make([]recipeNeed, 0, len(needs))
	for i, n := range needs {
		parts, ok := grocery.StoreAlternativeLines(lines[i], specs[lines[i].IngredientKey])
		if !ok {
			out = append(out, n)
			continue
		}
		for _, p := range parts {
			out = append(out, componentNeed(p))
		}
	}
	return out
}

// needLine is a need as the grocery line the specialty lookup reads.
func needLine(n recipeNeed) grocery.Line {
	l := grocery.Line{IngredientKey: n.ingredientID, Name: n.name}
	if l.IngredientKey == "" {
		l.IngredientKey = UnresolvedKeyPrefix + ingredients.NormalizeName(n.name)
	}
	if n.quantity != nil {
		q := ingredients.NewQuantity(1, 1).MulRat(n.quantity)
		l.Quantity, l.UnitCode = &q, n.unit
	}
	return l
}

// componentNeed is a store alternative's ingredient as a need; a name: key is
// matched by name.
func componentNeed(l grocery.Line) recipeNeed {
	n := recipeNeed{name: l.Name}
	if !strings.HasPrefix(l.IngredientKey, UnresolvedKeyPrefix) {
		n.ingredientID = l.IngredientKey
	}
	if l.Quantity != nil && !l.Quantity.IsZero() {
		n.quantity, n.unit = l.Quantity.Rat(), l.UnitCode
	}
	return n
}
