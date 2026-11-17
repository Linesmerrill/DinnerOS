package recommendations

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/companions"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Companions are a household's things that go with dinner (package
// companions). They join the week's pairings here, after pairing rules and
// before learned pairings, so the week's suggestions, Add, Not This Week and
// the grocery list work for them unchanged:
//
//   - A companion is due for a planned meal when the meal's cuisine (or its
//     region) is one of the companion's cuisines, the meal is one of its
//     recipes, or the meal is in one of its meal categories (carried over
//     from a pairing rule by migration).
//   - It is also due when its frequency says so (DueByFrequency). Then it is
//     shown once, with the week's first main meal, and marked Standalone.
//   - Just Suggest It offers it; Add It for Me adds it without asking
//     (applyAutoCompanions); Never leaves it out, and for a companion made
//     from an add-on recipe also hides that add-on's rule and learned
//     pairings.
//   - A store item is a grocery item keyed "companion:<id>"; a companion
//     made from an add-on recipe offers that recipe, keyed like any add-on
//     ("recipe:<id>"), so a rule for the same add-on shows once, as before.

// PairingSourceCompanion marks a pairing a household companion offers.
const PairingSourceCompanion = "companion"

// CompanionSource reads companions and records when they were listed.
// *companions.Service implements it.
type CompanionSource interface {
	// ForEvaluation returns the visible companions, making companions for
	// add-on recipes in catalog that have none.
	ForEvaluation(ctx context.Context, householdID string, catalog []recipes.Recipe) ([]companions.Companion, error)
	// Active returns the visible companions without migrating.
	Active(ctx context.Context, householdID string) ([]companions.Companion, error)
	MarkListed(ctx context.Context, householdID, id, week string, at time.Time) error
}

// WithCompanions turns companions on, and returns s.
func (s *Service) WithCompanions(c CompanionSource) *Service {
	s.companions = c
	return s
}

func companionKey(id string) string { return "companion:" + id }

// CompanionHints implements companions.HintSource: for each add-on recipe a
// pairing rule adds, the cuisines of rules that match on cuisines alone and
// the meal categories of rules that match on meal categories alone. A rule
// that mixes conditions carries nothing over, so a migrated companion never
// matches a meal its rules didn't (the rules keep working as they are).
func (s *Service) CompanionHints(ctx context.Context, householdID string) ([]companions.Hint, error) {
	p, err := s.Profile(ctx, householdID)
	if err != nil {
		return nil, err
	}
	return companionHints(p.Pairings), nil
}

func companionHints(rules []PairingRule) []companions.Hint {
	var out []companions.Hint
	index := map[string]int{}
	for _, r := range rules {
		if r.Add.RecipeID == "" {
			continue
		}
		i, ok := index[r.Add.RecipeID]
		if !ok {
			i = len(out)
			index[r.Add.RecipeID] = i
			out = append(out, companions.Hint{RecipeID: r.Add.RecipeID})
		}
		w, h := r.When, &out[i]
		switch {
		case len(w.Cuisines) > 0 && len(w.MealCategories)+len(w.Tags)+len(w.Proteins) == 0:
			for _, c := range w.Cuisines {
				if !slices.Contains(h.Cuisines, c) {
					h.Cuisines = append(h.Cuisines, c)
				}
			}
		case w.onlyCategories():
			for _, c := range w.MealCategories {
				if !slices.Contains(h.MealCategories, c) {
					h.MealCategories = append(h.MealCategories, c)
				}
			}
		}
	}
	return out
}

// loadCompanions sets d's companions. A failure is logged: pairings still
// work without them.
func (s *Service) loadCompanions(ctx context.Context, d *pairingData, householdID string, catalog []recipes.Recipe) {
	if s.companions == nil {
		return
	}
	list, err := s.companions.ForEvaluation(ctx, householdID, catalog)
	if err != nil {
		s.logger.WarnContext(ctx, "load companions", "householdId", householdID, "error", err)
		return
	}
	d.companions = list
}

// hostEntry is the meal frequency-due companions are shown with: the week's
// first planned main meal (unscheduled ones last), or "".
func (d *pairingData) hostEntry(plan planning.Plan) string {
	first := plan.First()
	best, bestOrder := "", 0
	for _, e := range plan.Entries {
		r, ok := d.byID[e.RecipeID]
		if !ok || r.IsAddon {
			continue
		}
		order := 7
		if e.Day != "" {
			order = e.Day.OrderOn(first)
		}
		if best == "" || order < bestOrder {
			best, bestOrder = e.ID, order
		}
	}
	return best
}

// companionMatch says why a companion goes with the meal, or "" when it
// doesn't.
func companionMatch(c companions.Companion, main recipes.Recipe, facts mealFacts) string {
	if c.HasRecipe(main.ID) {
		return "Goes with " + main.Name
	}
	for _, v := range c.Cuisines {
		if slices.Contains(facts.cuisines, v) {
			name := cuisineLabel(v)
			if name == "" {
				name = v
			}
			return "Goes with " + name + " meals"
		}
	}
	for _, v := range c.MealCategories {
		if slices.Contains(facts.categories, v) {
			return "Goes with " + mealCategoryFor(v).short + " meals"
		}
	}
	return ""
}

// companionPairings returns what the household's companions offer with a
// main meal, in companion order.
func (d *pairingData) companionPairings(sub pairingSubject, main recipes.Recipe, facts mealFacts, plan planning.Plan) []Pairing {
	var out []Pairing
	host, hostKnown := "", false
	for _, c := range d.companions {
		if c.Mode == companions.ModeNever {
			continue
		}
		reason, standalone := companionMatch(c, main, facts), false
		if reason == "" {
			if sub.EntryID == "" || !c.DueByFrequency(d.week) {
				continue
			}
			if !hostKnown {
				host, hostKnown = d.hostEntry(plan), true
			}
			if sub.EntryID != host {
				continue
			}
			reason, standalone = "You get it "+companions.FrequencyPhrase(c.Frequency), true
		}
		p, ok := d.companionTarget(c, sub)
		if !ok {
			continue
		}
		p.Source, p.CompanionID, p.Standalone, p.Reason = PairingSourceCompanion, c.ID, standalone, reason
		p.Frequency = PairingSuggest
		if c.Mode == companions.ModeAuto {
			p.Frequency = PairingAlways
		}
		out = append(out, p)
	}
	return out
}

// companionTarget is what accepting a companion adds: its add-on recipe, or
// its store item.
func (d *pairingData) companionTarget(c companions.Companion, sub pairingSubject) (Pairing, bool) {
	if c.LinkedRecipe != nil {
		return d.target(PairingTarget{RecipeID: c.LinkedRecipe.ID}, sub)
	}
	g := GroceryItem{Name: c.GroceryName(), Quantity: c.Quantity, Unit: c.Unit}
	return Pairing{Key: companionKey(c.ID), Kind: PairingKindGroceryItem, GroceryItem: &g}, true
}

// turnedOff is the keys of add-ons whose companion is set to Never.
func (d *pairingData) turnedOff() map[string]bool {
	var out map[string]bool
	for _, c := range d.companions {
		if c.Mode == companions.ModeNever && c.LinkedRecipe != nil {
			if out == nil {
				out = map[string]bool{}
			}
			out["recipe:"+c.LinkedRecipe.ID] = true
		}
	}
	return out
}

// markListed records the week on the companions behind pairings that went
// on the week's list or into its plan. Failures are logged.
func (s *Service) markListed(ctx context.Context, householdID string, w planning.Week, pairings []Pairing, now time.Time) {
	if s.companions == nil {
		return
	}
	done := map[string]bool{}
	for _, p := range pairings {
		if p.CompanionID == "" || done[p.CompanionID] {
			continue
		}
		done[p.CompanionID] = true
		if err := s.companions.MarkListed(ctx, householdID, p.CompanionID, w.String(), now); err != nil && !errors.Is(err, companions.ErrNotFound) {
			s.logger.WarnContext(ctx, "record companion week", "householdId", householdID, "week", w.String(), "error", err)
		}
	}
}

// hasAutoCompanions reports whether any visible companion adds itself.
func hasAutoCompanions(list []companions.Companion) bool {
	return slices.ContainsFunc(list, func(c companions.Companion) bool { return c.Mode == companions.ModeAuto })
}

// applyAutoCompanions adds the week's due Add It for Me companions that the
// week doesn't have yet and nobody declined: store items go on the grocery
// list, and with recipesToo an add-on recipe is planned on its meal's day.
// It never touches a finalized week or a week already over. userID may be
// empty. It returns the plan, changed when a recipe was planned, and whether
// it changed anything. Failures are logged; the list works without them.
func (s *Service) applyAutoCompanions(ctx context.Context, d *pairingData, w planning.Week, plan planning.Plan, userID string, recipesToo bool) (planning.Plan, bool) {
	if !hasAutoCompanions(d.companions) || plan.Status == planning.StatusFinalized ||
		w.WeeksUntil(planning.WeekOf(s.now().UTC())) > 0 {
		return plan, false
	}
	now := s.now().UTC()
	changed := false
	for _, e := range plan.Entries {
		r, ok := d.byID[e.RecipeID]
		if !ok || r.IsAddon {
			continue
		}
		for _, p := range d.suggest(entrySubject(e), plan) {
			if p.Source != PairingSourceCompanion || p.Frequency != PairingAlways || p.InPlan || p.Dismissed ||
				d.state.decision(e.ID, p.Key) >= 0 || (p.Recipe != nil && !recipesToo) {
				continue
			}
			next, ok := s.addAutoCompanion(ctx, d, w, plan, e, p, userID, now)
			if ok {
				plan, changed = next, true
			}
		}
	}
	return plan, changed
}

// addAutoCompanion adds one companion pairing for entry, claiming the
// decision first so two readers at once add it once.
func (s *Service) addAutoCompanion(ctx context.Context, d *pairingData, w planning.Week, plan planning.Plan, entry planning.Entry, p Pairing, userID string, now time.Time) (planning.Plan, bool) {
	for range maxAttempts {
		next := d.state
		next.Decisions, next.GroceryItems = slices.Clone(next.Decisions), slices.Clone(next.GroceryItems)
		if next.decision(entry.ID, p.Key) >= 0 {
			return plan, false
		}
		decision := PairingDecision{EntryID: entry.ID, Key: p.Key, Status: DecisionAccepted, DecidedBy: userID, DecidedAt: now}
		if p.GroceryItem != nil {
			if next.groceryItemByKey(p.Key) >= 0 || len(next.GroceryItems) >= MaxWeekGroceryItems {
				return plan, false
			}
			g := WeekGroceryItem{
				ID: newID(), Key: p.Key, GroceryItem: *p.GroceryItem, ForEntryID: entry.ID, ForRecipeID: entry.RecipeID,
				ForRecipeName: d.byID[entry.RecipeID].Name, Source: p.Source, CompanionID: p.CompanionID, Standalone: p.Standalone,
				AddedBy: userID, AddedAt: now,
			}
			next.GroceryItems = append(next.GroceryItems, g)
			decision.GroceryItemID = g.ID
		}
		next.setDecision(decision)
		next.UpdatedAt = now
		saved, err := s.pairings.SaveWeekPairings(ctx, next)
		if errors.Is(err, ErrConflict) {
			state, err := s.pairings.GetWeekPairings(ctx, d.state.HouseholdID, w.String())
			if err != nil && !isNotFound(err) {
				return plan, false
			}
			if err == nil {
				d.state = state
			}
			continue
		}
		if err != nil {
			s.logger.WarnContext(ctx, "add a companion", "householdId", d.state.HouseholdID, "week", w.String(), "error", err)
			return plan, false
		}
		d.state = saved
		if p.Recipe != nil {
			updated, added, err := s.plans.AddEntries(ctx, d.state.HouseholdID, userID, w.String(), []planning.NewEntry{{
				RecipeID: p.Recipe.ID, Day: string(entry.Day), Servings: p.Servings, Origin: planning.OriginAutopilot,
			}})
			if err != nil {
				s.logger.WarnContext(ctx, "plan a companion", "householdId", d.state.HouseholdID, "week", w.String(), "error", err)
				s.updateDecision(ctx, d.state.HouseholdID, w, entry.ID, p.Key, nil)
				return plan, false
			}
			s.updateDecision(ctx, d.state.HouseholdID, w, entry.ID, p.Key, func(dec *PairingDecision) { dec.AddedEntryID = added[0].ID })
			plan = updated
		}
		s.markListed(ctx, d.state.HouseholdID, w, []Pairing{p}, now)
		return plan, true
	}
	return plan, false
}
