package providers

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// The owner's case: a recipe asks for cloves, the household buys a bulb sold
// as "Each" (1 ct). Per-amount math can't measure cloves against a count, so
// it flags the line; the weekly rule buys one bulb and says so.
func TestCountPackagesForWeeklyCoverageBuysOneBulb(t *testing.T) {
	needs := []ingredients.Amount{amount(t, "4", "clove")}
	size := amount(t, "1", "count")

	exact := CountPackagesFor(needs, &size, CoveragePerAmount, Item{})
	if exact.Packages != 1 || exact.Reason != ReasonUnitNotConvertible {
		t.Fatalf("per_amount = %d packages, reason %q; want 1 and %q", exact.Packages, exact.Reason, ReasonUnitNotConvertible)
	}
	if exact.CoversWeek {
		t.Error("per_amount should not claim weekly coverage")
	}

	weekly := CountPackagesFor(needs, &size, CoveragePerWeek, Item{})
	switch {
	case weekly.Packages != 1:
		t.Errorf("per_week = %d packages, want 1", weekly.Packages)
	case weekly.Reason != "":
		t.Errorf("per_week reason = %q, want no flag: one bulb covering the week is not something to check", weekly.Reason)
	case !weekly.CoversWeek:
		t.Error("per_week should record that the count rests on weekly coverage")
	}
	if got, want := CoverageText(weekly, &size), "1 × 1 ct covers this week (4 cloves)"; got != want {
		t.Errorf("CoverageText = %q, want %q", got, want)
	}
	if got := ReasonText(weekly, &size); got != "" {
		t.Errorf("ReasonText = %q, want none", got)
	}
}

// The rule must not collapse a need that can actually be measured. A week of
// beef is still three packages under either rule.
func TestCountPackagesForWeeklyCoverageKeepsMeasuredMath(t *testing.T) {
	needs := []ingredients.Amount{amount(t, "9/4", "lb")}
	size := amount(t, "16", "oz")
	for _, coverage := range []Coverage{CoveragePerAmount, CoveragePerWeek} {
		got := CountPackagesFor(needs, &size, coverage, Item{})
		if got.Packages != 3 {
			t.Errorf("%s = %d packages, want 3", coverage, got.Packages)
		}
		if got.CoversWeek {
			t.Errorf("%s claimed weekly coverage for a measured need", coverage)
		}
	}
}

// A partly measurable line keeps its exact count and its flag: the part that
// converted is real, so assuming one package would under-buy it.
func TestCountPackagesForWeeklyCoverageKeepsPartialFlag(t *testing.T) {
	needs := []ingredients.Amount{amount(t, "1", "count"), amount(t, "40", "oz")}
	size := amount(t, "32", "oz")
	got := CountPackagesFor(needs, &size, CoveragePerWeek, Item{})
	switch {
	case got.Packages != 2:
		t.Errorf("Packages = %d, want 2", got.Packages)
	case got.Reason != ReasonUnitNotConvertible:
		t.Errorf("Reason = %q, want %q kept", got.Reason, ReasonUnitNotConvertible)
	case got.CoversWeek:
		t.Error("a partly measured line should not claim weekly coverage")
	}
}

// A per_week line with no package size isn't something to check: without a
// size nothing can be measured, so one package covers the week either way
// (the owner's Cilantro, Garlic and Ginger). Only per_amount needs the size.
func TestCountPackagesForMissingSizeFollowsCoverage(t *testing.T) {
	needs := []ingredients.Amount{amount(t, "4", "clove")}

	weekly := CountPackagesFor(needs, nil, CoveragePerWeek, Item{})
	if weekly.Packages != 1 || weekly.Reason != "" || !weekly.CoversWeek {
		t.Errorf("per_week, no size = %+v, want 1 package covering the week, unflagged", weekly)
	}
	if got, want := CoverageText(weekly, nil), "1 package covers this week (4 cloves)"; got != want {
		t.Errorf("CoverageText = %q, want %q", got, want)
	}
	if got := ReasonText(weekly, nil); got != "" {
		t.Errorf("ReasonText = %q, want none", got)
	}
	if got := CoverageText(CountPackagesFor(nil, nil, CoveragePerWeek, Item{}), nil); got != "1 package covers this week" {
		t.Errorf("unquantified CoverageText = %q", got)
	}

	exact := CountPackagesFor(needs, nil, CoveragePerAmount, Item{})
	if exact.Packages != 1 || exact.Reason != ReasonNoPackageSize || exact.CoversWeek {
		t.Errorf("per_amount, no size = %+v, want 1 package flagged %q", exact, ReasonNoPackageSize)
	}
}

// CoverageAuto is the zero value and must behave as exact math, so a caller
// that forgets to choose gets today's behaviour rather than an assumption.
func TestCountPackagesForAutoIsExact(t *testing.T) {
	needs := []ingredients.Amount{amount(t, "4", "clove")}
	size := amount(t, "1", "count")
	got := CountPackagesFor(needs, &size, CoverageAuto, Item{})
	if got.CoversWeek || got.Reason != ReasonUnitNotConvertible {
		t.Errorf("CoverageAuto = %+v, want exact math", got)
	}
	if plain := CountPackages(needs, &size); plain.Packages != got.Packages || plain.Reason != got.Reason {
		t.Errorf("CountPackages and CoverageAuto disagree: %+v vs %+v", plain, got)
	}
}
