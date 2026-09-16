package shopping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// This file holds prices: what a handoff line cost, a saved product's package
// price, and a week's order total. Money is integer US cents, and every price
// is optional. Walmart's emails carry no item prices, so they arrive from a
// member: typed in, or read on the phone from a screenshot of the order,
// which never leaves the phone (docs/shopping-providers.md#prices).

// MaxPriceCents bounds one price or order total: $10,000.
const MaxPriceCents = 1_000_000

func validatePrice(cents *int64) error {
	if cents != nil && (*cents < 0 || *cents > MaxPriceCents) {
		return invalid("priceCents must be between 0 and %d", MaxPriceCents)
	}
	return nil
}

// WeekSpend is what a household says a week's order cost in all: fees, tax,
// tip, and discounts included.
type WeekSpend struct {
	HouseholdID     string
	Week            string
	OrderTotalCents int64
	UpdatedBy       string
	UpdatedAt       time.Time
}

// LinePrice is one handoff line's price; nil PriceCents clears it.
type LinePrice struct {
	LineID     string
	PriceCents *int64
}

// SetPrices sets what handoff lines cost. A confirmed line's pantry purchase
// gets the same price, and the saved product remembers the package price
// (the line's price ÷ its packages) while it is still the product the line
// bought. It needs pantry.edit, like confirming.
func (s *Service) SetPrices(ctx context.Context, actor households.Membership, handoffID string, prices []LinePrice) (Handoff, error) {
	if err := authorize(actor, households.PermPantryEdit); err != nil {
		return Handoff{}, err
	}
	if len(prices) == 0 {
		return Handoff{}, invalid("lines needs at least one line")
	}
	if len(prices) > MaxSelectionKeys {
		return Handoff{}, invalid("lines holds at most %d lines", MaxSelectionKeys)
	}
	h, err := s.store.GetHandoff(ctx, actor.HouseholdID, handoffID)
	if err != nil {
		return Handoff{}, err
	}
	byLine := make(map[string]*int64, len(prices))
	for _, p := range prices {
		if _, ok := h.Line(p.LineID); !ok {
			return Handoff{}, invalid("line %q isn't in this handoff", p.LineID)
		}
		if _, dup := byLine[p.LineID]; dup {
			return Handoff{}, invalid("lines lists %s more than once", p.LineID)
		}
		if err := validatePrice(p.PriceCents); err != nil {
			return Handoff{}, err
		}
		byLine[p.LineID] = p.PriceCents
	}
	updated, err := s.store.SetLinePrices(ctx, actor.HouseholdID, h.ID, byLine, s.timestamp())
	if err != nil {
		return Handoff{}, fmt.Errorf("price handoff lines: %w", err)
	}
	for _, p := range prices {
		line, _ := updated.Line(p.LineID)
		if line.PurchaseID != "" {
			if _, err := s.pantry.SetPurchasePrice(ctx, actor, line.PurchaseID, p.PriceCents); err != nil && !isNotFound(err) {
				return Handoff{}, fmt.Errorf("price pantry purchase: %w", err)
			}
		}
		packages := line.Packages
		if line.Status == LineConfirmed {
			packages = line.ConfirmedPackages
		}
		s.rememberPackagePrice(ctx, updated, line, packages, p.PriceCents)
	}
	s.logger.InfoContext(ctx, "shopping prices set", "householdId", actor.HouseholdID, "handoffId", h.ID, "lines", len(prices))
	return updated, nil
}

// rememberPackagePrice saves a line's per-package price on the saved product
// when the household still buys that product for the ingredient. A cleared
// line price leaves the saved product alone: the product still costs what it
// did. Failures are logged; the line's price is what matters.
func (s *Service) rememberPackagePrice(ctx context.Context, h Handoff, line HandoffLine, packages int, priceCents *int64) {
	if priceCents == nil || packages <= 0 {
		return
	}
	each := (*priceCents + int64(packages)/2) / int64(packages)
	err := s.store.SetPreferencePrice(ctx, h.HouseholdID, h.Provider, line.IngredientKey, line.ProductID, each, s.timestamp())
	if err != nil && !errors.Is(err, ErrNotFound) {
		s.logger.WarnContext(ctx, "remember package price failed", "householdId", h.HouseholdID, "ingredientKey", line.IngredientKey, "error", err)
	}
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, pantry.ErrNotFound)
}

// SetWeekSpend records a week's order total, or clears it with nil. It needs
// shopping.edit.
func (s *Service) SetWeekSpend(ctx context.Context, actor households.Membership, week string, totalCents *int64) error {
	if err := authorize(actor, households.PermShoppingEdit); err != nil {
		return err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return err
	}
	if totalCents == nil {
		if err := s.store.DeleteWeekSpend(ctx, actor.HouseholdID, w.String()); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return nil
	}
	if *totalCents < 0 || *totalCents > MaxPriceCents {
		return invalid("orderTotalCents must be between 0 and %d", MaxPriceCents)
	}
	_, err = s.store.PutWeekSpend(ctx, WeekSpend{
		HouseholdID: actor.HouseholdID, Week: w.String(), OrderTotalCents: *totalCents, UpdatedBy: actor.UserID, UpdatedAt: s.timestamp(),
	})
	return err
}
