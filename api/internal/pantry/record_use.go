package pantry

import (
	"context"
	"math/big"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// RecordUse takes what a member says they used ("2 Tbsp") off the item's
// estimate, for a cooked recipe the estimate couldn't count or anything else
// used outside a recipe. The result is a correction by a person, like setting
// the amount left: a new segment starts from it, and the member's number wins.
// An amount that converts only by density (spoons from a jar bought by weight)
// is taken off with the item's typical density. Using it all marks it out.
func (s *Service) RecordUse(ctx context.Context, actor households.Membership, id, quantity, unit string) (Item, error) {
	if err := authorizeEdit(actor); err != nil {
		return Item{}, err
	}
	quantity, unit, err := normalizeAmount(quantity, unit)
	if err != nil {
		return Item{}, err
	}
	if quantity == "" {
		return Item{}, invalid("quantity is required")
	}
	item, err := s.store.GetItem(ctx, actor.HouseholdID, id)
	if err != nil {
		return Item{}, err
	}
	settings, err := s.Settings(ctx, actor.HouseholdID)
	if err != nil {
		return Item{}, err
	}
	est := s.Estimate(item, settings)
	if est == nil || est.Remaining == nil {
		return Item{}, invalid("there's no amount to take this from; set how much is left instead")
	}
	used, ok := convertAmount(ratOf(quantity), unit, est.Unit, item.UnitSize)
	if !ok {
		used, ok = estimateAmount(ratOf(quantity), unit, est.Unit, item)
	}
	if !ok {
		return Item{}, invalid("%s doesn't convert to %s; set how much is left instead", unit, est.Unit)
	}
	left := roundRat(new(big.Rat).Sub(est.Remaining, used), 100)
	if left.Sign() <= 0 {
		out := StatusOut
		return s.Update(ctx, actor, id, UpdateInput{Status: &out})
	}
	q := ingredients.NewQuantity(left.Num().Int64(), left.Denom().Int64()).String()
	inStock := StatusInStock
	status := &inStock
	if item.Status != StatusOut {
		status = nil
	}
	return s.Update(ctx, actor, id, UpdateInput{Quantity: &q, Unit: &est.Unit, Status: status})
}
