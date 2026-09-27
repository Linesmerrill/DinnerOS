package skips

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

// RecipeReader loads a household's recipe, so a recipe skip is only ever made
// for a recipe of the household's own and carries its name. *recipes.Service
// implements it.
type RecipeReader interface {
	// Get returns recipes.ErrNotFound for a recipe outside the household.
	Get(ctx context.Context, householdID, id string) (recipes.Recipe, error)
}

// Service implements skipped ingredients. Reads take a household ID (HTTP
// routes authorize household.view); writes take the actor and check plan.edit,
// since a skip changes what the week's grocery list asks anyone to buy.
type Service struct {
	store Store
	// catalog is optional; without it a skip made from free text still matches
	// free-text lines, just not catalogued ones.
	catalog Catalog
	// recipes is optional; without it a recipe skip can't be checked, so
	// ScopeRecipe is refused.
	recipes RecipeReader
	logger  *slog.Logger
	now     func() time.Time
}

// WithRecipes lets the service make recipe skips (ScopeRecipe), and returns s.
func (s *Service) WithRecipes(r RecipeReader) *Service {
	s.recipes = r
	return s
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
	var recipeName string
	if normalized.Scope == ScopeRecipe {
		if recipeName, err = s.recipeName(ctx, actor.HouseholdID, normalized.RecipeID); err != nil {
			return Skip{}, false, err
		}
	}
	// Only a new skip can push the household over the limit; replacing one
	// never does, so an existing skip stays changeable at the cap.
	existing, err := s.store.ListSkips(ctx, actor.HouseholdID)
	if err != nil {
		return Skip{}, false, fmt.Errorf("skips: list: %w", err)
	}
	isNew := true
	for _, e := range existing {
		if e.IngredientKey == normalized.IngredientKey && e.RecipeID == normalized.RecipeID {
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
		RecipeID: normalized.RecipeID, RecipeName: recipeName,
		CreatedBy: actor.UserID, CreatedAt: now, UpdatedBy: actor.UserID, UpdatedAt: now,
	})
	if err != nil {
		return Skip{}, false, fmt.Errorf("skips: put: %w", err)
	}
	return stored, created, nil
}

// recipeName checks that id is one of the household's recipes and returns its
// name.
func (s *Service) recipeName(ctx context.Context, householdID, id string) (string, error) {
	if s.recipes == nil {
		return "", invalid("leaving an ingredient out of one recipe isn't available")
	}
	r, err := s.recipes.Get(ctx, householdID, id)
	if errors.Is(err, recipes.ErrNotFound) {
		return "", invalid("recipeId is not one of this household's recipes")
	}
	if err != nil {
		return "", fmt.Errorf("skips: load recipe: %w", err)
	}
	return r.Name, nil
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
//
// Recipe skips go into SkipRules.Recipes, by recipe, and apply to lines of that
// recipe only. When two ingredient-wide skips land on one key (a free-text and
// a catalog spelling of the same ingredient), always outranks week.
func (s *Service) GrocerySkips(ctx context.Context, householdID, week string) (grocery.SkipRules, error) {
	if householdID == "" {
		return grocery.SkipRules{}, errHouseholdRequired
	}
	applies, keysOf, err := s.resolved(ctx, householdID, func(skip Skip) bool { return skip.AppliesTo(week) })
	if err != nil {
		return grocery.SkipRules{}, err
	}
	rules := grocery.SkipRules{Ingredients: grocery.SkipSet{}, Recipes: map[string]map[string]bool{}}
	for _, skip := range applies {
		if skip.Scope == ScopeRecipe {
			keys := rules.Recipes[skip.RecipeID]
			if keys == nil {
				keys = map[string]bool{}
				rules.Recipes[skip.RecipeID] = keys
			}
			for _, key := range keysOf(skip) {
				keys[key] = true
			}
			continue
		}
		scope := skip.Scope.Grocery()
		for _, key := range keysOf(skip) {
			if current, ok := rules.Ingredients[key]; !ok || scope.Outranks(current) {
				rules.Ingredients[key] = scope
			}
		}
	}
	return rules, nil
}

// RecipeLeftOut returns what the household leaves out of one recipe when it
// cooks it: its recipe skips for that recipe, and its always-skips. A week
// skip is about what to buy this week, not how the dish is cooked, so it is
// not included. Cooking instructions and cook deductions read it.
func (s *Service) RecipeLeftOut(ctx context.Context, householdID, recipeID string) (grocery.LeftOutSet, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	applies, keysOf, err := s.resolved(ctx, householdID, func(skip Skip) bool {
		return skip.Scope == ScopeAlways || skip.Scope == ScopeRecipe && skip.RecipeID == recipeID
	})
	if err != nil {
		return nil, err
	}
	set := grocery.LeftOutSet{}
	for _, skip := range applies {
		lo := grocery.LeftOut{SkipID: skip.ID, Scope: skip.Scope.Grocery()}
		for _, key := range keysOf(skip) {
			if current, ok := set[key]; !ok || lo.Scope.Outranks(current.Scope) {
				set[key] = lo
			}
		}
	}
	return set, nil
}

// resolved lists the household's skips that keep says apply, and a function
// giving every key one of them matches: the key it was made from, its
// normalized name, and its catalog ingredient ID when it was made from free
// text and the catalog knows the name.
func (s *Service) resolved(ctx context.Context, householdID string, keep func(Skip) bool) ([]Skip, func(Skip) []string, error) {
	stored, err := s.store.ListSkips(ctx, householdID)
	if err != nil {
		return nil, nil, fmt.Errorf("skips: list: %w", err)
	}
	applies := make([]Skip, 0, len(stored))
	var unlinked []string
	for _, skip := range stored {
		if !keep(skip) {
			continue
		}
		applies = append(applies, skip)
		if skip.Key != "" && skip.IngredientKey == UnresolvedKeyPrefix+skip.Key && !slices.Contains(unlinked, skip.Key) {
			unlinked = append(unlinked, skip.Key)
		}
	}
	catalogIDs := map[string]string{}
	if len(unlinked) > 0 && s.catalog != nil {
		found, err := s.catalog.IngredientsByKey(ctx, unlinked)
		if err != nil {
			return nil, nil, fmt.Errorf("skips: resolve skipped ingredients in the catalog: %w", err)
		}
		for _, ing := range found {
			catalogIDs[ing.Key] = ing.ID
		}
	}
	keysOf := func(skip Skip) []string {
		keys := skip.Keys()
		if id := catalogIDs[skip.Key]; id != "" {
			keys = append(keys, id)
		}
		return keys
	}
	return applies, keysOf, nil
}
