package shopping

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file decides which confirmed handoff lines become pantry purchases
// (docs/pantry-usage.md#what-goes-in-the-pantry).
//
// A tub of sour cream bought for two tablespoons lasts for weeks; a pound of
// ground beef bought for a pound of ground beef doesn't. Putting the beef in
// the pantry would leave it "in stock" and keep it off next week's list, so
// the rule tracks what is left over and skips what the week's meals use up:
//
//   - per_amount lines (rice, oil, spices, sauces, and anything a member
//     counts by measure) are always tracked.
//   - per_week lines (produce, meat, dairy, bakery, deli) are tracked only
//     when the leftover can be measured and is at least MinLeftoverPercent of
//     what was bought: the package size is known, all of the week's need
//     converts to it (exactly, or by typical density), and there's plenty
//     left. Sour cream, a dozen eggs, a bag of shredded cheese.
//   - meat and seafood are never tracked: what's left is frozen or thrown
//     out, not kept on a shelf, and "in stock" would hide it from the list.
//
// A line that isn't tracked is still confirmed, and the week's Shop tab
// leaves it out as "Ordered this week" instead of "In your pantry".

// MinLeftoverPercent is the smallest measured leftover, as a percent of what
// a per_week line bought, that puts the line in the pantry.
const MinLeftoverPercent = 25

// Leftover is what a line's packages hold against the week's need, in the
// package size's unit.
type Leftover struct {
	// Measured is false when the need can't be measured against the package:
	// no size, or amounts that don't convert.
	Measured bool
	Bought   *big.Rat
	Needed   *big.Rat
}

// UsedFraction is the share of what was bought that the week needs, between
// 0 and 1, or nil when it isn't measured.
func (l Leftover) UsedFraction() *big.Rat {
	if !l.Measured || l.Bought.Sign() <= 0 {
		return nil
	}
	f := new(big.Rat).Quo(l.Needed, l.Bought)
	if f.Cmp(big.NewRat(1, 1)) > 0 {
		f.SetInt64(1)
	}
	return f
}

// leftoverOf measures packages of the line's product against its stored
// amounts.
func leftoverOf(l HandoffLine, packages int) Leftover {
	size := amountFromSize(l.PackageSize)
	if size == nil || packages <= 0 {
		return Leftover{}
	}
	count := l.PackageCount()
	if count.Needed == nil || len(count.Unconverted) > 0 {
		return Leftover{}
	}
	bought := new(big.Rat).Mul(size.Quantity.Rat(), big.NewRat(int64(packages), 1))
	return Leftover{Measured: true, Bought: bought, Needed: count.Needed.Quantity.Rat()}
}

// pantryTrackingFor applies the rule above to packages of a confirmed line.
func pantryTrackingFor(l HandoffLine, packages int) PantryTracking {
	if l.Coverage != providers.CoveragePerWeek {
		return PantryTracked
	}
	if l.Category == ingredients.CategoryMeatSeafood {
		return PantryNotTracked
	}
	left := leftoverOf(l, packages)
	if !left.Measured {
		return PantryNotTracked
	}
	remaining := new(big.Rat).Sub(left.Bought, left.Needed)
	// remaining ÷ bought ≥ MinLeftoverPercent ÷ 100
	if new(big.Rat).Mul(remaining, big.NewRat(100, 1)).Cmp(new(big.Rat).Mul(left.Bought, big.NewRat(MinLeftoverPercent, 1))) >= 0 {
		return PantryTracked
	}
	return PantryNotTracked
}

// orderedKeys returns the ingredient keys this week's confirmed handoff lines
// bought fresh without tracking them, for ExcludedOrdered.
func (s *Service) orderedKeys(ctx context.Context, householdID, week string, provider providers.Key) (map[string]bool, error) {
	list, err := s.store.ListHandoffs(ctx, householdID, HandoffFilter{Week: week, Limit: MaxHandoffList})
	if err != nil {
		return nil, fmt.Errorf("list the week's handoffs: %w", err)
	}
	out := map[string]bool{}
	for _, h := range list {
		if h.Provider != provider {
			continue
		}
		for _, l := range h.Lines {
			if l.Status == LineConfirmed && l.Pantry == PantryNotTracked {
				out[l.IngredientKey] = true
			}
		}
	}
	return out, nil
}
