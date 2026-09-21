package shopping

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// This file finds the lines where the store's smallest package is far bigger
// than the week needs, and offers the two ways out
// (docs/shopping-providers.md#bulk-packs).
//
// The package math already computes both halves of the question. Coverage
// (coverage.go) decides whether a package is bought for the week or by exact
// measure, and leftoverOf (leftovers.go) measures what a line's packages hold
// against what the week's meals use. A bulk pack is what happens when those
// two disagree loudly: a per_week line whose packages hold far more than the
// week needs, and which the pantry isn't tracking — because a four-pound pork
// loin bought for ten ounces is not "in stock on a shelf", and the leftovers
// rule is right to refuse it.
//
// What is left is a real 54 ounces of pork with nowhere to go. So:
//
//  1. Plan another meal. Autopilot ranks the household's own recipes that use
//     the same ingredient for the week's open day (LeftoverPicks). Accepting
//     one is an ordinary plan change; nothing here writes the plan.
//  2. Freeze it. Portion, seal, and record it as a frozen pantry item with
//     the amount actually sealed (pantry.Service.Freeze). Later weeks then
//     read it from the freezer instead of buying another loin.
//
// Both are offers. A member who does neither loses nothing they had before.

// MinBulkSurplusPercent is the share of a line's packages the week must leave
// unused before it counts as a bulk pack. Half is the threshold because below
// it the surplus is a portion, not a second meal: a 16 oz pack for 10 oz is
// ordinary shopping, and a 64 oz pack for 10 oz is not.
const MinBulkSurplusPercent = 50

// BulkPack is one confirmed-or-pending handoff line whose packages hold far
// more than the week needs.
type BulkPack struct {
	LineSource
	LineID      string
	ProductID   string
	ProductName string
	PackageSize *PackageSize
	// Packages is what the line bought (confirmed when it is, else what the
	// links asked for).
	Packages int
	// Unit is the package size's unit; every amount below is in it.
	Unit string
	// Bought, Needed, and Surplus are exact amounts ("27/2").
	Bought  string
	Needed  string
	Surplus string
	// SurplusPercent is Surplus ÷ Bought as a whole percent.
	SurplusPercent int
	// Freezable is true when the category is one that freezes well, so
	// "portion and seal it" is worth offering.
	Freezable bool
	// Frozen is true when this line's remainder is already in the freezer.
	Frozen bool
	// Suggestions are recipes to plan later in the same week, best first.
	// Empty when the household has none, or the week has no open day.
	Suggestions []recommendations.LeftoverPick
}

// BulkPacks are a handoff's bulk packs, with the week they belong to.
type BulkPacks struct {
	HouseholdID string
	Week        string
	Provider    providers.Key
	HandoffID   string
	Packs       []BulkPack
}

// freezableCategories freeze and come back as food. Produce and dairy are
// deliberately absent: a frozen bag of spinach is a different product from
// the fresh bunch a recipe asked for, and telling someone to seal sour cream
// would be bad advice given confidently.
var freezableCategories = map[string]bool{
	ingredients.CategoryMeatSeafood: true,
	ingredients.CategoryBakery:      true,
	ingredients.CategoryDeli:        true,
	ingredients.CategoryFrozen:      true,
}

// BulkPacks returns the handoff's lines whose packages leave at least
// MinBulkSurplusPercent of what they bought unused, each with the recipes
// Autopilot would plan for the rest.
//
// Suggestions are best effort: a ranking failure is logged and the pack comes
// back without them, because "you bought four pounds" is useful on its own.
func (s *Service) BulkPacks(ctx context.Context, householdID, handoffID string) (BulkPacks, error) {
	h, err := s.GetHandoff(ctx, householdID, handoffID)
	if err != nil {
		return BulkPacks{}, err
	}
	out := BulkPacks{
		HouseholdID: householdID, Week: h.Week, Provider: h.Provider, HandoffID: h.ID, Packs: []BulkPack{},
	}
	frozen, err := s.frozenLines(ctx, householdID, h.ID)
	if err != nil {
		return BulkPacks{}, err
	}
	for _, l := range h.Lines {
		pack, ok := bulkPackOf(l)
		if !ok {
			continue
		}
		pack.Frozen = frozen[l.ID]
		pack.Suggestions = s.leftoverPicks(ctx, householdID, h.Week, pack.LineSource)
		out.Packs = append(out.Packs, pack)
	}
	return out, nil
}

// bulkPackOf measures one line. ok is false when the line isn't a bulk pack:
// it was skipped, its packages carry over by measure, the amounts don't
// convert, the pantry is tracking the leftover, or the surplus is ordinary.
func bulkPackOf(l HandoffLine) (BulkPack, bool) {
	if l.Status == LineSkipped || l.Coverage != providers.CoveragePerWeek {
		return BulkPack{}, false
	}
	packages := l.Packages
	if l.Status == LineConfirmed {
		packages = l.ConfirmedPackages
	}
	if packages <= 0 {
		return BulkPack{}, false
	}
	// A tracked leftover is already recorded as pantry stock, and next week's
	// list already knows about it; there is nothing to rescue.
	if pantryTrackingFor(l, packages) == PantryTracked {
		return BulkPack{}, false
	}
	left := leftoverOf(l, packages)
	if !left.Measured || left.Bought.Sign() <= 0 {
		return BulkPack{}, false
	}
	surplus := new(big.Rat).Sub(left.Bought, left.Needed)
	if surplus.Sign() <= 0 {
		return BulkPack{}, false
	}
	// surplus ÷ bought ≥ MinBulkSurplusPercent ÷ 100
	scaled := new(big.Rat).Mul(surplus, big.NewRat(100, 1))
	if scaled.Cmp(new(big.Rat).Mul(left.Bought, big.NewRat(MinBulkSurplusPercent, 1))) < 0 {
		return BulkPack{}, false
	}
	pct := new(big.Rat).Quo(scaled, left.Bought)
	pack := BulkPack{
		LineSource: l.LineSource, LineID: l.ID, ProductID: l.ProductID, ProductName: l.ProductName,
		PackageSize: l.PackageSize, Packages: packages,
		Bought: left.Bought.RatString(), Needed: left.Needed.RatString(), Surplus: surplus.RatString(),
		SurplusPercent: int(new(big.Int).Quo(pct.Num(), pct.Denom()).Int64()),
		Freezable:      freezableCategories[l.Category],
	}
	if l.PackageSize != nil {
		pack.Unit = l.PackageSize.Unit
	}
	return pack, true
}

// frozenLines are the handoff's line IDs whose remainder is already sealed.
// Without a freezer reader nothing is frozen, which is true enough.
func (s *Service) frozenLines(ctx context.Context, householdID, handoffID string) (map[string]bool, error) {
	out := map[string]bool{}
	if s.frozen == nil {
		return out, nil
	}
	stock, err := s.frozen.FrozenStock(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("list frozen pantry items: %w", err)
	}
	for _, f := range stock {
		if id, lineID, ok := splitFrozenFrom(f.Item.FrozenFrom); ok && id == handoffID {
			out[lineID] = true
		}
	}
	return out, nil
}

func splitFrozenFrom(ref string) (handoffID, lineID string, ok bool) {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			return ref[:i], ref[i+1:], i > 0 && i < len(ref)-1
		}
	}
	return "", "", false
}

// leftoverPicks asks the planner for second meals. It never fails the read:
// the pack is worth showing with no suggestions at all.
func (s *Service) leftoverPicks(ctx context.Context, householdID, week string, src LineSource) []recommendations.LeftoverPick {
	if s.leftovers == nil {
		return nil
	}
	picks, err := s.leftovers.LeftoverPicks(ctx, householdID, week, src.IngredientID(), src.Name, 0)
	if err != nil {
		s.logger.WarnContext(ctx, "bulk pack suggestions failed",
			"householdId", householdID, "week", week, "ingredientKey", src.IngredientKey, "error", err)
		return nil
	}
	return picks
}

// FrozenStockReader lists a household's frozen pantry items.
// *pantry.Service implements it.
type FrozenStockReader interface {
	FrozenStock(ctx context.Context, householdID string) ([]pantry.FrozenItem, error)
}

// LeftoverPlanner ranks recipes that use an ingredient again later in the
// same week. *recommendations.Service implements it.
type LeftoverPlanner interface {
	LeftoverPicks(
		ctx context.Context, householdID, week, ingredientID, ingredientName string, limit int,
	) ([]recommendations.LeftoverPick, error)
}
