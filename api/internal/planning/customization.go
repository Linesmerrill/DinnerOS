package planning

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
)

// This file holds planning's hooks for meal customizations (package
// customize): entries store the member's choices, and the grocery list asks a
// CustomizationSource to apply them. Validation and the protein table live in
// package customize.

// CustomizationSource applies meal customizations to the grocery selections
// of a week's entries. *customize.Service implements it.
type CustomizationSource interface {
	// CustomizeGrocery returns selections with each entry's customized lines
	// replaced. entries[i] is the entry selections[i] was built from.
	CustomizeGrocery(ctx context.Context, householdID string, entries []Entry, selections []grocery.RecipeSelection) ([]grocery.RecipeSelection, error)
}

// entrySearchWeeksBefore is how many weeks before a cooked meal FindEntry
// looks for its entry; it also looks one week after.
const entrySearchWeeksBefore = 8

// WithCustomizations makes GroceryList apply customized entries through
// source, and returns s.
func (s *Service) WithCustomizations(source CustomizationSource) *Service {
	s.customizations = source
	return s
}

// SetEntryCustomizations replaces an entry's customizations; an empty list
// removes them. The caller validates them (customize.Service). Like
// UpdateEntry it fails with ErrNotFound or ErrFinalized.
func (s *Service) SetEntryCustomizations(ctx context.Context, householdID, week, entryID string, list []Customization) (Plan, error) {
	w, err := s.parse(householdID, week)
	if err != nil {
		return Plan{}, err
	}
	list = slices.Clone(list)
	if list == nil {
		list = []Customization{}
	}
	return s.written(ctx, householdID, func() (Plan, error) {
		return s.store.UpdateEntry(ctx, householdID, w, entryID, EntryChanges{Customizations: &list}, s.now().UTC())
	})
}

// FindEntry finds an entry by ID in the household's plans from
// entrySearchWeeksBefore weeks before near's week to the week after, for
// callers that know when a meal was cooked but not its week. found is false
// when the entry isn't in that range.
func (s *Service) FindEntry(ctx context.Context, householdID, entryID string, near time.Time) (p Plan, e Entry, found bool, err error) {
	if householdID == "" {
		return Plan{}, Entry{}, false, errHouseholdRequired
	}
	w := WeekOf(near)
	plans, err := s.store.ListPlans(ctx, householdID, w.AddWeeks(-entrySearchWeeksBefore), w.AddWeeks(1))
	if err != nil {
		return Plan{}, Entry{}, false, err
	}
	for _, p := range plans {
		if e, ok := p.entry(entryID); ok {
			p, err := s.stamp(ctx, householdID, p, nil)
			return p, e, err == nil, err
		}
	}
	return Plan{}, Entry{}, false, nil
}

// customizeGrocery applies the customization source to the week's
// selections when there is one and some entry is customized. selections
// holds one selection per entry not in skipped, in entry order.
func (s *Service) customizeGrocery(ctx context.Context, householdID string, p Plan, selections []grocery.RecipeSelection, skipped []SkippedEntry) ([]grocery.RecipeSelection, error) {
	if s.customizations == nil {
		return selections, nil
	}
	left := make(map[string]bool, len(skipped))
	for _, sk := range skipped {
		left[sk.EntryID] = true
	}
	entries := make([]Entry, 0, len(selections))
	customized := false
	for _, e := range p.Entries {
		if left[e.ID] {
			continue
		}
		entries = append(entries, e)
		customized = customized || len(e.Customizations) > 0
	}
	if !customized || len(entries) != len(selections) {
		return selections, nil
	}
	out, err := s.customizations.CustomizeGrocery(ctx, householdID, entries, selections)
	if err != nil {
		return nil, fmt.Errorf("planning: apply meal customizations: %w", err)
	}
	return out, nil
}
