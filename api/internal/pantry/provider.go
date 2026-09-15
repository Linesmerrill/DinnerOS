package pantry

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// This file holds what shopping providers (internal/shopping) need from the
// pantry: recording a member-confirmed order from a handoff as a purchase
// with source provider. Apps can't send that source (validatePurchase), so
// every provider purchase comes from a stored handoff line.

// ProviderPurchaseInput is the input for Service.RecordProviderPurchase.
type ProviderPurchaseInput struct {
	// IngredientID or Name identifies the ingredient, as for a checked-off
	// grocery line; a new ingredient is added.
	IngredientID string
	Name         string
	// Quantity and Unit are what was ordered, required: a package count
	// (unit package) or an exact amount.
	Quantity string
	Unit     string
	// UnitSize, when set, says how much one Unit holds ("1 package = 16 oz");
	// Unit must be discrete and SizeUnit a volume or weight.
	UnitSize *UnitSize
	// Week is the ISO week of the grocery list the handoff was built from.
	Week string
	// Provider names the handoff line; Key, HandoffID, and LineID are
	// required.
	Provider ProviderRef
}

// RecordProviderPurchase records that the household ordered an item through
// a shopping provider handoff: a purchase with source provider that restocks
// the item (added when missing) and starts a usage cycle, as a grocery
// check-off does. It is idempotent per handoff line: when the line already
// has a purchase, that purchase is returned with Created false and nothing
// changes.
func (s *Service) RecordProviderPurchase(ctx context.Context, actor households.Membership, in ProviderPurchaseInput) (PurchaseResult, error) {
	if err := authorizeEdit(actor); err != nil {
		return PurchaseResult{}, err
	}
	if s.usage == nil {
		return PurchaseResult{}, errUsageNotConfigured
	}
	pr := in.Provider
	if pr.Key == "" || pr.HandoffID == "" || pr.LineID == "" {
		return PurchaseResult{}, errors.New("pantry: provider purchase needs a provider, handoff, and line")
	}
	if res, found, err := s.FindProviderPurchase(ctx, actor.HouseholdID, pr.HandoffID, pr.LineID); found || err != nil {
		return res, err
	}
	p := purchase{source: PurchaseProvider, provider: &pr}
	var err error
	if p.quantity, p.unit, err = normalizeAmount(in.Quantity, in.Unit); err != nil {
		return PurchaseResult{}, err
	}
	if p.quantity == "" {
		return PurchaseResult{}, invalid("a provider purchase needs a quantity")
	}
	if in.UnitSize != nil {
		size, err := validHouseMadeSize(in.UnitSize)
		if err != nil {
			return PurchaseResult{}, err
		}
		if size.Unit != p.unit {
			return PurchaseResult{}, invalid("unitSize must describe the purchase unit %q", p.unit)
		}
		p.unitSize = size
	}
	if p.week = strings.TrimSpace(in.Week); p.week != "" && !isoWeek.MatchString(p.week) {
		return PurchaseResult{}, invalid("week must be an ISO week such as 2026-W38")
	}
	a, err := s.validateAddition(ctx, AddInput{IngredientID: in.IngredientID, Name: in.Name})
	if err != nil {
		return PurchaseResult{}, err
	}
	return s.recordPurchase(ctx, actor, p, "", a, nil)
}

// FindProviderPurchase returns the purchase recorded for a handoff line and
// the item as it is now. found is false when the line has none.
func (s *Service) FindProviderPurchase(ctx context.Context, householdID, handoffID, lineID string) (res PurchaseResult, found bool, err error) {
	if householdID == "" {
		return PurchaseResult{}, false, errHouseholdRequired
	}
	if s.usage == nil {
		return PurchaseResult{}, false, errUsageNotConfigured
	}
	existing, err := s.usage.FindPurchaseByProviderLine(ctx, householdID, handoffID, lineID)
	if errors.Is(err, ErrNotFound) {
		return PurchaseResult{}, false, nil
	}
	if err != nil {
		return PurchaseResult{}, false, fmt.Errorf("find provider purchase: %w", err)
	}
	item, err := s.store.GetItem(ctx, householdID, existing.ItemID)
	if err != nil {
		return PurchaseResult{}, true, err
	}
	return PurchaseResult{Purchase: existing, Item: item}, true, nil
}
