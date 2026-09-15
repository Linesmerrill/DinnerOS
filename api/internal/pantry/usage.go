package pantry

import (
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file holds the pure usage-estimate logic: cycles, segments, the learned
// rate, and the estimate itself. It has no I/O and takes the time as an
// argument. docs/pantry-usage.md explains the model.

// StatusSource says who set an item's status.
type StatusSource string

// Status sources.
const (
	StatusSourcePerson   StatusSource = "person"
	StatusSourceEstimate StatusSource = "estimate"
)

// CycleSource says what started a usage cycle.
type CycleSource string

// Cycle sources. The first three are purchase sources (PurchaseSource).
const (
	CycleGroceryList CycleSource = "grocery_list"
	CycleManual      CycleSource = "manual"
	CycleProvider    CycleSource = "provider"
	// CycleEdit: a person set an amount without recording a purchase.
	CycleEdit CycleSource = "edit"
	// CycleHouseMade: the household made a batch of a specialty ingredient
	// (PurchaseHouseMade).
	CycleHouseMade CycleSource = "house_made"
)

// Usage limits and defaults.
const (
	// DefaultLowThresholdPercent is the household threshold until it's set.
	DefaultLowThresholdPercent = 80
	// MaxHistorySegments is how many closed segments an item keeps.
	MaxHistorySegments = 6
	// MinRateSegments is how many usable segments a learned rate needs.
	MinRateSegments = 2
	// MinSegmentDuration is the shortest segment the rate learns from.
	MinSegmentDuration = 24 * time.Hour
)

// UnitSize says how much one discrete unit holds: one Unit ("package") is
// Quantity SizeUnit ("8 oz").
type UnitSize struct {
	Unit     string
	Quantity string
	SizeUnit string
}

// Tracking is an item's current usage cycle. A cycle starts when the
// household buys the item (or first sets an amount) and lasts until the next
// purchase. A person correcting the amount mid-cycle starts a new segment of
// the same cycle. Every amount is exact and in Unit.
type Tracking struct {
	CycleID        string
	CycleSource    CycleSource
	CycleStartedAt time.Time
	// Unit is the tracking unit.
	Unit string
	// Reference is 100%: the amount bought, or first set by hand.
	Reference string
	// SegmentStart is the amount at SegmentStartedAt, the latest purchase or
	// correction by a person.
	SegmentStart     string
	SegmentStartedAt time.Time
	// SegmentRecipeUsed is what cooked recipes used since SegmentStartedAt.
	SegmentRecipeUsed string
	// RecipeUsed and RecipeUses cover the whole cycle, for the explanation.
	RecipeUsed string
	RecipeUses int
	// SkippedUses counts cooked recipes that used the item but couldn't be
	// deducted (no amount, or units that don't convert).
	SkippedUses int
}

// Segment is a closed stretch of usage, kept to learn the rate. Amounts are
// exact and in Unit.
type Segment struct {
	StartedAt  time.Time
	EndedAt    time.Time
	Unit       string
	Start      string
	RecipeUsed string
	Remaining  string
	// Observed is true when a person recorded what remained (corrected the
	// amount or marked the item out). False means a restock assumed it was
	// used up.
	Observed bool
}

// Rate is the learned non-recipe use: the median daily use across recent
// segments, beyond what recipes accounted for.
type Rate struct {
	// PerDay is exact, in Unit, rounded to thousandths.
	PerDay     string
	Unit       string
	Segments   int
	ComputedAt time.Time
}

// Estimate is an item's estimated remaining amount at a moment. Amounts are
// in Unit.
type Estimate struct {
	CycleID        string
	CycleSource    CycleSource
	CycleStartedAt time.Time
	// AdjustedAt is when a person last corrected the amount in this cycle, or
	// zero.
	AdjustedAt time.Time
	Unit       string
	// Reference is 100%. Remaining is rounded to hundredths.
	Reference        *big.Rat
	Remaining        *big.Rat
	PercentRemaining int
	PercentUsed      int
	RecipeUsed       *big.Rat
	RecipeUses       int
	SkippedUses      int
	// OtherUsed is the learned rate applied since the segment started,
	// rounded to hundredths.
	OtherUsed *big.Rat
	// DailyRate is the learned rate in Unit (rounded to thousandths), or nil.
	DailyRate     *big.Rat
	RateSegments  int
	ThresholdPct  int
	ThresholdItem bool
	// BelowThreshold is true when at least ThresholdPct of Reference is used.
	BelowThreshold bool
	Summary        string
	// EstimatedAt is the moment the estimate describes.
	EstimatedAt time.Time
}

// --- Exact amounts ------------------------------------------------------------

// ratOf parses an exact amount; empty or invalid is 0.
func ratOf(s string) *big.Rat {
	if s == "" {
		return new(big.Rat)
	}
	q, err := ingredients.ParseQuantity(s)
	if err != nil {
		return new(big.Rat)
	}
	return q.Rat()
}

// roundRat rounds a non-negative r to the nearest 1/den (halves up). Negative
// values are 0.
func roundRat(r *big.Rat, den int64) *big.Rat {
	if r.Sign() <= 0 {
		return new(big.Rat)
	}
	x := new(big.Rat).Mul(r, big.NewRat(den, 1))
	x.Add(x, big.NewRat(1, 2))
	n := new(big.Int).Quo(x.Num(), x.Denom())
	return new(big.Rat).SetFrac(n, big.NewInt(den))
}

func clampZero(r *big.Rat) *big.Rat {
	if r.Sign() < 0 {
		return new(big.Rat)
	}
	return r
}

var secondsPerDay = big.NewRat(86400, 1)

func days(d time.Duration) *big.Rat {
	if d <= 0 {
		return new(big.Rat)
	}
	return new(big.Rat).Quo(big.NewRat(int64(d/time.Second), 1), secondsPerDay)
}

// convertAmount converts q from one unit code to another. A discrete unit
// converts only through size (one size.Unit is size.Quantity size.SizeUnit).
// ok is false when no exact conversion exists: volume never becomes mass,
// and a count never becomes either without a size.
func convertAmount(q *big.Rat, from, to string, size *UnitSize) (*big.Rat, bool) {
	if from == to {
		return new(big.Rat).Set(q), true
	}
	if size != nil && size.Unit != size.SizeUnit {
		switch {
		case from == size.Unit:
			return convertAmount(new(big.Rat).Mul(q, ratOf(size.Quantity)), size.SizeUnit, to, nil)
		case to == size.Unit:
			inSize, ok := convertAmount(q, from, size.SizeUnit, nil)
			per := ratOf(size.Quantity)
			if !ok || per.Sign() == 0 {
				return nil, false
			}
			return inSize.Quo(inSize, per), true
		}
	}
	fu, err := ingredients.LookupUnit(from)
	if err != nil {
		return nil, false
	}
	tu, err := ingredients.LookupUnit(to)
	if err != nil || !fu.CanConvertTo(tu) {
		return nil, false
	}
	r := new(big.Rat).Mul(q, fu.BaseFactor())
	return r.Quo(r, tu.BaseFactor()), true
}

// trackingAmount is the amount and unit a recorded amount is tracked in: a
// discrete unit with a known size is tracked in the size's unit.
func trackingAmount(quantity, unit string, size *UnitSize) (*big.Rat, string) {
	q := ratOf(quantity)
	if size != nil && size.Unit == unit {
		return q.Mul(q, ratOf(size.Quantity)), size.SizeUnit
	}
	return q, unit
}

// --- Cycle transitions --------------------------------------------------------

func cloneTracking(t *Tracking) *Tracking {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// cloneItem returns item with its reference fields copied, so changing the
// copy never changes the original.
func cloneItem(item Item) Item {
	item.Tracking = cloneTracking(item.Tracking)
	if item.UnitSize != nil {
		size := *item.UnitSize
		item.UnitSize = &size
	}
	if item.Rate != nil {
		rate := *item.Rate
		item.Rate = &rate
	}
	item.History = slices.Clone(item.History)
	return item
}

// startCycle begins a usage cycle with amount q in unit at at.
func startCycle(item *Item, id string, source CycleSource, q *big.Rat, unit string, at time.Time) {
	amount := q.RatString()
	item.Tracking = &Tracking{
		CycleID: id, CycleSource: source, CycleStartedAt: at, Unit: unit,
		Reference: amount, SegmentStart: amount, SegmentStartedAt: at,
		SegmentRecipeUsed: "0", RecipeUsed: "0",
	}
}

// closeSegment ends the current segment at end with remaining (in the
// tracking unit), adds it to the history, and relearns the rate. The item's
// Tracking is left as it was.
func closeSegment(item *Item, end time.Time, remaining *big.Rat, observed bool, now time.Time) {
	t := item.Tracking
	if end.Before(t.SegmentStartedAt) {
		end = t.SegmentStartedAt
	}
	seg := Segment{
		StartedAt: t.SegmentStartedAt, EndedAt: end, Unit: t.Unit,
		Start: t.SegmentStart, RecipeUsed: ratOf(t.SegmentRecipeUsed).RatString(),
		Remaining: clampZero(remaining).RatString(), Observed: observed,
	}
	history := append(slices.Clone(item.History), seg)
	if len(history) > MaxHistorySegments {
		history = history[len(history)-MaxHistorySegments:]
	}
	item.History = history
	item.Rate = learnRate(history, t.Unit, item.UnitSize, now)
}

// restock applies a purchase to item at now: the current segment closes (as
// used up, unless a person marked the item out, which records when), and a
// new cycle starts with the purchased amount. A purchase without an amount
// ends tracking. The item is in stock afterwards.
func restock(item *Item, cycleID string, source CycleSource, quantity, unit string, now time.Time) {
	if t := item.Tracking; t != nil {
		end, observed := now, false
		if item.Status == StatusOut && item.StatusSource != StatusSourceEstimate && item.StatusSetAt.After(t.SegmentStartedAt) {
			end, observed = item.StatusSetAt, true
		}
		closeSegment(item, end, new(big.Rat), observed, now)
	}
	item.Tracking = nil
	item.Quantity, item.Unit = quantity, unit
	if quantity != "" {
		q, trackUnit := trackingAmount(quantity, unit, item.UnitSize)
		startCycle(item, cycleID, source, q, trackUnit, now)
	}
	item.Status, item.StatusSource, item.StatusSetAt = StatusInStock, StatusSourcePerson, now
}

// applyPersonEdit records what a person's change from before to after means
// for status provenance and tracking. A person's amount always wins over the
// estimate: it closes the segment as observed and becomes the new starting
// point. An amount above the cycle's reference, after the item was out, or in
// a unit that doesn't convert starts a new cycle. Clearing the amount ends
// tracking; marking the item out (which clears the amount) keeps the cycle.
func applyPersonEdit(before Item, after *Item, now time.Time, newCycleID func() string) {
	if after.Status != before.Status {
		after.StatusSource, after.StatusSetAt = StatusSourcePerson, now
	}
	if after.Quantity == before.Quantity && after.Unit == before.Unit {
		return
	}
	if after.Quantity == "" {
		if after.Status != StatusOut {
			after.Tracking = nil
		}
		return
	}
	q, unit := trackingAmount(after.Quantity, after.Unit, after.UnitSize)
	t := before.Tracking
	if t == nil {
		startCycle(after, newCycleID(), CycleEdit, q, unit, now)
		return
	}
	after.Tracking = cloneTracking(t)
	if before.Status == StatusOut {
		end := now
		if before.StatusSetAt.After(t.SegmentStartedAt) {
			end = before.StatusSetAt
		}
		closeSegment(after, end, new(big.Rat), true, now)
		startCycle(after, newCycleID(), CycleEdit, q, unit, now)
		return
	}
	converted, ok := convertAmount(q, unit, t.Unit, after.UnitSize)
	if !ok {
		startCycle(after, newCycleID(), CycleEdit, q, unit, now)
		return
	}
	closeSegment(after, now, converted, true, now)
	if converted.Cmp(ratOf(t.Reference)) > 0 {
		startCycle(after, newCycleID(), CycleEdit, converted, t.Unit, now)
		return
	}
	after.Tracking.SegmentStart = converted.RatString()
	after.Tracking.SegmentStartedAt = now
	after.Tracking.SegmentRecipeUsed = "0"
}

// --- Learned rate -------------------------------------------------------------

// learnRate computes the non-recipe daily use from closed segments: for each
// of the most recent MaxHistorySegments segments lasting at least
// MinSegmentDuration, (start − recipe use − remaining) ÷ days, never below 0.
// The rate is the median of those, and needs MinRateSegments of them.
// Segments in a unit that doesn't convert to unit are ignored.
func learnRate(history []Segment, unit string, size *UnitSize, now time.Time) *Rate {
	var rates []*big.Rat
	for i := len(history) - 1; i >= 0; i-- {
		seg := history[i]
		duration := seg.EndedAt.Sub(seg.StartedAt)
		if duration < MinSegmentDuration {
			continue
		}
		start, ok1 := convertAmount(ratOf(seg.Start), seg.Unit, unit, size)
		used, ok2 := convertAmount(ratOf(seg.RecipeUsed), seg.Unit, unit, size)
		left, ok3 := convertAmount(ratOf(seg.Remaining), seg.Unit, unit, size)
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		other := clampZero(start.Sub(start, used).Sub(start, left))
		rates = append(rates, other.Quo(other, days(duration)))
	}
	if len(rates) < MinRateSegments {
		return nil
	}
	slices.SortFunc(rates, func(a, b *big.Rat) int { return a.Cmp(b) })
	mid := len(rates) / 2
	median := new(big.Rat).Set(rates[mid])
	if len(rates)%2 == 0 {
		median.Add(rates[mid-1], rates[mid]).Quo(median, big.NewRat(2, 1))
	}
	return &Rate{PerDay: roundRat(median, 1000).RatString(), Unit: unit, Segments: len(rates), ComputedAt: now}
}

// --- Estimate -----------------------------------------------------------------

// effectiveThreshold returns the item's threshold and whether it's the item's
// own override.
func effectiveThreshold(item Item, householdPercent int) (int, bool) {
	if item.LowThresholdPercent >= 1 && item.LowThresholdPercent <= 100 {
		return item.LowThresholdPercent, true
	}
	if householdPercent < 1 || householdPercent > 100 {
		householdPercent = DefaultLowThresholdPercent
	}
	return householdPercent, false
}

// estimateItem estimates item's remaining amount at now, or returns nil when
// the item isn't tracked. Remaining = segment start − recipe use since −
// learned rate × days since, never below 0, and 0 when the item is out.
func estimateItem(item Item, householdPercent int, now time.Time) *Estimate {
	t := item.Tracking
	if t == nil {
		return nil
	}
	ref := ratOf(t.Reference)
	if ref.Sign() <= 0 {
		return nil
	}
	e := &Estimate{
		CycleID: t.CycleID, CycleSource: t.CycleSource, CycleStartedAt: t.CycleStartedAt, Unit: t.Unit,
		Reference: ref, RecipeUsed: ratOf(t.RecipeUsed), RecipeUses: t.RecipeUses, SkippedUses: t.SkippedUses,
		OtherUsed: new(big.Rat), EstimatedAt: now,
	}
	if t.SegmentStartedAt.After(t.CycleStartedAt) {
		e.AdjustedAt = t.SegmentStartedAt
	}
	remaining := ratOf(t.SegmentStart)
	remaining.Sub(remaining, ratOf(t.SegmentRecipeUsed))
	if item.Rate != nil {
		if perDay, ok := convertAmount(ratOf(item.Rate.PerDay), item.Rate.Unit, t.Unit, item.UnitSize); ok {
			e.DailyRate, e.RateSegments = roundRat(perDay, 1000), item.Rate.Segments
			other := new(big.Rat).Mul(perDay, days(now.Sub(t.SegmentStartedAt)))
			remaining.Sub(remaining, other)
			e.OtherUsed = roundRat(other, 100)
		}
	}
	remaining = clampZero(remaining)
	if remaining.Cmp(ref) > 0 {
		remaining.Set(ref)
	}
	if item.Status == StatusOut {
		remaining = new(big.Rat)
	}
	e.Remaining = roundRat(remaining, 100)
	pct := new(big.Rat).Quo(new(big.Rat).Mul(remaining, big.NewRat(100, 1)), ref)
	e.PercentRemaining = int(roundRat(pct, 1).Num().Int64())
	e.PercentUsed = 100 - e.PercentRemaining
	e.ThresholdPct, e.ThresholdItem = effectiveThreshold(item, householdPercent)
	used := new(big.Rat).Sub(ref, remaining)
	e.BelowThreshold = used.Mul(used, big.NewRat(100, 1)).Cmp(new(big.Rat).Mul(ref, big.NewRat(int64(e.ThresholdPct), 1))) >= 0
	e.Summary = e.summary()
	return e
}

// needsAutoLow reports whether the estimate should mark item low now: it's
// tracked and in stock, the estimate crossed the threshold, this cycle hasn't
// alerted yet, and no person has set the status since the amount was last
// recorded (a person's status wins).
func needsAutoLow(item Item, e *Estimate) bool {
	if e == nil || item.Tracking == nil || !e.BelowThreshold || item.Status != StatusInStock {
		return false
	}
	if item.LowAlertCycleID == item.Tracking.CycleID {
		return false
	}
	personSet := item.StatusSource != StatusSourceEstimate && item.StatusSetAt.After(item.Tracking.SegmentStartedAt)
	return !personSet
}

func (e *Estimate) summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "About %d%% left", e.PercentRemaining)
	var parts []string
	if e.RecipeUses > 0 {
		parts = append(parts, fmt.Sprintf("%s used %s", countText(e.RecipeUses, "recipe", "recipes"), amountText(e.RecipeUsed, e.Unit)))
	}
	if e.DailyRate != nil && e.DailyRate.Sign() > 0 {
		parts = append(parts, fmt.Sprintf("about %s a day of other use", amountText(e.DailyRate, e.Unit)))
	}
	if len(parts) > 0 {
		b.WriteString(": ")
		b.WriteString(strings.Join(parts, ", plus "))
	}
	b.WriteString(".")
	if e.SkippedUses > 0 {
		fmt.Fprintf(&b, " %s couldn't be counted.", countText(e.SkippedUses, "recipe", "recipes"))
	}
	return b.String()
}

func countText(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// amountText renders an amount for people: kitchen fractions when exact to
// eighths, otherwise hundredths.
func amountText(r *big.Rat, unitCode string) string {
	shown := r
	if eighths := roundRat(r, 8); eighths.Cmp(r) != 0 {
		shown = roundRat(r, 100)
	}
	q := ingredients.NewQuantity(1, 1).MulRat(shown)
	text := q.Format()
	if u, err := ingredients.LookupUnit(unitCode); err == nil {
		if label := u.Label(q); label != "" {
			text += " " + label
		}
	}
	return text
}
