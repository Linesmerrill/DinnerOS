package recommendations

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// Proposals carry pairings on each slot: add-ons and grocery items offered
// with the proposed meal. Pairings from always rules are included by default;
// accepting adds the included ones (or exactly the ones the member chose) with
// the meals.

// ProposalPairingID identifies a slot's pairing: "<slotId>/<key>".
func ProposalPairingID(slotID, key string) string { return slotID + "/" + key }

// attachProposalPairings sets the pairings of p's slots (only slotID's when
// set). Failures are logged: a proposal without pairings is still useful.
func (s *Service) attachProposalPairings(ctx context.Context, p *Proposal, plan planning.Plan, slotID string) {
	if s.pairings == nil {
		return
	}
	w, err := planning.ParseWeek(p.Week)
	if err != nil {
		return
	}
	d, err := s.loadPairings(ctx, p.HouseholdID, w)
	if err != nil {
		s.logger.WarnContext(ctx, "suggest pairings for a proposal", "householdId", p.HouseholdID, "week", p.Week, "error", err)
		return
	}
	for i := range p.Slots {
		sl := &p.Slots[i]
		if slotID != "" && sl.ID != slotID {
			continue
		}
		sl.Pairings = nil
		for _, pr := range visiblePairings(d.suggest(pairingSubject{SlotID: sl.ID, RecipeID: sl.RecipeID, Day: sl.Day, Servings: sl.Servings}, plan)) {
			pr.Included = pr.Frequency == PairingAlways
			sl.Pairings = append(sl.Pairings, pr)
		}
	}
}

// recordProposalSuggestions records pairing.suggested for the pairings of p's
// slots (only slotID's when set).
func (s *Service) recordProposalSuggestions(ctx context.Context, p Proposal, userID, slotID string, now time.Time) {
	for _, sl := range p.Slots {
		if slotID != "" && sl.ID != slotID {
			continue
		}
		for _, pr := range sl.Pairings {
			payload := suggestedPayload(pr)
			payload.ProposalID, payload.SlotID, payload.Day = p.ID, sl.ID, sl.Day
			s.record(ctx, events.Event{
				HouseholdID: p.HouseholdID, UserID: userID, Type: events.TypePairingSuggested, RecipeID: sl.RecipeID, Week: p.Week,
				OccurredAt: now, Payload: payload,
			})
		}
	}
}

type proposalPairing struct {
	id      string
	slot    Slot
	pairing Pairing
}

// chooseProposalPairings returns the pairings accepting adds, in slot order:
// the included ones when ids is nil, otherwise exactly ids. Pairings of
// excluded slots are ignored.
func chooseProposalPairings(p Proposal, excluded []string, ids *[]string) ([]proposalPairing, error) {
	chosen := map[string]bool{}
	if ids != nil {
		for _, id := range *ids {
			slotID, key, _ := strings.Cut(id, "/")
			i := p.slot(slotID)
			if i < 0 || indexByKey(p.Slots[i].Pairings, key) < 0 {
				return nil, invalidf("pairingIds: %q is not a pairing of this proposal", id)
			}
			chosen[id] = true
		}
	}
	var out []proposalPairing
	for _, sl := range p.Slots {
		if slices.Contains(excluded, sl.ID) {
			continue
		}
		for _, pr := range sl.Pairings {
			id := ProposalPairingID(sl.ID, pr.Key)
			if (ids == nil && pr.Included) || chosen[id] {
				out = append(out, proposalPairing{id: id, slot: sl, pairing: pr})
			}
		}
	}
	return out, nil
}

// AddedPairing is a pairing accepting a proposal added.
type AddedPairing struct {
	ID          string
	SlotID      string
	Pairing     Pairing
	Entry       *planning.Entry
	GroceryItem *WeekGroceryItem
}

// SkippedPairing is a chosen pairing accepting didn't add.
type SkippedPairing struct {
	ID     string
	SlotID string
	Key    string
	// Reason is PairingSkipAlreadyPlanned or PairingSkipUnavailable.
	Reason string
}

// plannedPairings splits the chosen pairings of added slots into add-on plan
// entries (in order) and grocery items, skipping add-ons already planned that
// day or no longer plannable.
func (s *Service) plannedPairings(ctx context.Context, householdID string, p Proposal, plan planning.Plan, chosen []proposalPairing, addedSlots []string) (entries []planning.NewEntry, addons, groceries []proposalPairing, skipped []SkippedPairing) {
	for _, c := range chosen {
		if !slices.Contains(addedSlots, c.slot.ID) {
			continue
		}
		if c.pairing.GroceryItem != nil {
			groceries = append(groceries, c)
			continue
		}
		recipeID := c.pairing.Recipe.ID
		if slices.ContainsFunc(plan.Entries, func(e planning.Entry) bool { return e.RecipeID == recipeID && string(e.Day) == c.slot.Day }) {
			skipped = append(skipped, SkippedPairing{ID: c.id, SlotID: c.slot.ID, Key: c.pairing.Key, Reason: PairingSkipAlreadyPlanned})
			continue
		}
		r, err := s.recipes.Get(ctx, householdID, recipeID)
		if err != nil || !r.IsAddon || !slices.Contains(r.Servings, c.pairing.Servings) {
			skipped = append(skipped, SkippedPairing{ID: c.id, SlotID: c.slot.ID, Key: c.pairing.Key, Reason: PairingSkipUnavailable})
			continue
		}
		entries = append(entries, planning.NewEntry{
			RecipeID: recipeID, Day: c.slot.Day, Servings: c.pairing.Servings, Origin: planning.OriginAutopilot, ProposalID: p.ID,
		})
		addons = append(addons, c)
	}
	return entries, addons, groceries, skipped
}

// finishProposalPairings records what accepting added: add-on entries and
// grocery items for the meals' new entries, as week decisions, plus events.
// The plan has already changed, so failures to save are logged.
func (s *Service) finishProposalPairings(ctx context.Context, householdID, userID string, p Proposal, w planning.Week, mealEntries map[string]planning.Entry,
	addons []proposalPairing, addonEntries []planning.Entry, groceries []proposalPairing, chosen []proposalPairing, explicit bool, now time.Time,
) []AddedPairing {
	var added []AddedPairing
	var decisions []PairingDecision
	for i, c := range addons {
		main := mealEntries[c.slot.ID]
		e := addonEntries[i]
		added = append(added, AddedPairing{ID: c.id, SlotID: c.slot.ID, Pairing: c.pairing, Entry: &e})
		decisions = append(decisions, PairingDecision{EntryID: main.ID, Key: c.pairing.Key, Status: DecisionAccepted, AddedEntryID: e.ID, DecidedBy: userID, DecidedAt: now})
	}
	// A grocery item goes on the list once, for the first meal that chose it.
	var items []WeekGroceryItem
	itemChoice := map[string]proposalPairing{}
	for _, c := range groceries {
		main, ok := mealEntries[c.slot.ID]
		if !ok || slices.ContainsFunc(items, func(g WeekGroceryItem) bool { return g.Key == c.pairing.Key }) {
			continue
		}
		g := WeekGroceryItem{
			ID: newID(), Key: c.pairing.Key, GroceryItem: *c.pairing.GroceryItem, ForEntryID: main.ID, ForRecipeID: main.RecipeID,
			ForRecipeName: c.slot.RecipeName, Source: c.pairing.Source, RuleID: c.pairing.RuleID, AddedBy: userID, AddedAt: now,
		}
		items, itemChoice[g.ID] = append(items, g), c
	}
	if s.pairings != nil && len(decisions)+len(items) > 0 {
		for _, g := range s.saveProposalPairings(ctx, householdID, userID, w, decisions, items, now) {
			c := itemChoice[g.ID]
			added = append(added, AddedPairing{ID: c.id, SlotID: c.slot.ID, Pairing: c.pairing, GroceryItem: &g})
		}
	}
	for _, a := range added {
		payload := acceptedPayload(a.Pairing)
		main := mealEntries[a.SlotID]
		payload.EntryID, payload.ProposalID, payload.SlotID, payload.Day = main.ID, p.ID, a.SlotID, string(main.Day)
		if a.Entry != nil {
			payload.AddedEntryID = a.Entry.ID
		}
		if a.GroceryItem != nil {
			payload.GroceryItemID = a.GroceryItem.ID
		}
		s.record(ctx, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypePairingAccepted, RecipeID: main.RecipeID, Week: w.String(), OccurredAt: now, Payload: payload,
		})
	}
	if !explicit {
		return added
	}
	// Pairings the proposal included that the member left out.
	for _, sl := range p.Slots {
		main, ok := mealEntries[sl.ID]
		if !ok {
			continue
		}
		for _, pr := range sl.Pairings {
			id := ProposalPairingID(sl.ID, pr.Key)
			if !pr.Included || slices.ContainsFunc(chosen, func(c proposalPairing) bool { return c.id == id }) {
				continue
			}
			s1 := suggestedPayload(pr)
			s.record(ctx, events.Event{
				HouseholdID: householdID, UserID: userID, Type: events.TypePairingDismissed, RecipeID: sl.RecipeID, Week: w.String(), OccurredAt: now,
				Payload: events.PairingDismissed{
					Key: s1.Key, Kind: s1.Kind, Source: s1.Source, Frequency: s1.Frequency, MealCategory: s1.MealCategory, RuleID: s1.RuleID,
					Confidence: s1.Confidence, EntryID: main.ID, ProposalID: p.ID, SlotID: sl.ID, Day: sl.Day, Reason: "excluded",
				},
			})
		}
	}
	return added
}

// saveProposalPairings adds decisions and grocery items to the week's state,
// skipping grocery items the list already has, and returns the items it
// added. Failures are logged and add nothing.
func (s *Service) saveProposalPairings(ctx context.Context, householdID, userID string, w planning.Week, decisions []PairingDecision, items []WeekGroceryItem, now time.Time) []WeekGroceryItem {
	for range maxAttempts {
		state, err := s.pairings.GetWeekPairings(ctx, householdID, w.String())
		switch {
		case isNotFound(err):
			state = WeekPairings{HouseholdID: householdID, Week: w.String()}
		case err != nil:
			s.logger.ErrorContext(ctx, "save accepted proposal pairings", "householdId", householdID, "week", w.String(), "error", err)
			return nil
		}
		state.Decisions, state.GroceryItems = slices.Clone(state.Decisions), slices.Clone(state.GroceryItems)
		for _, dec := range decisions {
			state.setDecision(dec)
		}
		var saved []WeekGroceryItem
		for _, g := range items {
			if state.groceryItemByKey(g.Key) >= 0 || len(state.GroceryItems) >= MaxWeekGroceryItems {
				continue
			}
			state.GroceryItems = append(state.GroceryItems, g)
			state.setDecision(PairingDecision{EntryID: g.ForEntryID, Key: g.Key, Status: DecisionAccepted, GroceryItemID: g.ID, DecidedBy: userID, DecidedAt: now})
			saved = append(saved, g)
		}
		state.UpdatedAt = now
		_, err = s.pairings.SaveWeekPairings(ctx, state)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			s.logger.ErrorContext(ctx, "save accepted proposal pairings", "householdId", householdID, "week", w.String(), "error", err)
			return nil
		}
		return saved
	}
	return nil
}

// addonRecipes returns which of the plan's recipes are add-ons. Add-on
// entries go with a meal and don't take its day.
func (s *Service) addonRecipes(ctx context.Context, householdID string, plan planning.Plan) (map[string]bool, error) {
	out := map[string]bool{}
	if len(plan.Entries) == 0 {
		return out, nil
	}
	catalog, err := s.recipes.Catalog(ctx, householdID)
	if err != nil {
		return nil, err
	}
	for _, r := range catalog {
		if r.IsAddon {
			out[r.ID] = true
		}
	}
	return out, nil
}
