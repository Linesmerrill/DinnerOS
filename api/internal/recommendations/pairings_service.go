package recommendations

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Pairing results and skip reasons.
const (
	PairingAdded        = "added"
	PairingAlreadyAdded = "alreadyAdded"
	// PairingSkipAlreadyPlanned: the add-on is already planned that day.
	PairingSkipAlreadyPlanned = "alreadyPlanned"
	// PairingSkipUnavailable: the add-on recipe was removed or can no longer
	// be planned.
	PairingSkipUnavailable = "unavailable"
	// MaxRecipePairings bounds the recipe-scoped read.
	MaxRecipePairings = 6
	// pendingAcceptWindow is how long an accept that hasn't recorded its plan
	// entry yet counts as in progress.
	pendingAcceptWindow = time.Minute
)

// pairingData is what suggesting pairings for one household and week needs.
type pairingData struct {
	profile           Profile
	householdServings int
	byID              map[string]recipes.Recipe
	overrides         map[string]RecipeOverride
	learned           map[string][]PairingStats
	state             WeekPairings
	facts             map[string]mealFacts
}

// loadPairings loads the profile, catalog, overrides, 26 weeks of plans, and
// the week's pairing state, and learns pairings from every week but w.
func (s *Service) loadPairings(ctx context.Context, householdID string, w planning.Week) (*pairingData, error) {
	if s.pairings == nil {
		return nil, ErrPairingsUnavailable
	}
	profile, err := s.Profile(ctx, householdID)
	if err != nil {
		return nil, err
	}
	servings, err := s.DefaultServings(ctx, profile)
	if err != nil {
		return nil, err
	}
	catalog, err := s.recipes.Catalog(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("load catalog: %w", err)
	}
	overrideList, err := s.store.ListOverrides(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("load recipe overrides: %w", err)
	}
	plans, err := s.plans.ListPlans(ctx, householdID, w.AddWeeks(-(planning.MaxRangeWeeks - 1)).String(), w.String())
	if err != nil {
		return nil, fmt.Errorf("load plans: %w", err)
	}
	state, err := s.pairings.GetWeekPairings(ctx, householdID, w.String())
	switch {
	case isNotFound(err):
		state = WeekPairings{HouseholdID: householdID, Week: w.String()}
	case err != nil:
		return nil, fmt.Errorf("load week pairings: %w", err)
	}
	d := &pairingData{
		profile: profile, householdServings: servings, state: state,
		byID: make(map[string]recipes.Recipe, len(catalog)), overrides: make(map[string]RecipeOverride, len(overrideList)),
		facts: map[string]mealFacts{},
	}
	for _, r := range catalog {
		d.byID[r.ID] = r
	}
	for _, o := range overrideList {
		d.overrides[o.RecipeID] = o
	}
	d.learned = buildPairingHistory(catalog, d.overrides, plans, w.String()).learned()
	return d, nil
}

func (d *pairingData) override(recipeID string) *RecipeOverride {
	if o, ok := d.overrides[recipeID]; ok {
		return &o
	}
	return nil
}

// mealFacts returns what rules match a main meal on.
func (d *pairingData) mealFacts(r recipes.Recipe) mealFacts {
	if f, ok := d.facts[r.ID]; ok {
		return f
	}
	o := d.override(r.ID)
	a := attributes(r, o, d.profile.bands())
	f := mealFacts{
		categories: recipeMealCategories(r, o), cuisines: append(slices.Clone(a.Cuisines), a.CuisineRegions...),
		tags: a.Tags, proteins: a.Proteins,
	}
	d.facts[r.ID] = f
	return f
}

// pairingSubject is the main meal pairings are for.
type pairingSubject struct {
	// EntryID is the meal's plan entry; SlotID its proposal slot.
	EntryID  string
	SlotID   string
	RecipeID string
	// Day is "" for an unscheduled meal.
	Day      string
	Servings int
	// anyDay: the meal isn't planned, so an add-on anywhere in the week
	// counts as planned.
	anyDay bool
}

// suggest returns the pairings for a main meal, best first: always rules,
// suggest rules (in rule order), then learned pairings by confidence. A
// target appears once, and add-on recipes that can't be planned are left
// out. InPlan and Dismissed are set against plan and the week's state.
func (d *pairingData) suggest(sub pairingSubject, plan planning.Plan) []Pairing {
	main, ok := d.byID[sub.RecipeID]
	if !ok || main.IsAddon {
		return nil
	}
	facts := d.mealFacts(main)
	var out []Pairing
	seen := map[string]int{}
	add := func(p Pairing) {
		if _, ok := seen[p.Key]; !ok {
			seen[p.Key] = len(out)
			out = append(out, p)
		}
	}
	for _, frequency := range []string{PairingAlways, PairingSuggest} {
		for _, rule := range d.profile.Pairings {
			if rule.Frequency != frequency {
				continue
			}
			matched, category := rule.When.matches(facts)
			if !matched {
				continue
			}
			p, ok := d.target(rule.Add, sub)
			if !ok {
				continue
			}
			p.Source, p.Frequency, p.MealCategory, p.RuleID = PairingSourceRule, rule.Frequency, category, rule.ID
			p.Reason = "Your rule: " + rule.When.summary() + " → " + targetName(p)
			add(p)
		}
	}
	var stats []PairingStats
	for _, c := range facts.categories {
		stats = append(stats, d.learned[c]...)
	}
	slices.SortStableFunc(stats, func(a, b PairingStats) int {
		if c := cmp.Compare(b.Confidence(), a.Confidence()); c != 0 {
			return c
		}
		return cmp.Compare(b.Together, a.Together)
	})
	for _, st := range stats {
		key := "recipe:" + st.AddonID
		if i, ok := seen[key]; ok {
			// History backs a rule: show its confidence too.
			if out[i].Learned == nil && (out[i].MealCategory == "" || out[i].MealCategory == st.Category) {
				withStats(&out[i], st)
			}
			continue
		}
		p, ok := d.target(PairingTarget{RecipeID: st.AddonID}, sub)
		if !ok {
			continue
		}
		p.Source, p.Frequency, p.MealCategory = PairingSourceLearned, PairingSuggest, st.Category
		withStats(&p, st)
		category := mealCategoryFor(st.Category)
		p.Reason = fmt.Sprintf("You usually have %s with %s (%d%% of %s weeks)", targetName(p), category.short,
			int(math.Round(st.Confidence()*100)), category.week)
		add(p)
	}
	d.mark(out, sub, plan)
	return out
}

func withStats(p *Pairing, st PairingStats) {
	p.Confidence = st.Confidence()
	p.Learned = &LearnedPairing{WeeksTogether: st.Together, CategoryWeeks: st.CategoryWeeks, OtherWeeks: st.OtherWeeks(), OtherRate: st.OtherRate()}
}

func targetName(p Pairing) string {
	if p.Recipe != nil {
		return p.Recipe.Name
	}
	return p.GroceryItem.Name
}

// target builds the pairing for a rule's or learned target, or false when an
// add-on recipe is missing, isn't an add-on, or has no serving sizes.
func (d *pairingData) target(t PairingTarget, sub pairingSubject) (Pairing, bool) {
	p := Pairing{Key: t.Key(), Kind: t.Kind()}
	if t.GroceryItem != nil {
		g := *t.GroceryItem
		p.GroceryItem = &g
		return p, true
	}
	r, ok := d.byID[t.RecipeID]
	if !ok || !r.IsAddon || len(r.Servings) == 0 {
		return Pairing{}, false
	}
	p.Recipe = &PairingRecipe{
		ID: r.ID, Name: r.Name, Headline: r.Headline, ImageURL: r.ImageURL, CookMinutes: r.CookMinutes(),
		TimesOrdered: r.TimesOrdered, LastOrderedWeek: r.LastOrderedWeek, Tags: r.Tags,
	}
	p.Servings = addonServings(r.Servings, sub.Servings)
	return p, true
}

// addonServings is the smallest authored size that feeds want, or the
// largest size.
func addonServings(sizes []int, want int) int {
	sorted := slices.Sorted(slices.Values(sizes))
	for _, s := range sorted {
		if s >= want {
			return s
		}
	}
	return sorted[len(sorted)-1]
}

// mark sets InPlan and Dismissed. An add-on is in the plan when it is planned
// on the meal's day (anywhere in the week for a meal that isn't planned). A
// grocery item is on the list when the week has it for a meal that's still
// planned, or when a planned recipe uses an ingredient by that name.
func (d *pairingData) mark(pairings []Pairing, sub pairingSubject, plan planning.Plan) {
	var names map[string]bool
	for i := range pairings {
		p := &pairings[i]
		switch p.Kind {
		case PairingKindRecipe:
			p.InPlan = slices.ContainsFunc(plan.Entries, func(e planning.Entry) bool {
				return e.RecipeID == p.Recipe.ID && (sub.anyDay || string(e.Day) == sub.Day)
			})
		case PairingKindGroceryItem:
			if names == nil {
				names = d.plannedIngredientNames(plan)
			}
			j := d.state.groceryItemByKey(p.Key)
			p.InPlan = (j >= 0 && planEntry(plan, d.state.GroceryItems[j].ForEntryID) != nil) || names[ingredients.NormalizeName(p.GroceryItem.Name)]
		}
		if sub.EntryID == "" {
			continue
		}
		if j := d.state.decision(sub.EntryID, p.Key); j >= 0 && d.state.Decisions[j].Status == DecisionDismissed {
			p.Dismissed = true
		}
	}
}

func (d *pairingData) plannedIngredientNames(plan planning.Plan) map[string]bool {
	names := map[string]bool{}
	for _, e := range plan.Entries {
		for _, line := range d.byID[e.RecipeID].Ingredients {
			names[ingredients.NormalizeName(line.Name)] = true
		}
	}
	return names
}

// visiblePairings drops pairings already in the plan or dismissed, and keeps
// at most MaxPairingsPerMeal.
func visiblePairings(all []Pairing) []Pairing {
	var out []Pairing
	for _, p := range all {
		if !p.InPlan && !p.Dismissed && len(out) < MaxPairingsPerMeal {
			out = append(out, p)
		}
	}
	return out
}

func planEntry(plan planning.Plan, id string) *planning.Entry {
	for i := range plan.Entries {
		if plan.Entries[i].ID == id {
			return &plan.Entries[i]
		}
	}
	return nil
}

func entrySubject(e planning.Entry) pairingSubject {
	return pairingSubject{EntryID: e.ID, RecipeID: e.RecipeID, Day: string(e.Day), Servings: e.Servings}
}

func indexByKey(pairings []Pairing, key string) int {
	return slices.IndexFunc(pairings, func(p Pairing) bool { return p.Key == key })
}

// --- week pairings ----------------------------------------------------------------

// MealPairings are the pairings offered with one planned main meal.
type MealPairings struct {
	Entry          planning.Entry
	MealCategories []string
	Pairings       []Pairing
}

// WeekPairingsView is a week's pairing suggestions and paired grocery items.
type WeekPairingsView struct {
	Week  planning.Week
	Meals []MealPairings
	// GroceryItems are on the week's grocery list: their meal is still planned.
	GroceryItems []WeekGroceryItem
}

// WeekPairings returns the pairings for the week's planned main meals (only
// entryID's when set), leaving out pairings already in the week or dismissed.
// Pairings shown for an entry for the first time are recorded as
// pairing.suggested by userID.
func (s *Service) WeekPairings(ctx context.Context, householdID, userID, week, entryID string) (WeekPairingsView, error) {
	w, err := planning.ParseWeek(week)
	if err != nil {
		return WeekPairingsView{}, err
	}
	plan, err := s.plans.Get(ctx, householdID, w.String())
	if err != nil {
		return WeekPairingsView{}, fmt.Errorf("load plan: %w", err)
	}
	if entryID != "" && planEntry(plan, entryID) == nil {
		return WeekPairingsView{}, fmt.Errorf("%w: plan entry not found", ErrNotFound)
	}
	d, err := s.loadPairings(ctx, householdID, w)
	if err != nil {
		return WeekPairingsView{}, err
	}
	view := d.weekView(w, plan, entryID)
	s.recordEntrySuggestions(ctx, householdID, userID, w, view.Meals)
	return view, nil
}

func (d *pairingData) weekView(w planning.Week, plan planning.Plan, entryID string) WeekPairingsView {
	view := WeekPairingsView{Week: w}
	for _, e := range plan.Entries {
		r, ok := d.byID[e.RecipeID]
		if !ok || r.IsAddon || (entryID != "" && e.ID != entryID) {
			continue
		}
		view.Meals = append(view.Meals, MealPairings{
			Entry: e, MealCategories: d.mealFacts(r).categories, Pairings: visiblePairings(d.suggest(entrySubject(e), plan)),
		})
	}
	for _, g := range d.state.GroceryItems {
		if planEntry(plan, g.ForEntryID) != nil {
			view.GroceryItems = append(view.GroceryItems, g)
		}
	}
	return view
}

// recordEntrySuggestions records pairing.suggested once per plan entry and
// target. Earlier suggestions are read back from the week's events; a failure
// to read them skips recording.
func (s *Service) recordEntrySuggestions(ctx context.Context, householdID, userID string, w planning.Week, meals []MealPairings) {
	if userID == "" || !slices.ContainsFunc(meals, func(m MealPairings) bool { return len(m.Pairings) > 0 }) {
		return
	}
	earlier, err := s.events.List(ctx, events.Query{
		HouseholdID: householdID, Types: []events.Type{events.TypePairingSuggested},
		Since: w.Monday().AddDate(0, 0, -7*planning.MaxRangeWeeks), Limit: events.MaxListLimit,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "load earlier pairing suggestions", "householdId", householdID, "week", w.String(), "error", err)
		return
	}
	seen := map[string]bool{}
	for _, e := range earlier {
		if p, ok := e.Payload.(events.PairingSuggested); ok && e.Week == w.String() && p.EntryID != "" {
			seen[p.EntryID+"/"+p.Key] = true
		}
	}
	now := s.now().UTC()
	for _, m := range meals {
		for _, p := range m.Pairings {
			if seen[m.Entry.ID+"/"+p.Key] {
				continue
			}
			payload := suggestedPayload(p)
			payload.EntryID, payload.Day = m.Entry.ID, string(m.Entry.Day)
			s.record(ctx, events.Event{
				HouseholdID: householdID, UserID: userID, Type: events.TypePairingSuggested, RecipeID: m.Entry.RecipeID,
				Week: w.String(), OccurredAt: now, Payload: payload,
			})
		}
	}
}

func suggestedPayload(p Pairing) events.PairingSuggested {
	return events.PairingSuggested{
		Key: p.Key, Kind: p.Kind, Source: p.Source, Frequency: p.Frequency, MealCategory: p.MealCategory, RuleID: p.RuleID,
		Confidence: roundConfidence(p.Confidence),
	}
}

func roundConfidence(c float64) float64 { return math.Round(c*1000) / 1000 }

// --- accept, dismiss, remove ------------------------------------------------------

// PairingAcceptResult is the outcome of accepting a pairing.
type PairingAcceptResult struct {
	// Status is PairingAdded, or PairingAlreadyAdded when the week already had it.
	Status  string
	Pairing Pairing
	// Entry is the add-on's plan entry; GroceryItem the grocery item.
	Entry       *planning.Entry
	GroceryItem *WeekGroceryItem
	Plan        planning.Plan
}

// AcceptPairing adds a pairing for a planned main meal to the week: an add-on
// recipe becomes a plan entry on the meal's day with origin autopilot, and a
// grocery item goes on the week's grocery list. Accepting again, or accepting
// something the week already has, returns PairingAlreadyAdded.
func (s *Service) AcceptPairing(ctx context.Context, householdID, userID, week, entryID, key string) (PairingAcceptResult, error) {
	if err := required(householdID, userID); err != nil {
		return PairingAcceptResult{}, err
	}
	if entryID == "" || key == "" {
		return PairingAcceptResult{}, invalidf("entryId and key are required")
	}
	w, plan, err := s.loadWeek(ctx, householdID, week)
	if err != nil {
		return PairingAcceptResult{}, err
	}
	entry := planEntry(plan, entryID)
	if entry == nil {
		return PairingAcceptResult{}, fmt.Errorf("%w: plan entry not found", ErrNotFound)
	}
	for range maxAttempts {
		d, err := s.loadPairings(ctx, householdID, w)
		if err != nil {
			return PairingAcceptResult{}, err
		}
		all := d.suggest(entrySubject(*entry), plan)
		i := indexByKey(all, key)
		if i < 0 {
			return PairingAcceptResult{}, fmt.Errorf("%w: no such pairing for this meal", ErrNotFound)
		}
		p := all[i]
		now := s.now().UTC()
		if res, ok := d.alreadyAccepted(p, *entry, plan, now); ok {
			return res, nil
		}
		// Claim the decision first, so two members accepting at once add it
		// once; a grocery item is added in the same write.
		next := d.state
		next.Decisions, next.GroceryItems = slices.Clone(next.Decisions), slices.Clone(next.GroceryItems)
		decision := PairingDecision{EntryID: entryID, Key: key, Status: DecisionAccepted, DecidedBy: userID, DecidedAt: now}
		var item *WeekGroceryItem
		if p.GroceryItem != nil {
			if len(next.GroceryItems) >= MaxWeekGroceryItems {
				return PairingAcceptResult{}, invalidf("the week has %d paired grocery items already", MaxWeekGroceryItems)
			}
			g := WeekGroceryItem{
				ID: newID(), Key: key, GroceryItem: *p.GroceryItem, ForEntryID: entryID, ForRecipeID: entry.RecipeID,
				ForRecipeName: d.byID[entry.RecipeID].Name, Source: p.Source, RuleID: p.RuleID, AddedBy: userID, AddedAt: now,
			}
			next.GroceryItems = append(next.GroceryItems, g)
			decision.GroceryItemID, item = g.ID, &g
		}
		next.setDecision(decision)
		next.UpdatedAt = now
		saved, err := s.pairings.SaveWeekPairings(ctx, next)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return PairingAcceptResult{}, err
		}
		res := PairingAcceptResult{Status: PairingAdded, Pairing: p, GroceryItem: item, Plan: plan}
		accepted := acceptedPayload(p)
		accepted.EntryID, accepted.Day = entryID, string(entry.Day)
		if p.Recipe != nil {
			updated, added, err := s.plans.AddEntries(ctx, householdID, userID, w.String(), []planning.NewEntry{{
				RecipeID: p.Recipe.ID, Day: string(entry.Day), Servings: p.Servings, Origin: planning.OriginAutopilot,
			}})
			if err != nil {
				s.updateDecision(ctx, householdID, w, entryID, key, nil)
				switch {
				case errors.Is(err, planning.ErrFinalized):
					return PairingAcceptResult{}, ErrPlanFinalized
				case errors.Is(err, planning.ErrRecipeNotFound), errors.Is(err, planning.ErrInvalidEntry):
					return PairingAcceptResult{}, fmt.Errorf("%w: the add-on can no longer be planned", ErrNotFound)
				}
				return PairingAcceptResult{}, err
			}
			res.Plan, res.Entry = updated, &added[0]
			accepted.AddedEntryID = added[0].ID
			s.updateDecision(ctx, householdID, w, entryID, key, func(dec *PairingDecision) { dec.AddedEntryID = added[0].ID })
		} else {
			accepted.GroceryItemID = item.ID
		}
		_ = saved
		s.record(ctx, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypePairingAccepted, RecipeID: entry.RecipeID, Week: w.String(),
			OccurredAt: now, Payload: accepted,
		})
		return res, nil
	}
	return PairingAcceptResult{}, ErrConflict
}

// alreadyAccepted reports whether the week already has the pairing for the
// meal: planned or on the list, or accepted moments ago by an accept that
// hasn't added its plan entry yet.
func (d *pairingData) alreadyAccepted(p Pairing, entry planning.Entry, plan planning.Plan, now time.Time) (PairingAcceptResult, bool) {
	res := PairingAcceptResult{Status: PairingAlreadyAdded, Pairing: p, Plan: plan}
	if p.Recipe != nil {
		for i, e := range plan.Entries {
			if e.RecipeID == p.Recipe.ID && e.Day == entry.Day {
				res.Entry = &plan.Entries[i]
				return res, true
			}
		}
		j := d.state.decision(entry.ID, p.Key)
		if j >= 0 {
			dec := d.state.Decisions[j]
			if dec.Status == DecisionAccepted && dec.AddedEntryID == "" && now.Sub(dec.DecidedAt) < pendingAcceptWindow {
				return res, true
			}
		}
		return PairingAcceptResult{}, false
	}
	if !p.InPlan {
		return PairingAcceptResult{}, false
	}
	if j := d.state.groceryItemByKey(p.Key); j >= 0 {
		item := d.state.GroceryItems[j]
		res.GroceryItem = &item
	}
	return res, true
}

func acceptedPayload(p Pairing) events.PairingAccepted {
	s := suggestedPayload(p)
	return events.PairingAccepted{Key: s.Key, Kind: s.Kind, Source: s.Source, Frequency: s.Frequency, MealCategory: s.MealCategory, RuleID: s.RuleID, Confidence: s.Confidence}
}

// setDecision replaces the entry's decision on the key, or adds one, keeping
// at most MaxPairingDecisions (the oldest go first).
func (wp *WeekPairings) setDecision(dec PairingDecision) {
	if i := wp.decision(dec.EntryID, dec.Key); i >= 0 {
		wp.Decisions = slices.Delete(wp.Decisions, i, i+1)
	}
	wp.Decisions = append(wp.Decisions, dec)
	if n := len(wp.Decisions); n > MaxPairingDecisions {
		wp.Decisions = wp.Decisions[n-MaxPairingDecisions:]
	}
}

// updateDecision changes (or, with a nil change, removes) a decision after
// the plan changed. It retries conflicts and logs failures: the plan change
// already happened.
func (s *Service) updateDecision(ctx context.Context, householdID string, w planning.Week, entryID, key string, change func(*PairingDecision)) {
	for range maxAttempts {
		state, err := s.pairings.GetWeekPairings(ctx, householdID, w.String())
		if err != nil {
			s.logger.ErrorContext(ctx, "update pairing decision", "householdId", householdID, "week", w.String(), "error", err)
			return
		}
		i := state.decision(entryID, key)
		if i < 0 {
			return
		}
		state.Decisions = slices.Clone(state.Decisions)
		if change == nil {
			state.Decisions = slices.Delete(state.Decisions, i, i+1)
		} else {
			change(&state.Decisions[i])
		}
		state.UpdatedAt = s.now().UTC()
		if _, err = s.pairings.SaveWeekPairings(ctx, state); !errors.Is(err, ErrConflict) {
			if err != nil {
				s.logger.ErrorContext(ctx, "update pairing decision", "householdId", householdID, "week", w.String(), "error", err)
			}
			return
		}
	}
}

// DismissPairing hides a pairing for a planned main meal this week. It
// returns the week's pairings for that meal.
func (s *Service) DismissPairing(ctx context.Context, householdID, userID, week, entryID, key string) (WeekPairingsView, error) {
	if err := required(householdID, userID); err != nil {
		return WeekPairingsView{}, err
	}
	if entryID == "" || key == "" {
		return WeekPairingsView{}, invalidf("entryId and key are required")
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return WeekPairingsView{}, err
	}
	plan, err := s.plans.Get(ctx, householdID, w.String())
	if err != nil {
		return WeekPairingsView{}, fmt.Errorf("load plan: %w", err)
	}
	entry := planEntry(plan, entryID)
	if entry == nil {
		return WeekPairingsView{}, fmt.Errorf("%w: plan entry not found", ErrNotFound)
	}
	for range maxAttempts {
		d, err := s.loadPairings(ctx, householdID, w)
		if err != nil {
			return WeekPairingsView{}, err
		}
		all := d.suggest(entrySubject(*entry), plan)
		i := indexByKey(all, key)
		if i < 0 {
			return WeekPairingsView{}, fmt.Errorf("%w: no such pairing for this meal", ErrNotFound)
		}
		p := all[i]
		if p.Dismissed || p.InPlan {
			return d.weekView(w, plan, entryID), nil
		}
		now := s.now().UTC()
		next := d.state
		next.Decisions = slices.Clone(next.Decisions)
		next.setDecision(PairingDecision{EntryID: entryID, Key: key, Status: DecisionDismissed, DecidedBy: userID, DecidedAt: now})
		next.UpdatedAt = now
		saved, err := s.pairings.SaveWeekPairings(ctx, next)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return WeekPairingsView{}, err
		}
		s1 := suggestedPayload(p)
		s.record(ctx, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypePairingDismissed, RecipeID: entry.RecipeID, Week: w.String(), OccurredAt: now,
			Payload: events.PairingDismissed{
				Key: s1.Key, Kind: s1.Kind, Source: s1.Source, Frequency: s1.Frequency, MealCategory: s1.MealCategory, RuleID: s1.RuleID,
				Confidence: s1.Confidence, EntryID: entryID, Day: string(entry.Day), Reason: "dismissed",
			},
		})
		d.state = saved
		return d.weekView(w, plan, entryID), nil
	}
	return WeekPairingsView{}, ErrConflict
}

// RemovePairingGroceryItem takes a paired grocery item off the week's list.
func (s *Service) RemovePairingGroceryItem(ctx context.Context, householdID, userID, week, itemID string) error {
	if err := required(householdID, userID); err != nil {
		return err
	}
	w, _, err := s.loadWeek(ctx, householdID, week)
	if err != nil {
		return err
	}
	if s.pairings == nil {
		return ErrPairingsUnavailable
	}
	for range maxAttempts {
		state, err := s.pairings.GetWeekPairings(ctx, householdID, w.String())
		if isNotFound(err) {
			return fmt.Errorf("%w: grocery item not found", ErrNotFound)
		}
		if err != nil {
			return err
		}
		i := state.groceryItem(itemID)
		if i < 0 {
			return fmt.Errorf("%w: grocery item not found", ErrNotFound)
		}
		item := state.GroceryItems[i]
		state.GroceryItems = slices.Delete(slices.Clone(state.GroceryItems), i, i+1)
		if j := state.decision(item.ForEntryID, item.Key); j >= 0 {
			state.Decisions = slices.Delete(slices.Clone(state.Decisions), j, j+1)
		}
		state.UpdatedAt = s.now().UTC()
		if _, err := s.pairings.SaveWeekPairings(ctx, state); !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return ErrConflict
}

// --- rules from suggestions -------------------------------------------------------

// MakePairingRuleInput names a learned pairing to keep as a rule: one of a
// plan entry's (EntryID) or a proposal slot's (SlotID).
type MakePairingRuleInput struct {
	EntryID   string
	SlotID    string
	Key       string
	Frequency string
}

// Rule statuses.
const (
	RuleCreated   = "created"
	RuleMerged    = "merged"
	RuleUnchanged = "unchanged"
)

// MakePairingRuleResult is the outcome of MakePairingRule.
type MakePairingRuleResult struct {
	// Status is RuleCreated, RuleMerged (the category was added to a rule for
	// the same item), or RuleUnchanged (a rule already covers it).
	Status  string
	Rule    PairingRule
	Profile Profile
}

// MakePairingRule turns a learned pairing into a household rule for its meal
// category, saved through the profile like any section change.
func (s *Service) MakePairingRule(ctx context.Context, householdID, userID, week string, in MakePairingRuleInput) (MakePairingRuleResult, error) {
	if err := required(householdID, userID); err != nil {
		return MakePairingRuleResult{}, err
	}
	frequency := normalizeValue(in.Frequency)
	switch {
	case (in.EntryID == "") == (in.SlotID == ""):
		return MakePairingRuleResult{}, invalidf("send exactly one of entryId or slotId")
	case in.Key == "":
		return MakePairingRuleResult{}, invalidf("key is required")
	case frequency == "":
		frequency = PairingSuggest
	case !slices.Contains(optionValues(PairingFrequencyOptions), frequency):
		return MakePairingRuleResult{}, invalidf("frequency must be always or suggest")
	}
	p, err := s.findPairing(ctx, householdID, week, in)
	if err != nil {
		return MakePairingRuleResult{}, err
	}
	profile, err := s.Profile(ctx, householdID)
	if err != nil {
		return MakePairingRuleResult{}, err
	}
	if p.Source == PairingSourceRule {
		for _, r := range profile.Pairings {
			if r.ID == p.RuleID {
				return MakePairingRuleResult{Status: RuleUnchanged, Rule: r, Profile: profile}, nil
			}
		}
	}
	if p.Recipe == nil || p.MealCategory == "" {
		return MakePairingRuleResult{}, invalidf("only a learned pairing with a meal category can become a rule")
	}
	rules := slices.Clone(profile.Pairings)
	status := RuleCreated
	i := slices.IndexFunc(rules, func(r PairingRule) bool {
		return r.Add.Key() == p.Key && r.Frequency == frequency && r.When.onlyCategories()
	})
	switch {
	case i >= 0 && slices.Contains(rules[i].When.MealCategories, p.MealCategory):
		return MakePairingRuleResult{Status: RuleUnchanged, Rule: rules[i], Profile: profile}, nil
	case i >= 0:
		status = RuleMerged
		rules[i].When.MealCategories = append(slices.Clone(rules[i].When.MealCategories), p.MealCategory)
	default:
		rules = append(rules, PairingRule{
			When: PairingWhen{MealCategories: []string{p.MealCategory}}, Add: PairingTarget{RecipeID: p.Recipe.ID}, Frequency: frequency,
		})
	}
	updated, err := s.UpdateProfile(ctx, householdID, userID, ProfileUpdate{Pairings: &rules}, false)
	if err != nil {
		return MakePairingRuleResult{}, err
	}
	res := MakePairingRuleResult{Status: status, Profile: updated}
	for _, r := range updated.Pairings {
		if r.Add.Key() == p.Key && r.Frequency == frequency && slices.Contains(r.When.MealCategories, p.MealCategory) {
			res.Rule = r
			break
		}
	}
	s.record(ctx, events.Event{
		HouseholdID: householdID, UserID: userID, Type: events.TypePairingRuleCreated, Week: "", OccurredAt: s.now().UTC(),
		Payload: events.PairingRuleCreated{
			RuleID: res.Rule.ID, Key: p.Key, Kind: p.Kind, MealCategory: p.MealCategory, Frequency: frequency,
			Confidence: roundConfidence(p.Confidence), Merged: status == RuleMerged,
		},
	})
	return res, nil
}

// findPairing finds a pairing offered with a plan entry or a proposal slot,
// including ones dismissed or already in the week.
func (s *Service) findPairing(ctx context.Context, householdID, week string, in MakePairingRuleInput) (Pairing, error) {
	w, err := planning.ParseWeek(week)
	if err != nil {
		return Pairing{}, err
	}
	var all []Pairing
	if in.SlotID != "" {
		proposal, err := s.store.GetProposal(ctx, householdID, w.String())
		if err != nil {
			return Pairing{}, err
		}
		i := proposal.slot(in.SlotID)
		if i < 0 {
			return Pairing{}, fmt.Errorf("%w: the proposal has no meal on %q", ErrNotFound, in.SlotID)
		}
		all = proposal.Slots[i].Pairings
	} else {
		plan, err := s.plans.Get(ctx, householdID, w.String())
		if err != nil {
			return Pairing{}, fmt.Errorf("load plan: %w", err)
		}
		entry := planEntry(plan, in.EntryID)
		if entry == nil {
			return Pairing{}, fmt.Errorf("%w: plan entry not found", ErrNotFound)
		}
		d, err := s.loadPairings(ctx, householdID, w)
		if err != nil {
			return Pairing{}, err
		}
		all = d.suggest(entrySubject(*entry), plan)
	}
	i := indexByKey(all, in.Key)
	if i < 0 {
		return Pairing{}, fmt.Errorf("%w: no such pairing for this meal", ErrNotFound)
	}
	return all[i], nil
}

// --- recipe pairings --------------------------------------------------------------

// RecipePairingsResult is what goes well with a recipe, planned or not.
type RecipePairingsResult struct {
	RecipeID string
	// Week is the week asked about, or nil.
	Week *planning.Week
	// Entry is the recipe's plan entry in Week, when it is planned.
	Entry          *planning.Entry
	MealCategories []string
	// Pairings include ones already in the week (InPlan), at most
	// MaxRecipePairings.
	Pairings []Pairing
}

// RecipePairings returns the pairings for a recipe, for its detail screen.
// With a week, InPlan is set against that week: for the recipe's plan entry
// when it is planned (its day), otherwise for the whole week. Without one,
// nothing is in the plan. Add-ons have no pairings.
func (s *Service) RecipePairings(ctx context.Context, householdID, recipeID, week string) (RecipePairingsResult, error) {
	r, err := s.recipe(ctx, householdID, recipeID)
	if err != nil {
		return RecipePairingsResult{}, err
	}
	res := RecipePairingsResult{RecipeID: r.ID}
	w := planning.WeekOf(s.now().UTC())
	plan := planning.Plan{HouseholdID: householdID, Week: w}
	if week != "" {
		if w, err = planning.ParseWeek(week); err != nil {
			return RecipePairingsResult{}, err
		}
		res.Week = &w
		if plan, err = s.plans.Get(ctx, householdID, w.String()); err != nil {
			return RecipePairingsResult{}, fmt.Errorf("load plan: %w", err)
		}
	}
	if r.IsAddon {
		return res, nil
	}
	d, err := s.loadPairings(ctx, householdID, w)
	if err != nil {
		return RecipePairingsResult{}, err
	}
	sub := pairingSubject{RecipeID: r.ID, Servings: d.householdServings, anyDay: true}
	for i, e := range plan.Entries {
		if e.RecipeID == r.ID {
			res.Entry, sub = &plan.Entries[i], entrySubject(e)
			break
		}
	}
	res.MealCategories = d.mealFacts(r).categories
	for _, p := range d.suggest(sub, plan) {
		if len(res.Pairings) < MaxRecipePairings {
			p.Dismissed = false
			res.Pairings = append(res.Pairings, p)
		}
	}
	return res, nil
}

// --- grocery list -----------------------------------------------------------------

// GroceryExtras implements planning.ExtrasSource: the week's paired grocery
// items whose meal is still planned, as grocery lines keyed like uncatalogued
// ingredients ("name:club crackers"), so the pantry and saved store products
// apply to them.
func (s *Service) GroceryExtras(ctx context.Context, plan planning.Plan) ([]grocery.Line, error) {
	if s.pairings == nil {
		return nil, nil
	}
	state, err := s.pairings.GetWeekPairings(ctx, plan.HouseholdID, plan.Week.String())
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lines []grocery.Line
	for _, g := range state.GroceryItems {
		key := ingredients.NormalizeName(g.Name)
		if key == "" || planEntry(plan, g.ForEntryID) == nil {
			continue
		}
		line := grocery.Line{
			IngredientKey: "name:" + key, Name: g.Name,
			Sources: []grocery.Source{{RecipeID: g.ForRecipeID, RecipeName: g.ForRecipeName}},
			Extra:   &grocery.Extra{ID: g.ID, Origin: grocery.OriginPairing, Text: g.Text()},
		}
		line.Category, _ = ingredients.Categorize(g.Name)
		if g.Quantity != "" {
			if q, err := ingredients.ParseQuantity(g.Quantity); err == nil {
				line.Quantity, line.UnitCode = &q, g.Unit
			}
		}
		lines = append(lines, line)
	}
	return lines, nil
}
