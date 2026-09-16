package menu

import (
	"context"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Week strip limits.
const (
	DefaultWeeksBefore = 8
	DefaultWeeksAfter  = 4
	MaxWeeksAround     = 52
)

// WeekStatusNone is the status of a week without a stored plan.
const WeekStatusNone = "none"

// The earliest and latest weeks plans can have (planning keeps week years
// four-digit).
var (
	firstWeek = planning.Week{Year: 2000, Number: 1}
	lastWeek  = planning.WeekOf(time.Date(9999, time.December, 28, 0, 0, 0, 0, time.UTC))
)

// WeekSummary is one week in the strip.
type WeekSummary struct {
	Week   planning.Week
	Timing Timing
	// Planned counts planned main meals, AddOns the week's planned add-ons
	// (a pairing's garlic bread), Cooked distinct cooked entries (or recipes
	// and dates, for events without an entry), and Ordered main meals
	// delivered that week. Add-ons are never counted as meals.
	Planned int
	AddOns  int
	Cooked  int
	Ordered int
	// Status is draft, finalized, or none.
	Status string
}

// WeekStrip is the week strip.
type WeekStrip struct {
	// Weeks are oldest first.
	Weeks []WeekSummary
	// Earliest is the earliest week with a planned entry or an ordered main
	// meal, or nil.
	Earliest *planning.Week
}

// Weeks returns the strip from before weeks before around ("" for the current
// week) to after weeks after it. before and after are clamped to 0–52.
func (s *Service) Weeks(ctx context.Context, householdID, around string, before, after int) (WeekStrip, error) {
	before, after = min(max(before, 0), MaxWeeksAround), min(max(after, 0), MaxWeeksAround)
	current, loc, err := s.clock(ctx, householdID)
	if err != nil {
		return WeekStrip{}, err
	}
	center, err := parseWeek(around, current)
	if err != nil {
		return WeekStrip{}, err
	}
	from, to := center.AddWeeks(-before), center.AddWeeks(after)
	if from.WeeksUntil(firstWeek) > 0 {
		from = firstWeek
	}
	if lastWeek.WeeksUntil(to) > 0 {
		to = lastWeek
	}

	plans := map[planning.Week]planning.Summary{}
	for start := from; start.WeeksUntil(to) >= 0; start = start.AddWeeks(planning.MaxRangeWeeks) {
		end := start.AddWeeks(planning.MaxRangeWeeks - 1)
		if to.WeeksUntil(end) > 0 {
			end = to
		}
		list, err := s.opts.Plans.List(ctx, householdID, start.String(), end.String())
		if err != nil {
			return WeekStrip{}, fmt.Errorf("load plans: %w", err)
		}
		for _, sum := range list {
			plans[sum.Week] = sum
		}
	}
	catalog, err := s.opts.Recipes.MenuCatalog(ctx, householdID)
	if err != nil {
		return WeekStrip{}, fmt.Errorf("load catalog: %w", err)
	}
	// Cooking is recorded during or after the week; a week's margin covers
	// time zones and early check-offs.
	cooked, err := s.cookedEvents(ctx, householdID, from.Monday().AddDate(0, 0, -7))
	if err != nil {
		return WeekStrip{}, err
	}
	earliest, ok, err := s.opts.Plans.EarliestPlannedWeek(ctx, householdID)
	if err != nil {
		return WeekStrip{}, fmt.Errorf("load earliest plan: %w", err)
	}
	var earliestPlanned *planning.Week
	if ok {
		earliestPlanned = &earliest
	}
	return buildWeekStrip(from, to, current, loc, plans, catalog, cooked, earliestPlanned), nil
}

func buildWeekStrip(from, to, current planning.Week, loc *time.Location, plans map[planning.Week]planning.Summary,
	catalog []recipes.Recipe, cooked []events.Event, earliestPlanned *planning.Week) WeekStrip {
	if loc == nil {
		loc = time.UTC
	}
	ordered := map[string]int{}
	addons := map[string]bool{}
	strip := WeekStrip{Earliest: earliestPlanned}
	for _, r := range catalog {
		if r.IsAddon {
			addons[r.ID] = true
		}
		if r.IsAddon || len(r.OrderWeeks) == 0 {
			continue
		}
		for _, w := range r.OrderWeeks {
			ordered[w]++
		}
		// Order weeks are sorted, so the first is the recipe's earliest.
		if w, err := planning.ParseWeek(r.OrderWeeks[0]); err == nil && (strip.Earliest == nil || w.WeeksUntil(*strip.Earliest) > 0) {
			strip.Earliest = &w
		}
	}
	cookedKeys := map[string]map[string]bool{}
	for _, e := range cooked {
		week, key := eventWeek(e, loc), e.RecipeID
		if p, ok := e.Payload.(events.RecipeCooked); ok {
			if p.EntryID != "" {
				key = "entry:" + p.EntryID
			} else {
				key += "|" + p.Date
			}
		}
		if cookedKeys[week] == nil {
			cookedKeys[week] = map[string]bool{}
		}
		cookedKeys[week][key] = true
	}
	for w := from; w.WeeksUntil(to) >= 0; w = w.AddWeeks(1) {
		ws := WeekSummary{Week: w, Timing: timingOf(w, current), Ordered: ordered[w.String()], Cooked: len(cookedKeys[w.String()]), Status: WeekStatusNone}
		if sum, ok := plans[w]; ok && !sum.UpdatedAt.IsZero() {
			ws.Planned, ws.AddOns = splitAddOns(sum, addons)
			ws.Status = string(sum.Status)
		}
		strip.Weeks = append(strip.Weeks, ws)
	}
	return strip
}

// splitAddOns counts a week's planned main meals and add-ons. An entry whose
// recipe isn't in the catalog counts as a main meal, and a summary without
// recipe IDs falls back to its entry count, so a store that doesn't project
// them still reports the week as planned.
func splitAddOns(sum planning.Summary, addons map[string]bool) (meals, addOns int) {
	if len(sum.RecipeIDs) == 0 {
		return sum.EntryCount, 0
	}
	for _, id := range sum.RecipeIDs {
		if addons[id] {
			addOns++
			continue
		}
		meals++
	}
	return meals, addOns
}
