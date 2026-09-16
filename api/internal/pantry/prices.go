package pantry

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// This file holds purchase prices and what shopping's weekly cost reads from
// the pantry (docs/grocery-engine.md#weekly-cost). Money is integer US cents.
// A price is optional everywhere: a purchase without one is simply left out
// of cost math, and the cost says how many items it was based on.

// MaxPriceCents bounds one purchase's price: $10,000.
const MaxPriceCents = 1_000_000

// validatePrice accepts nil (no price) or 0…MaxPriceCents.
func validatePrice(cents *int64) error {
	if cents != nil && (*cents < 0 || *cents > MaxPriceCents) {
		return invalid("priceCents must be between 0 and %d", MaxPriceCents)
	}
	return nil
}

// SetPurchasePrice sets what a purchase cost, or clears it with nil. Prices
// often arrive after the purchase: typed in later, or read from an order
// screenshot on the phone.
func (s *Service) SetPurchasePrice(ctx context.Context, actor households.Membership, purchaseID string, priceCents *int64) (Purchase, error) {
	if err := authorizeEdit(actor); err != nil {
		return Purchase{}, err
	}
	if s.usage == nil {
		return Purchase{}, errUsageNotConfigured
	}
	if err := validatePrice(priceCents); err != nil {
		return Purchase{}, err
	}
	if strings.TrimSpace(purchaseID) == "" {
		return Purchase{}, ErrNotFound
	}
	p, err := s.usage.SetPurchasePrice(ctx, actor.HouseholdID, purchaseID, priceCents)
	if err != nil {
		return Purchase{}, err
	}
	s.logger.InfoContext(ctx, "pantry purchase priced", "householdId", actor.HouseholdID, "purchaseId", p.ID, "priced", priceCents != nil)
	return p, nil
}

// PurchasesByIDs returns the household's purchases with these IDs.
func (s *Service) PurchasesByIDs(ctx context.Context, householdID string, ids []string) ([]Purchase, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if s.usage == nil {
		return nil, errUsageNotConfigured
	}
	if len(ids) == 0 {
		return nil, nil
	}
	list, err := s.usage.PurchasesByIDs(ctx, householdID, ids)
	if err != nil {
		return nil, fmt.Errorf("list pantry purchases by id: %w", err)
	}
	return list, nil
}

// CookUsageForEntries returns what cooking these plan entries deducted.
func (s *Service) CookUsageForEntries(ctx context.Context, householdID string, entryIDs []string) ([]CookUsage, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if s.usage == nil {
		return nil, errUsageNotConfigured
	}
	if len(entryIDs) == 0 {
		return nil, nil
	}
	list, err := s.usage.ListCookUsageByEntries(ctx, householdID, entryIDs)
	if err != nil {
		return nil, fmt.Errorf("list cook usage: %w", err)
	}
	return list, nil
}

// StockValue is what is left of one purchase, by the pantry's estimate.
type StockValue struct {
	// Tracked is false when the item has no current cycle from this
	// purchase: it was restocked since, corrected, or never tracked.
	Tracked bool
	// Bought and Remaining are in the cycle's tracking unit. Bought is the
	// purchase alone (a carried remainder isn't part of what it cost).
	Bought    *big.Rat
	Remaining *big.Rat
	Unit      string
}

// PurchaseStock estimates what remains of a purchase that started its item's
// current cycle. A cycle that carried leftovers in holds more than the
// purchase; the remaining amount is capped at what was bought, so the
// purchase's value is never counted above its price.
func (s *Service) PurchaseStock(ctx context.Context, p Purchase) (StockValue, error) {
	item, err := s.store.GetItem(ctx, p.HouseholdID, p.ItemID)
	if err != nil {
		return StockValue{}, err
	}
	t := item.Tracking
	if t == nil || t.CycleID != p.ID || p.Quantity == "" {
		return StockValue{}, nil
	}
	bought, unit := trackingAmount(p.Quantity, p.Unit, p.UnitSize)
	if unit != t.Unit {
		converted, ok := convertAmount(bought, unit, t.Unit, item.UnitSize)
		if !ok {
			return StockValue{}, nil
		}
		bought = converted
	}
	settings, err := s.Settings(ctx, p.HouseholdID)
	if err != nil {
		settings = Settings{LowThresholdPercent: DefaultLowThresholdPercent}
	}
	e := estimateItem(item, settings.LowThresholdPercent, s.timestamp())
	if e == nil || bought.Sign() <= 0 {
		return StockValue{}, nil
	}
	remaining := new(big.Rat).Set(e.Remaining)
	if remaining.Cmp(bought) > 0 {
		remaining.Set(bought)
	}
	return StockValue{Tracked: true, Bought: bought, Remaining: remaining, Unit: t.Unit}, nil
}

// PurchaseAmount is a purchase's amount in unit, or false when it doesn't
// convert (a count without a size against ounces).
func PurchaseAmount(p Purchase, unit string) (*big.Rat, bool) {
	if p.Quantity == "" {
		return nil, false
	}
	q, u := trackingAmount(p.Quantity, p.Unit, p.UnitSize)
	return convertAmount(q, u, unit, p.UnitSize)
}
