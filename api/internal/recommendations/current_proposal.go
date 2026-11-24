package recommendations

import (
	"context"
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// notRecentlyWeeks mirrors the baseline model: "Haven’t had it in N weeks" is
// only said of a meal last had this many weeks or more from the week planned.
const notRecentlyWeeks = 10

// notRecentlyCode is the reason the baseline model gives for a meal the
// household hasn't had in a while.
const notRecentlyCode = "notRecently"

// current is a saved proposal as it stands now. A pending proposal keeps the
// picks and reasons it was made with, but the week moves on after it: a
// recipe is deleted, a suggested meal gets planned and made by hand, a day
// passes. Decided proposals are history and stay as they were. This is the
// proposal as shown; Accept reads it without dropping planned meals, so it
// can say they were already planned.
func (s *Service) current(ctx context.Context, w planning.Week, p Proposal) Proposal {
	p = s.withoutRemovedRecipes(ctx, p)
	p = s.withoutPlannedRecipes(ctx, w, p)
	p = s.withFreshRecency(ctx, w, p)
	return s.withoutPastDays(ctx, w, p)
}

// withoutPlannedRecipes leaves out a pending proposal's meals that are
// already on the week's plan. A proposal made before a meal was planned by
// hand kept offering it for another night of the same week: Chicken Penne al
// Limone, planned Sunday and marked made, suggested again for Tuesday (build
// 123 review).
func (s *Service) withoutPlannedRecipes(ctx context.Context, w planning.Week, p Proposal) Proposal {
	if p.Status != StatusProposed || s.plans == nil || len(p.Slots) == 0 {
		return p
	}
	plan, err := s.plans.Get(ctx, p.HouseholdID, w.String())
	if err != nil || len(plan.Entries) == 0 {
		return p
	}
	planned := make(map[string]bool, len(plan.Entries))
	for _, e := range plan.Entries {
		planned[e.RecipeID] = true
	}
	kept := slices.DeleteFunc(slices.Clone(p.Slots), func(sl Slot) bool { return planned[sl.RecipeID] })
	if len(kept) < len(p.Slots) {
		p.Slots, p.Planned = kept, len(kept)
	}
	return p
}

// withFreshRecency drops "Haven’t had it in N weeks" from a pending
// proposal's meal that has since been planned or made within that many weeks
// of the proposal's week. The reason was true when the proposal was made, but
// a meal made last night hasn't gone 16 weeks.
func (s *Service) withFreshRecency(ctx context.Context, w planning.Week, p Proposal) Proposal {
	if p.Status != StatusProposed {
		return p
	}
	stale := map[string]bool{}
	for _, sl := range p.Slots {
		if slices.ContainsFunc(sl.Reasons, isNotRecently) {
			stale[sl.RecipeID] = false
		}
	}
	if len(stale) == 0 {
		return p
	}
	recent := s.recentlyHad(ctx, p.HouseholdID, w, stale)
	if len(recent) == 0 {
		return p
	}
	p.Slots = slices.Clone(p.Slots)
	for i, sl := range p.Slots {
		if recent[sl.RecipeID] {
			p.Slots[i].Reasons = slices.DeleteFunc(slices.Clone(sl.Reasons), isNotRecently)
		}
	}
	return p
}

func isNotRecently(r Reason) bool { return r.Code == notRecentlyCode }

// recentlyHad returns which of the recipes were planned, or made, within
// notRecentlyWeeks of week w. Reads that fail count as nothing found.
func (s *Service) recentlyHad(ctx context.Context, householdID string, w planning.Week, recipeIDs map[string]bool) map[string]bool {
	recent := map[string]bool{}
	near := func(week string) bool {
		between, ok := autopilot.WeeksBetween(w.String(), week)
		return ok && absInt(between) < notRecentlyWeeks
	}
	if s.plans != nil {
		plans, err := s.plans.ListPlans(ctx, householdID, w.AddWeeks(-(notRecentlyWeeks - 1)).String(), w.String())
		if err == nil {
			for _, plan := range plans {
				if !near(plan.Week.String()) {
					continue
				}
				for _, e := range plan.Entries {
					if _, ok := recipeIDs[e.RecipeID]; ok {
						recent[e.RecipeID] = true
					}
				}
			}
		}
	}
	if s.events == nil {
		return recent
	}
	first := planning.Day("")
	if s.households != nil {
		if h, err := s.households.GetHousehold(ctx, householdID); err == nil {
			first = planning.Day(h.FirstDay())
		}
	}
	// One read for all of them: a slot or two, out of a few weeks of meals.
	list, err := s.events.List(ctx, events.Query{
		HouseholdID: householdID, Types: []events.Type{events.TypeRecipeCooked},
		Since: w.AddWeeks(-notRecentlyWeeks).Monday(), Limit: events.MaxListLimit,
	})
	if err != nil {
		return recent
	}
	for _, e := range list {
		if _, ok := recipeIDs[e.RecipeID]; !ok || recent[e.RecipeID] {
			continue
		}
		week := e.Week
		if week == "" {
			week = planning.WeekOfOn(e.OccurredAt, first).String()
		}
		if near(week) {
			recent[e.RecipeID] = true
		}
	}
	return recent
}
