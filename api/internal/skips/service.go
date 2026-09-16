package skips

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Catalog resolves global ingredient catalog entries. *recipes.Service
// implements it.
type Catalog interface {
	IngredientsByKey(ctx context.Context, keys []string) ([]recipes.Ingredient, error)
}

// Service implements skipped ingredients. Reads take a household ID (HTTP
// routes authorize household.view); writes take the actor and check plan.edit,
// since a skip changes what the week's grocery list asks anyone to buy.
type Service struct {
	store Store
	// catalog is optional; without it a skip made from free text still matches
	// free-text lines, just not catalogued ones.
	catalog Catalog
	logger  *slog.Logger
	now     func() time.Time
}

// NewService returns a Service.
func NewService(store Store, catalog Catalog, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, catalog: catalog, logger: logger, now: time.Now}
}

func authorizeEdit(actor households.Membership) error {
	if !actor.Role.Can(households.PermPlanEdit) {
		return ErrForbidden
	}
	return nil
}

// List returns the household's skipped ingredients, newest first.
func (s *Service) List(ctx context.Context, householdID string) ([]Skip, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	return s.store.ListSkips(ctx, householdID)
}

// Set skips an ingredient, replacing the household's existing skip for it so
// "skip once" can become "skip forever" (or the other way round) in one call.
// created is false when it replaced one.
func (s *Service) Set(ctx context.Context, actor households.Membership, in Input) (Skip, bool, error) {
	if err := authorizeEdit(actor); err != nil {
		return Skip{}, false, err
	}
	if actor.HouseholdID == "" {
		return Skip{}, false, errHouseholdRequired
	}
	normalized, key, err := in.normalize()
	if err != nil {
		return Skip{}, false, err
	}
	// Only a new skip can push the household over the limit; replacing one
	// never does, so an existing skip stays changeable at the cap.
	existing, err := s.store.ListSkips(ctx, actor.HouseholdID)
	if err != nil {
		return Skip{}, false, fmt.Errorf("skips: list: %w", err)
	}
	isNew := true
	for _, e := range existing {
		if e.IngredientKey == normalized.IngredientKey {
			isNew = false
			break
		}
	}
	if isNew && len(existing) >= MaxPerHousehold {
		return Skip{}, false, invalid("a household can skip at most %d ingredients", MaxPerHousehold)
	}
	now := s.now().UTC()
	stored, created, err := s.store.PutSkip(ctx, Skip{
		HouseholdID: actor.HouseholdID, IngredientKey: normalized.IngredientKey, Key: key,
		Name: normalized.Name, Scope: normalized.Scope, Week: normalized.Week,
		CreatedBy: actor.UserID, CreatedAt: now, UpdatedBy: actor.UserID, UpdatedAt: now,
	})
	if err != nil {
		return Skip{}, false, fmt.Errorf("skips: put: %w", err)
	}
	return stored, created, nil
}

// Remove resumes an ingredient: the household buys it again from the next list
// on. Removing a skip that isn't there is ErrNotFound.
func (s *Service) Remove(ctx context.Context, actor households.Membership, id string) error {
	if err := authorizeEdit(actor); err != nil {
		return err
	}
	if actor.HouseholdID == "" {
		return errHouseholdRequired
	}
	return s.store.DeleteSkip(ctx, actor.HouseholdID, id)
}

// GrocerySkips returns the skips that apply to week, in the form the grocery
// engine takes.
//
// Each skip is registered under every key a grocery line for that ingredient
// can carry: the key it was made from, "name:" + its normalized name, and its
// catalog ingredient ID when the catalog knows that name. So skipping cilantro
// from a free-text line also skips a recipe that reaches cilantro through the
// catalog, which is the whole point of skipping an ingredient rather than a
// line.
//
// Week-scoped skips for other weeks are left out here rather than deleted:
// nothing is written as a side effect of building a list, and a member who
// opens last week's list still sees what was skipped then.
func (s *Service) GrocerySkips(ctx context.Context, householdID, week string) (grocery.SkipSet, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	stored, err := s.store.ListSkips(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("skips: list: %w", err)
	}
	applies := make([]Skip, 0, len(stored))
	var unlinked []string
	for _, skip := range stored {
		if !skip.AppliesTo(week) {
			continue
		}
		applies = append(applies, skip)
		if skip.Key != "" && skip.IngredientKey == UnresolvedKeyPrefix+skip.Key {
			unlinked = append(unlinked, skip.Key)
		}
	}
	catalogIDs := map[string]string{}
	if len(unlinked) > 0 && s.catalog != nil {
		found, err := s.catalog.IngredientsByKey(ctx, unlinked)
		if err != nil {
			return nil, fmt.Errorf("skips: resolve skipped ingredients in the catalog: %w", err)
		}
		for _, ing := range found {
			catalogIDs[ing.Key] = ing.ID
		}
	}
	set := grocery.SkipSet{}
	for _, skip := range applies {
		scope := grocery.SkipThisWeek
		if skip.Scope == ScopeAlways {
			scope = grocery.SkipAlways
		}
		for _, key := range skip.Keys() {
			set[key] = scope
		}
		if id := catalogIDs[skip.Key]; id != "" {
			set[id] = scope
		}
	}
	return set, nil
}
