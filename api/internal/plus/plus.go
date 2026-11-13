// Package plus is DinnerOS Plus: whether a household has it, and the
// RevenueCat webhook that keeps that true (decision 673).
//
// A household subscribes in the app through the App Store; RevenueCat
// validates the purchase and calls the webhook here with each change. The
// app user ID RevenueCat knows the purchase by is the household's ID, so Plus
// belongs to the household, not to the member who paid. Nothing here talks to
// RevenueCat or Apple: the webhook is the only way in, and it is checked with
// a shared secret.
package plus

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
)

// EntitlementID is the RevenueCat entitlement that is DinnerOS Plus.
const EntitlementID = "plus"

// ErrNotFound means the household has never had Plus.
var ErrNotFound = errors.New("plus: not found")

// Entitlement is a household's Plus subscription as RevenueCat last told us.
type Entitlement struct {
	HouseholdID string
	ProductID   string
	// Trial is true while the household is in its free trial.
	Trial bool
	// ExpiresAt is when Plus ends unless it renews; zero for a purchase
	// that doesn't expire.
	ExpiresAt time.Time
	// WillRenew is false once the member turned off renewal.
	WillRenew bool
	// BillingIssue is true while Apple can't charge the renewal; Plus stays
	// on through Apple's grace period.
	BillingIssue bool
	// Ended is set by an EXPIRATION event, whatever ExpiresAt says.
	Ended bool
	// Environment is SANDBOX for TestFlight and test purchases, PRODUCTION
	// for the App Store.
	Environment string
	// LastEventID and LastEventAt order events: an older one never undoes a
	// newer one, and a repeated one changes nothing.
	LastEventID string
	LastEventAt time.Time
	UpdatedAt   time.Time
}

// Active reports whether the household has Plus at now.
func (e Entitlement) Active(now time.Time) bool {
	if e.Ended {
		return false
	}
	return e.ExpiresAt.IsZero() || now.Before(e.ExpiresAt)
}

// Status is what the app is told about a household's Plus.
type Status struct {
	// Enabled is false while Plus is switched off (PLUS_ENABLED): the app
	// then shows no paywall and every feature is open.
	Enabled      bool
	Active       bool
	Trial        bool
	ExpiresAt    time.Time
	WillRenew    bool
	BillingIssue bool
	// FreeUntil is when free months from referrals end; zero if none. Plus
	// is active while either a subscription or free time is.
	FreeUntil time.Time
}

// Store keeps entitlements, one per household.
type Store interface {
	Get(ctx context.Context, householdID string) (Entitlement, error)
	// Put stores e unless the stored one has a later LastEventAt; applied
	// reports whether it was stored.
	Put(ctx context.Context, e Entitlement) (applied bool, err error)
	Delete(ctx context.Context, householdID string) error
}

// Service answers Plus questions and applies RevenueCat events.
type Service struct {
	store     Store
	enabled   bool
	now       func() time.Time
	free      FreeTimeStore
	referrals ReferralStore
}

// Enabled reports whether Plus is switched on (PLUS_ENABLED).
func (s *Service) Enabled() bool { return s.enabled }

// NewService returns the service. enabled is PLUS_ENABLED.
func NewService(store Store, enabled bool) *Service {
	return &Service{store: store, enabled: enabled, now: time.Now}
}

// Status returns a household's Plus.
func (s *Service) Status(ctx context.Context, householdID string) (Status, error) {
	out := Status{Enabled: s.enabled}
	now := s.now()
	if s.free != nil {
		until, err := s.free.FreeUntil(ctx, householdID)
		if err != nil {
			return Status{}, err
		}
		if now.Before(until) {
			out.Active, out.FreeUntil = true, until
		}
	}
	e, err := s.store.Get(ctx, householdID)
	if errors.Is(err, ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return Status{}, err
	}
	if e.Active(now) {
		out.Active = true
		out.Trial, out.ExpiresAt, out.WillRenew, out.BillingIssue = e.Trial, e.ExpiresAt, e.WillRenew, e.BillingIssue
	}
	return out, nil
}

// HasPlus reports whether a feature behind Plus is open to the household:
// always while Plus is switched off.
func (s *Service) HasPlus(ctx context.Context, householdID string) (bool, error) {
	st, err := s.Status(ctx, householdID)
	if err != nil {
		return false, err
	}
	return !st.Enabled || st.Active, nil
}

// Event is the part of a RevenueCat webhook event this package reads
// (https://www.revenuecat.com/docs/integrations/webhooks/event-types-and-fields).
type Event struct {
	ID                string   `json:"id"`
	Type              string   `json:"type"`
	AppUserID         string   `json:"app_user_id"`
	OriginalAppUserID string   `json:"original_app_user_id"`
	Aliases           []string `json:"aliases"`
	ProductID         string   `json:"product_id"`
	PeriodType        string   `json:"period_type"`
	EntitlementIDs    []string `json:"entitlement_ids"`
	ExpirationAtMs    *int64   `json:"expiration_at_ms"`
	EventTimestampMs  int64    `json:"event_timestamp_ms"`
	Environment       string   `json:"environment"`
}

// Outcome says what an event did, for the log.
type Outcome string

// Outcomes.
const (
	OutcomeApplied     Outcome = "applied"
	OutcomeStale       Outcome = "stale"
	OutcomeIgnored     Outcome = "ignored"
	OutcomeNoHousehold Outcome = "no_household"
)

// householdIDs are the IDs an event may name the household by: RevenueCat
// moves a purchase between aliases, so any of them can be the one we know.
func (ev Event) householdIDs() []string {
	var out []string
	for _, id := range append([]string{ev.AppUserID, ev.OriginalAppUserID}, ev.Aliases...) {
		id = strings.TrimSpace(id)
		if isHouseholdID(id) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// isHouseholdID reports whether id looks like one of ours: a 24-character
// hex ObjectID. RevenueCat's anonymous IDs ($RCAnonymousID:…) never are.
func isHouseholdID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// Apply applies one RevenueCat event. Events about other entitlements, test
// events, and events naming no household are ignored, not errors, so
// RevenueCat doesn't retry them.
func (s *Service) Apply(ctx context.Context, ev Event) (Outcome, error) {
	if ev.Type == "TEST" {
		return OutcomeIgnored, nil
	}
	if len(ev.EntitlementIDs) > 0 && !slices.Contains(ev.EntitlementIDs, EntitlementID) {
		return OutcomeIgnored, nil
	}
	ids := ev.householdIDs()
	if len(ids) == 0 {
		return OutcomeNoHousehold, nil
	}
	householdID := ids[0]
	at := time.UnixMilli(ev.EventTimestampMs).UTC()

	current, err := s.store.Get(ctx, householdID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", err
	}
	if err == nil && (current.LastEventID == ev.ID || at.Before(current.LastEventAt)) {
		return OutcomeStale, nil
	}
	next := current
	next.HouseholdID = householdID
	if ev.ProductID != "" {
		next.ProductID = ev.ProductID
	}
	if ev.ExpirationAtMs != nil {
		next.ExpiresAt = time.UnixMilli(*ev.ExpirationAtMs).UTC()
	}
	if ev.Environment != "" {
		next.Environment = ev.Environment
	}

	switch ev.Type {
	case "INITIAL_PURCHASE", "RENEWAL", "UNCANCELLATION", "PRODUCT_CHANGE":
		next.Ended, next.WillRenew, next.BillingIssue = false, true, false
		next.Trial = ev.PeriodType == "TRIAL"
	case "NON_RENEWING_PURCHASE":
		next.Ended, next.WillRenew, next.BillingIssue, next.Trial = false, false, false, false
	case "CANCELLATION":
		// Renewal is off; Plus lasts until it expires.
		next.WillRenew = false
	case "BILLING_ISSUE":
		next.BillingIssue = true
	case "EXPIRATION":
		next.Ended, next.WillRenew, next.Trial = true, false, false
	case "SUBSCRIPTION_EXTENDED", "REFUND_REVERSED":
		next.Ended = false
	default:
		return OutcomeIgnored, nil
	}
	next.LastEventID, next.LastEventAt, next.UpdatedAt = ev.ID, at, s.now().UTC()
	applied, err := s.store.Put(ctx, next)
	if err != nil {
		return "", err
	}
	if !applied {
		return OutcomeStale, nil
	}
	return OutcomeApplied, nil
}
