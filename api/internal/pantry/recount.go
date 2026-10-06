package pantry

import (
	"context"
	"errors"
	"fmt"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Recount is what replaying one cooked meal found: the lines today's matching
// deducts that the meal's record didn't, and the record's lines with those in.
type Recount struct {
	Usage CookUsage
	// Added are lines now deducted that weren't: a specialty swap's
	// ingredients, a packet counted by what it holds.
	Added []CookLine
	// Lines are the record's lines with Added in place of what they replace.
	Lines []CookLine
}

// RecountCooked replays a meal that was already deducted with today's
// matching, so a household's pantry is caught up when the counting improves
// (decision 630). Only what the old record missed is deducted, and only into
// the stretch it belongs to: an item a person has set the amount of since the
// meal was cooked already accounts for it, and is left alone. With apply
// false nothing is written. The caller replaces the record's lines with
// Recount.Lines.
func (s *Service) RecountCooked(ctx context.Context, u CookUsage, apply bool) (Recount, error) {
	if s.usage == nil || s.recipes == nil {
		return Recount{}, errUsageNotConfigured
	}
	out := Recount{Usage: u, Lines: u.Lines}
	m := CookedMeal{HouseholdID: u.HouseholdID, RecipeID: u.RecipeID, EntryID: u.EntryID, UserID: u.UserID, Servings: u.Servings, OccurredAt: u.OccurredAt}
	recipe, err := s.recipes.Get(ctx, m.HouseholdID, m.RecipeID)
	if errors.Is(err, recipes.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return Recount{}, fmt.Errorf("load cooked recipe: %w", err)
	}
	needs, _, err := s.cookNeeds(ctx, m, recipe)
	if err != nil {
		return Recount{}, err
	}
	lines, err := s.matchNeeds(ctx, m, needs)
	if err != nil {
		return Recount{}, err
	}
	deducted, skipped := map[string]bool{}, map[string]bool{}
	for _, l := range u.Lines {
		if l.Deducted != "" {
			deducted[l.ItemID] = true
		} else if l.SkipReason == SkipNoAmount || l.SkipReason == SkipUnitMismatch {
			skipped[l.ItemID] = true
		}
	}
	byItem := map[string][]CookLine{}
	var order []string
	for _, l := range lines {
		if l.Deducted == "" || deducted[l.ItemID] {
			continue
		}
		if _, ok := byItem[l.ItemID]; !ok {
			order = append(order, l.ItemID)
		}
		byItem[l.ItemID] = append(byItem[l.ItemID], l)
		out.Added = append(out.Added, l)
	}
	if len(out.Added) == 0 {
		return out, nil
	}
	merged := make([]CookLine, 0, len(u.Lines)+len(out.Added))
	for _, l := range u.Lines {
		if _, replaced := byItem[l.ItemID]; !replaced {
			merged = append(merged, l)
		}
	}
	out.Lines = append(merged, out.Added...)
	if !apply {
		return out, nil
	}
	for _, itemID := range order {
		if err := s.deductItem(ctx, m, itemID, byItem[itemID]); err != nil {
			return Recount{}, fmt.Errorf("item %s: %w", itemID, err)
		}
		if skipped[itemID] {
			if err := s.unskip(ctx, m.HouseholdID, itemID); err != nil {
				return Recount{}, fmt.Errorf("item %s: %w", itemID, err)
			}
		}
	}
	return out, nil
}

// unskip takes back a use counted as uncounted once it has been counted.
func (s *Service) unskip(ctx context.Context, householdID, itemID string) error {
	for range maxWriteAttempts {
		item, err := s.store.GetItem(ctx, householdID, itemID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if item.Tracking == nil || (item.Tracking.SkippedUses == 0 && item.Tracking.SegmentSkippedUses == 0) {
			return nil
		}
		next := cloneItem(item)
		next.Tracking.SkippedUses = max(0, next.Tracking.SkippedUses-1)
		next.Tracking.SegmentSkippedUses = max(0, next.Tracking.SegmentSkippedUses-1)
		_, err = s.store.UpdateItem(ctx, next)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return err
	}
	return ErrConflict
}
