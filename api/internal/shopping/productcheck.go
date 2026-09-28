package shopping

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file checks that saved products are still on the provider's site
// (docs/shopping-providers.md#checking-saved-products). Walmart retires and
// renumbers items; a saved item ID that no longer exists would silently fail
// to go into the cart. So every saved product carries the result of its last
// check, and a hand-off never sends an item DinnerOS knows is gone, or one it
// hasn't been able to confirm for too long, without the member deciding.
//
// The pure half is here: the stored check state, the health derived from it,
// and how a new result updates a saved product (its price included). The
// fetching is in productcheck_fetch.go and the scheduled sweep in
// productcheck_sweep.go.

// Check windows (decision #550).
const (
	// ProductFreshness is how long a product's last conclusive answer (or the
	// member choosing it) keeps it sendable. After that a product the checks
	// can't confirm is unverified and needs a decision before a hand-off. It
	// is three daily sweeps, so one missed run or a short outage at Walmart
	// doesn't stop anyone ordering.
	ProductFreshness = 72 * time.Hour
	// HandoffRecheckAge: at hand-off, a product whose last answer is older than
	// this is checked again first, within the budget below. With the daily
	// sweep most products are younger, so this is usually no requests at all.
	HandoffRecheckAge = 24 * time.Hour
	// HandoffCheckMax and HandoffCheckBudget bound the checks one hand-off
	// (or preflight) makes: a member is waiting.
	HandoffCheckMax    = 8
	HandoffCheckBudget = 15 * time.Second
	// SaveCheckBudget bounds the one check made when a member saves a product.
	SaveCheckBudget = 10 * time.Second
	// pauseAfterBlocked and pauseAfterThrottled are how long every check stops
	// after the provider refuses us (403) or asks us to slow down (429).
	pauseAfterBlocked   = 24 * time.Hour
	pauseAfterThrottled = 6 * time.Hour
)

// ProductCheckState is the last check of a saved product, stored on it.
type ProductCheckState struct {
	// ProductID is the item the check was for. A state for another item (the
	// member chose a new product since) is ignored.
	ProductID string
	// Status and Detail are the latest attempt's result, unknown included.
	Status    providers.ProductStatus
	Detail    string
	CheckedAt time.Time
	// VerifiedStatus and VerifiedAt are the latest conclusive answer (found,
	// unavailable, or gone), which an unknown attempt never overwrites.
	// VerifiedSince is when VerifiedStatus last changed.
	VerifiedStatus providers.ProductStatus
	VerifiedAt     time.Time
	VerifiedSince  time.Time
	// Name, PriceCents, Pickup, Delivery, and Store are what the provider
	// showed at the last found or unavailable answer.
	Name       string
	PriceCents *int64
	Pickup     string
	Delivery   string
	Store      string
}

// ProductHealth is what a saved product's checks mean for a hand-off.
type ProductHealth string

// Product health values.
const (
	// HealthOK: confirmed (or chosen by the member) within ProductFreshness.
	HealthOK ProductHealth = "ok"
	// HealthUnavailable: on the site, but out of stock for pickup and delivery
	// at the last check. It goes in the cart with a warning.
	HealthUnavailable ProductHealth = "unavailable"
	// HealthGone: the provider says the item doesn't exist. It is never sent;
	// the member re-chooses it or leaves the line out.
	HealthGone ProductHealth = "gone"
	// HealthUnverified: nothing has confirmed the item within
	// ProductFreshness. It is never sent without a decision.
	HealthUnverified ProductHealth = "unverified"
)

// Blocking reports whether a line with this product needs a decision before
// a hand-off.
func (h ProductHealth) Blocking() bool { return h == HealthGone || h == HealthUnverified }

// PriceSource says where a saved product's price came from.
type PriceSource string

// Price sources (decision #551).
const (
	// PriceFromMember: typed, confirmed on "Did you order these?", or imported
	// from an order screenshot. Documents without a source that have a price
	// read as this.
	PriceFromMember PriceSource = "member"
	// PriceFromProvider: the listed price a product check read.
	PriceFromProvider PriceSource = "provider"
)

// CurrentCheck returns the check state for the product saved now, or nil.
func (p Preference) CurrentCheck() *ProductCheckState {
	if p.Check == nil || p.Check.ProductID != p.ProductID {
		return nil
	}
	return p.Check
}

// basis is when the product was last known to be sendable or not: its last
// conclusive answer, or when the member chose it, whichever is later. A
// member pasting a link has just looked at the product on the site.
func (p Preference) basis() time.Time {
	b := p.ProductChosenAt
	if c := p.CurrentCheck(); c != nil && c.VerifiedAt.After(b) {
		b = c.VerifiedAt
	}
	return b
}

// Health derives the product's health at now.
func (p Preference) Health(now time.Time) ProductHealth {
	c := p.CurrentCheck()
	if c != nil && c.VerifiedStatus == providers.ProductGone {
		// Gone stays gone until a later conclusive answer replaces it; an
		// unknown attempt afterwards doesn't bring it back.
		return HealthGone
	}
	if now.Sub(p.basis()) > ProductFreshness {
		return HealthUnverified
	}
	if c != nil && c.VerifiedStatus == providers.ProductUnavailable && !c.VerifiedAt.Before(p.ProductChosenAt) {
		return HealthUnavailable
	}
	return HealthOK
}

// needsRecheck reports whether a hand-off should check the product again
// before sending it.
func (p Preference) needsRecheck(now time.Time) bool {
	return now.Sub(p.basis()) > HandoffRecheckAge
}

// applyCheck records result on p and reports whether the product just became
// gone. A found or unavailable answer with a price also refreshes the saved
// price:
//   - no saved price: the provider's price is saved;
//   - a price from an earlier check: replaced when it changed;
//   - a price from the member: kept, unless the provider's own price has
//     changed since the previous check, which means the member's number is
//     older than a real price change (decision #551).
func applyCheck(p Preference, result providers.ProductCheck, at time.Time) (Preference, bool, bool) {
	prev := p.CurrentCheck()
	next := ProductCheckState{ProductID: p.ProductID, Status: result.Status, Detail: result.Detail, CheckedAt: at}
	if prev != nil {
		next.VerifiedStatus, next.VerifiedAt, next.VerifiedSince = prev.VerifiedStatus, prev.VerifiedAt, prev.VerifiedSince
		next.Name, next.PriceCents, next.Pickup, next.Delivery, next.Store = prev.Name, prev.PriceCents, prev.Pickup, prev.Delivery, prev.Store
	}
	becameGone := false
	if result.Status.Conclusive() {
		if next.VerifiedStatus != result.Status {
			next.VerifiedSince = at
			becameGone = result.Status == providers.ProductGone
		}
		next.VerifiedStatus, next.VerifiedAt = result.Status, at
		if result.Status == providers.ProductGone {
			next.Pickup, next.Delivery = "", ""
		} else {
			next.Name, next.Pickup, next.Delivery, next.Store = result.Name, result.Pickup, result.Delivery, result.Store
			if result.PriceCents != nil {
				next.PriceCents = result.PriceCents
			}
		}
	}

	priceChanged := false
	if result.PriceCents != nil && (result.Status == providers.ProductFound || result.Status == providers.ProductUnavailable) {
		listed := *result.PriceCents
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
		if priceChanged && listed >= 0 && listed <= MaxPriceCents {
			p.PriceCents, p.PriceSource, p.PriceUpdatedAt = &listed, PriceFromProvider, at
		} else {
			priceChanged = false
		}
	}
	p.Check = &next
	return p, becameGone, priceChanged
}

// ProductChecker reads one product's page. It returns a result for every
// answer, unknown included; the error is set only when the provider refused
// us or asked us to slow down (a *CheckStop), and the caller then stops
// checking and pauses.
type ProductChecker interface {
	CheckProduct(ctx context.Context, provider providers.Key, productID string) (providers.ProductCheck, error)
}

// CheckStop means the provider refused a check (Blocked) or throttled it.
// Every check stops, and they pause for a while (see pauseAfterBlocked).
type CheckStop struct {
	Blocked bool
}

func (e *CheckStop) Error() string {
	if e.Blocked {
		return "shopping: the provider refused a product check"
	}
	return "shopping: the provider asked product checks to slow down"
}

// CheckPause is a provider's checks paused after a refusal or a throttle.
type CheckPause struct {
	Provider providers.Key
	Until    time.Time
	Reason   string
	At       time.Time
}

// checkRun is the outcome of one batch of checks.
type checkRun struct {
	Checked   int
	Found     int
	Gone      int
	Unknown   int
	Stopped   bool
	Paused    bool
	Updated   map[string]Preference // by preference ID
	NewlyGone []Preference
}

// checkProducts checks prefs one at a time, in the order given, until max
// distinct products are checked, the context ends, or the provider stops us.
// Preferences sharing a product ID share one request. Results are saved on
// each preference; a preference whose product changed meanwhile is left
// alone.
func (s *Service) checkProducts(ctx context.Context, provider providers.Key, prefs []Preference, maxProducts int) (checkRun, error) {
	run := checkRun{Updated: map[string]Preference{}}
	if s.checker == nil || len(prefs) == 0 || maxProducts <= 0 {
		return run, nil
	}
	if paused, err := s.checksPaused(ctx, provider); err != nil || paused {
		run.Paused = paused
		return run, err
	}
	byProduct := map[string][]Preference{}
	var order []string
	for _, p := range prefs {
		if _, seen := byProduct[p.ProductID]; !seen {
			order = append(order, p.ProductID)
		}
		byProduct[p.ProductID] = append(byProduct[p.ProductID], p)
	}
	for _, productID := range order {
		if run.Checked >= maxProducts || ctx.Err() != nil {
			break
		}
		result, err := s.checker.CheckProduct(ctx, provider, productID)
		if ctx.Err() != nil && !result.Status.Conclusive() {
			// The budget ran out mid-request: not an answer, and not worth
			// recording as one.
			break
		}
		run.Checked++
		switch result.Status {
		case providers.ProductFound, providers.ProductUnavailable:
			run.Found++
		case providers.ProductGone:
			run.Gone++
		default:
			run.Unknown++
		}
		at := s.timestamp()
		for _, p := range byProduct[productID] {
			next, becameGone, priceChanged := applyCheck(p, result, at)
			saved, err := s.store.SaveProductCheck(context.WithoutCancel(ctx), next, priceChanged)
			if err != nil {
				return run, fmt.Errorf("save product check: %w", err)
			}
			if !saved {
				continue // re-chosen meanwhile
			}
			run.Updated[p.ID] = next
			if becameGone {
				run.NewlyGone = append(run.NewlyGone, next)
			}
		}
		var stop *CheckStop
		if errors.As(err, &stop) {
			run.Stopped = true
			return run, s.pauseChecks(context.WithoutCancel(ctx), provider, stop)
		}
	}
	return run, nil
}

// checksPaused reports whether the provider's checks are paused now.
func (s *Service) checksPaused(ctx context.Context, provider providers.Key) (bool, error) {
	pause, err := s.store.GetCheckPause(ctx, provider)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get product check pause: %w", err)
	}
	return s.now().Before(pause.Until), nil
}

func (s *Service) pauseChecks(ctx context.Context, provider providers.Key, stop *CheckStop) error {
	now := s.timestamp()
	pause := CheckPause{Provider: provider, At: now, Until: now.Add(pauseAfterThrottled), Reason: providers.DetailThrottled}
	if stop.Blocked {
		pause.Until, pause.Reason = now.Add(pauseAfterBlocked), providers.DetailBlocked
	}
	s.logger.WarnContext(ctx, "product checks paused", "provider", provider, "reason", pause.Reason, "until", pause.Until)
	if err := s.store.SetCheckPause(ctx, pause); err != nil {
		return fmt.Errorf("pause product checks: %w", err)
	}
	return nil
}

// recheckForHandoff checks, within the hand-off budget, the saved products
// of the lines a hand-off would send or must decide about whose last answer
// is older than HandoffRecheckAge, oldest first. It returns the preferences
// with any new results applied.
func (s *Service) recheckForHandoff(ctx context.Context, provider providers.Key, prefs []Preference, proposal Proposal) ([]Preference, error) {
	if s.checker == nil {
		return prefs, nil
	}
	keys := map[string]bool{}
	for _, l := range proposal.Lines {
		keys[l.IngredientKey] = true
	}
	for _, e := range proposal.Excluded {
		if e.Reason.NeedsDecision() || e.Reason.LeftOutForProduct() {
			keys[e.IngredientKey] = true
		}
	}
	now := s.now()
	var due []Preference
	for _, p := range prefs {
		if keys[p.IngredientKey] && p.needsRecheck(now) {
			due = append(due, p)
		}
	}
	if len(due) == 0 {
		return prefs, nil
	}
	slices.SortStableFunc(due, func(a, b Preference) int { return a.basis().Compare(b.basis()) })
	budget, cancel := context.WithTimeout(ctx, HandoffCheckBudget)
	defer cancel()
	run, err := s.checkProducts(budget, provider, due, HandoffCheckMax)
	if err != nil {
		return nil, err
	}
	s.logger.InfoContext(ctx, "hand-off product checks", "provider", provider, "due", len(due), "checked", run.Checked,
		"gone", run.Gone, "unknown", run.Unknown, "stopped", run.Stopped, "paused", run.Paused)
	out := make([]Preference, len(prefs))
	for i, p := range prefs {
		if updated, ok := run.Updated[p.ID]; ok {
			p = updated
		}
		out[i] = p
	}
	return out, nil
}

// checkSaved checks a product a member just saved, within SaveCheckBudget,
// and returns it with the result. Failing to check is not an error: the
// product is saved either way, and the member choosing it counts as fresh.
func (s *Service) checkSaved(ctx context.Context, p Preference) Preference {
	if s.checker == nil {
		return p
	}
	budget, cancel := context.WithTimeout(ctx, SaveCheckBudget)
	defer cancel()
	run, err := s.checkProducts(budget, p.Provider, []Preference{p}, 1)
	if err != nil {
		s.logger.WarnContext(ctx, "saved product check failed", "householdId", p.HouseholdID, "error", err)
	}
	if updated, ok := run.Updated[p.ID]; ok {
		return updated
	}
	return p
}
