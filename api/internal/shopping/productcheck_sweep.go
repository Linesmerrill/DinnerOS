package shopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file is the scheduled sweep (cmd/checkproducts, decision #552): it
// re-checks every household's saved products, oldest check first, a capped
// number per run, and tells a household when a product it saved has gone.

// Sweep defaults.
const (
	// DefaultSweepMax is the most distinct products one run checks. At the
	// sweep interval (2.5 s plus up to 40% jitter) that is about ten minutes.
	DefaultSweepMax = 200
	// MaxSweepMax bounds a run however it is configured.
	MaxSweepMax = 1000
	// SweepStaleAfter: a product checked more recently than this is skipped,
	// so a daily run re-checks each product about once a day and never twice.
	SweepStaleAfter = 20 * time.Hour
	// sweepOversample is how many saved products are read per product to
	// check, since households share products and each is checked once.
	sweepOversample = 4
)

// SweepOptions configures one sweep run.
type SweepOptions struct {
	// Max is the most distinct products to check; 0 is DefaultSweepMax.
	Max int
}

// SweepReport says what one run did.
type SweepReport struct {
	// Candidates is how many saved products were due.
	Candidates int
	Checked    int
	Found      int
	Gone       int
	Unknown    int
	// NewlyGone is how many saved products became gone this run, and
	// Notified how many households were told.
	NewlyGone int
	Notified  int
	// Stopped: the provider refused or throttled a check, so the run stopped
	// and checks are paused. Paused: they already were, and nothing ran.
	Stopped bool
	Paused  bool
}

// SweepProducts runs one sweep for a provider.
func (s *Service) SweepProducts(ctx context.Context, provider providers.Key, o SweepOptions) (SweepReport, error) {
	var report SweepReport
	maxProducts := o.Max
	if maxProducts <= 0 {
		maxProducts = DefaultSweepMax
	}
	maxProducts = min(maxProducts, MaxSweepMax)
	paused, err := s.checksPaused(ctx, provider)
	if err != nil {
		return report, err
	}
	if paused {
		report.Paused = true
		return report, nil
	}
	due, err := s.store.ListPreferencesToCheck(ctx, provider, s.now().Add(-SweepStaleAfter), maxProducts*sweepOversample)
	if err != nil {
		return report, fmt.Errorf("list saved products to check: %w", err)
	}
	report.Candidates = len(due)
	run, err := s.checkProducts(ctx, provider, due, maxProducts)
	report.Checked, report.Found, report.Gone, report.Unknown = run.Checked, run.Found, run.Gone, run.Unknown
	report.Stopped, report.NewlyGone = run.Stopped, len(run.NewlyGone)
	// Whatever stopped the run, what it found out is told.
	report.Notified = s.notifyGone(context.WithoutCancel(ctx), provider, run.NewlyGone)
	return report, err
}

// notifyGone creates one notification per household for the saved products
// that just became gone, and returns how many were created. Failures are
// logged: the product's state is what matters, and the Shop tab shows it
// either way.
func (s *Service) notifyGone(ctx context.Context, provider providers.Key, gone []Preference) int {
	if s.notifier == nil || len(gone) == 0 {
		return 0
	}
	byHousehold := map[string][]Preference{}
	var order []string
	for _, p := range gone {
		if _, seen := byHousehold[p.HouseholdID]; !seen {
			order = append(order, p.HouseholdID)
		}
		byHousehold[p.HouseholdID] = append(byHousehold[p.HouseholdID], p)
	}
	name := string(provider)
	if p, ok := s.providers.Get(provider); ok {
		name = p.Name()
	}
	created := 0
	for _, householdID := range order {
		prefs := byHousehold[householdID]
		_, isNew, err := s.notifier.Create(ctx, notifications.New{
			HouseholdID: householdID,
			Type:        notifications.TypeShoppingProductGone,
			Title:       productsGoneTitle(len(prefs), name),
			Body:        productsGoneBody(prefs, name),
			Subject:     notifications.Subject{Kind: notifications.SubjectShoppingProducts, ID: string(provider)},
			DedupeKey:   productsGoneDedupeKey(prefs),
		})
		if err != nil {
			s.logger.WarnContext(ctx, "product gone notification failed", "householdId", householdID, "error", err)
			continue
		}
		if isNew {
			created++
		}
	}
	return created
}

func productsGoneTitle(n int, provider string) string {
	if n == 1 {
		return "A saved product is no longer on " + provider
	}
	return fmt.Sprintf("%d saved products are no longer on %s", n, provider)
}

func productsGoneBody(prefs []Preference, provider string) string {
	names := make([]string, 0, len(prefs))
	for _, p := range prefs {
		names = append(names, p.IngredientName)
	}
	shown := names
	if len(names) > 3 {
		shown = append(slices.Clone(names[:3]), fmt.Sprintf("%d more", len(names)-3))
	}
	it := "it"
	if len(prefs) > 1 {
		it = "them"
	}
	return fmt.Sprintf("%s. %s no longer lists the product you saved, so it can't go in your cart. Re-choose %s in Shop → Saved Products.",
		strings.Join(shown, ", "), provider, it)
}

// productsGoneDedupeKey names exactly these products, so a run that finds
// the same ones gone again (a retried sweep) doesn't notify twice, and a
// product re-chosen and gone again later does notify.
func productsGoneDedupeKey(prefs []Preference) string {
	parts := make([]string, 0, len(prefs))
	for _, p := range prefs {
		parts = append(parts, p.ID+":"+p.ProductID)
	}
	slices.Sort(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return "shopping.product_gone:" + hex.EncodeToString(sum[:8])
}

// goneStillRelevant reports whether a product-gone notification should still
// be pushed: some saved product of the provider is still gone. A member who
// re-chose it before the push went out isn't told again.
func (s *Service) goneStillRelevant(ctx context.Context, n notifications.Notification) (bool, error) {
	prefs, err := s.store.ListPreferences(ctx, n.HouseholdID, providers.Key(n.Subject.ID))
	if err != nil {
		return true, err
	}
	now := s.now()
	for _, p := range prefs {
		if p.Health(now) == HealthGone {
			return true, nil
		}
	}
	return false, nil
}
