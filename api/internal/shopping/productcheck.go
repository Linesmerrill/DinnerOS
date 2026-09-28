package shopping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// This file holds what the app reports about its checks of saved products
// (docs/shopping-providers.md#checking-saved-products).
//
// Walmart retires and renumbers items, and a cart link with a dead item ID
// adds nothing for it without saying so. So before a hand-off the member's
// phone reads each saved product's Walmart page and reports the result with
// the hand-off request. The server never fetches Walmart: it records what the
// phone saw, and it enforces the rule on every link it builds. A product the
// phone found gone is never a cart line; one it couldn't check goes in only
// when the member chose Send Anyway; either one left out is reported under
// Not Included with why.

// ProductStatus is what a check of a product's page found.
type ProductStatus string

// Product statuses.
const (
	// ProductFound: the item exists and is in stock for pickup or delivery.
	ProductFound ProductStatus = "found"
	// ProductUnavailable: the item exists but is out of stock for pickup and
	// delivery. It goes in the cart; it may come back.
	ProductUnavailable ProductStatus = "unavailable"
	// ProductGone: Walmart says the item doesn't exist (404). Never sent.
	ProductGone ProductStatus = "gone"
	// ProductUnknown: the check couldn't tell: blocked, timed out, or a page
	// it couldn't read. Never treated as found.
	ProductUnknown ProductStatus = "unknown"
)

func (s ProductStatus) valid() bool {
	return s == ProductFound || s == ProductUnavailable || s == ProductGone || s == ProductUnknown
}

// Conclusive reports whether s is an answer about the item rather than a
// failure to get one.
func (s ProductStatus) Conclusive() bool { return s != ProductUnknown && s != "" }

// CheckDecision is what the member decided about a line whose product is gone
// or couldn't be checked.
type CheckDecision string

// Check decisions. The empty decision is "not decided".
const (
	// DecisionSendAnyway: the member confirmed the item on Walmart themselves
	// and sends an unchecked product. Not allowed for a gone one.
	DecisionSendAnyway CheckDecision = "send_anyway"
	// DecisionLeaveOut: the member knowingly leaves the line out of this cart.
	DecisionLeaveOut CheckDecision = "leave_out"
)

// Limits on a report.
const (
	maxCheckNameLength = 300
	maxProductIDLength = 64
)

// ProductCheckReport is one line's check, as the app reports it with a match
// or hand-off request.
type ProductCheckReport struct {
	IngredientKey string
	// ProductID is the item the phone checked. A report for another item
	// than the one saved now (re-chosen meanwhile) counts as unknown.
	ProductID string
	Status    ProductStatus
	CheckedAt time.Time
	// Name and PriceCents are what Walmart's page showed, on found or
	// unavailable.
	Name       string
	PriceCents *int64
	Decision   CheckDecision
}

// ProductCheckState is the latest check of a saved product, stored on it so
// Saved Products can say "Needs re-choosing" without fetching anything.
type ProductCheckState struct {
	// ProductID is the item the check was for. A state for another item (the
	// member chose a new product since) is ignored.
	ProductID  string
	Status     ProductStatus
	CheckedAt  time.Time
	Name       string
	PriceCents *int64
}

// LineCheck is the check a hand-off line or exclusion went out with.
type LineCheck struct {
	Status    ProductStatus
	CheckedAt time.Time
	Decision  CheckDecision
	// Saved is true when no report came with the request and the status is
	// the saved product's last recorded check (only ever gone).
	Saved bool
}

// PriceSource says where a saved product's price came from.
type PriceSource string

// Price sources.
const (
	// PriceFromMember: typed, confirmed on "Did you order these?", or read
	// from an order screenshot. A price stored without a source reads as this.
	PriceFromMember PriceSource = "member"
	// PriceFromProvider: the price Walmart's page showed at a check.
	PriceFromProvider PriceSource = "provider"
)

// CurrentCheck returns the check state for the product saved now, or nil.
func (p Preference) CurrentCheck() *ProductCheckState {
	if p.Check == nil || p.Check.ProductID != p.ProductID {
		return nil
	}
	return p.Check
}

// NeedsRechoosing reports whether the last check found the saved product
// gone from the provider.
func (p Preference) NeedsRechoosing() bool {
	c := p.CurrentCheck()
	return c != nil && c.Status == ProductGone
}

// validateChecks validates reports and returns them by ingredient key.
func validateChecks(reports []ProductCheckReport, now time.Time) (map[string]ProductCheckReport, error) {
	if len(reports) > MaxSelectionKeys {
		return nil, invalid("productChecks holds at most %d checks", MaxSelectionKeys)
	}
	out := make(map[string]ProductCheckReport, len(reports))
	for _, r := range reports {
		key, err := normalizeIngredientKey(r.IngredientKey)
		if err != nil {
			return nil, err
		}
		if _, dup := out[key]; dup {
			return nil, invalid("productChecks lists %s more than once", key)
		}
		r.IngredientKey = key
		r.ProductID = strings.TrimSpace(r.ProductID)
		switch {
		case r.ProductID == "" || len(r.ProductID) > maxProductIDLength:
			return nil, invalid("productChecks: productId is required, at most %d characters", maxProductIDLength)
		case !r.Status.valid():
			return nil, invalid("productChecks: status must be found, unavailable, gone, or unknown")
		case r.Decision != "" && r.Decision != DecisionSendAnyway && r.Decision != DecisionLeaveOut:
			return nil, invalid("productChecks: decision must be send_anyway or leave_out")
		case r.Status == ProductGone && r.Decision == DecisionSendAnyway:
			return nil, invalid("productChecks: a product the provider no longer lists can't be sent; re-choose it or leave it out")
		case r.CheckedAt.IsZero():
			return nil, invalid("productChecks: checkedAt is required")
		case r.PriceCents != nil && (*r.PriceCents < 0 || *r.PriceCents > MaxPriceCents):
			return nil, invalid("productChecks: priceCents must be between 0 and %d", MaxPriceCents)
		}
		// A phone's clock can run ahead; nothing is dated after it reached us.
		if r.CheckedAt.After(now) {
			r.CheckedAt = now
		}
		r.CheckedAt = r.CheckedAt.UTC()
		r.Name = strings.Join(strings.Fields(r.Name), " ")
		if len(r.Name) > maxCheckNameLength {
			r.Name = r.Name[:maxCheckNameLength]
		}
		if r.Status != ProductFound && r.Status != ProductUnavailable {
			r.Name, r.PriceCents = "", nil
		}
		out[key] = r
	}
	return out, nil
}

// lineCheck is the check that applies to a line whose saved product is pref:
// the app's report when there is one, else the saved product's last check
// when it found the product gone, else nil (nothing known). A report for a
// product other than the one saved now is unknown and undecided: the member
// re-chose it since, and the new product hasn't been checked.
func lineCheck(pref Preference, reports map[string]ProductCheckReport) *LineCheck {
	if r, ok := reports[pref.IngredientKey]; ok {
		if r.ProductID != pref.ProductID {
			return &LineCheck{Status: ProductUnknown, CheckedAt: r.CheckedAt}
		}
		return &LineCheck{Status: r.Status, CheckedAt: r.CheckedAt, Decision: r.Decision}
	}
	if c := pref.CurrentCheck(); c != nil && c.Status == ProductGone {
		return &LineCheck{Status: ProductGone, CheckedAt: c.CheckedAt, Saved: true}
	}
	return nil
}

// blocks reports whether a line with this check can't simply be sent: its
// product is gone, or unchecked without Send Anyway, or the member left it
// out.
func (c *LineCheck) blocks() bool {
	if c == nil {
		return false
	}
	return c.Decision == DecisionLeaveOut || c.Status == ProductGone || (c.Status == ProductUnknown && c.Decision != DecisionSendAnyway)
}

// checkExclusion is the exclusion reason for a line whose check blocks it.
func (c *LineCheck) checkExclusion() ExclusionReason {
	switch {
	case c.Decision == DecisionLeaveOut && c.Status == ProductGone:
		return ExcludedLeftOutGone
	case c.Decision == DecisionLeaveOut && c.Status == ProductUnknown:
		return ExcludedLeftOutUnverified
	case c.Decision == DecisionLeaveOut:
		return ExcludedByMember
	case c.Status == ProductGone:
		return ExcludedProductGone
	default:
		return ExcludedProductUnverified
	}
}

// applyCheck records a report's result on p and reports whether the price
// changed. An unknown result never replaces a conclusive one for the same
// product: it says nothing about the item. A found or unavailable result with
// a price refreshes the saved price:
//   - no saved price: Walmart's price is saved;
//   - a price from an earlier check: replaced when it changed;
//   - a price the member entered: kept, unless Walmart's own price changed
//     since the previous check, which means the member's number is older
//     than a real price change. The first check never overrides it.
func applyCheck(p Preference, r ProductCheckReport) (Preference, bool) {
	prev := p.CurrentCheck()
	if !r.Status.Conclusive() && prev != nil && prev.Status.Conclusive() {
		return p, false
	}
	next := ProductCheckState{ProductID: p.ProductID, Status: r.Status, CheckedAt: r.CheckedAt, Name: r.Name, PriceCents: r.PriceCents}
	if r.Status == ProductGone && prev != nil {
		// Keep what the item was called, for the member re-choosing it.
		next.Name = prev.Name
	}
	priceChanged := false
	if r.PriceCents != nil && (r.Status == ProductFound || r.Status == ProductUnavailable) {
		listed := *r.PriceCents
		var prevListed *int64
		if prev != nil {
			prevListed = prev.PriceCents
		}
		switch {
		case p.PriceCents == nil:
			priceChanged = true
		case p.PriceSource == PriceFromProvider:
			priceChanged = *p.PriceCents != listed
		default:
			priceChanged = prevListed != nil && *prevListed != listed
		}
		if priceChanged {
			p.PriceCents, p.PriceSource, p.PriceUpdatedAt = &listed, PriceFromProvider, r.CheckedAt
		}
	} else if r.PriceCents == nil && prev != nil && r.Status.Conclusive() && r.Status != ProductGone {
		next.PriceCents = prev.PriceCents
	}
	p.Check = &next
	return p, priceChanged
}

// recordChecks stores the reported results on the household's saved products.
// A report for a product that isn't the one saved now is ignored.
func (s *Service) recordChecks(ctx context.Context, prefs []Preference, reports map[string]ProductCheckReport) error {
	for _, p := range prefs {
		r, ok := reports[p.IngredientKey]
		if !ok || r.ProductID != p.ProductID {
			continue
		}
		next, priceChanged := applyCheck(p, r)
		if next.Check == p.Check {
			continue
		}
		if _, err := s.store.SaveProductCheck(ctx, next, priceChanged); err != nil {
			return fmt.Errorf("save product check: %w", err)
		}
	}
	return nil
}

// DecisionNeededError is returned by CreateHandoff when a line's product is
// gone, or couldn't be checked, and the member hasn't decided what to do.
// Nothing is stored or sent.
// ErrEverythingLeftOut means every line with a saved product was left out, so
// there is nothing to put in the cart. The app says so instead of opening an
// empty cart, and records nothing.
var ErrEverythingLeftOut = errors.New("shopping: every line with a saved product was left out")

type DecisionNeededError struct {
	Lines []Excluded
}

func (e *DecisionNeededError) Error() string {
	return "shopping: decide on these lines before sending: " + e.Names()
}

// Names lists the lines' names, comma separated.
func (e *DecisionNeededError) Names() string {
	names := make([]string, 0, len(e.Lines))
	for _, l := range e.Lines {
		names = append(names, l.Name)
	}
	return strings.Join(names, ", ")
}
