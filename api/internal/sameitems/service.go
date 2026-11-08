package sameitems

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Catalog resolves global ingredient catalog entries. *recipes.Service
// implements it.
type Catalog interface {
	IngredientsByKey(ctx context.Context, keys []string) ([]recipes.Ingredient, error)
}

// Service implements same-item links. Reads take a household ID (HTTP routes
// authorize household.view); writes take the actor and check plan.edit, since
// a link changes what the week's grocery list asks anyone to buy.
type Service struct {
	store Store
	// catalog is optional; without it a link made from free text matches
	// free-text lines only.
	catalog Catalog
	now     func() time.Time
}

// NewService returns a Service.
func NewService(store Store, catalog Catalog) *Service {
	return &Service{store: store, catalog: catalog, now: time.Now}
}

func authorizeEdit(actor households.Membership) error {
	if !actor.Role.Can(households.PermPlanEdit) {
		return ErrForbidden
	}
	if actor.HouseholdID == "" {
		return errHouseholdRequired
	}
	return nil
}

// List returns the household's links, oldest first.
func (s *Service) List(ctx context.Context, householdID string) ([]Link, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	return s.store.ListLinks(ctx, householdID)
}

// Link says in.VariantKey is the same item as in.KeptKey. Links never chain:
// linking to an item that is itself a variant links to that item's kept one,
// and items that were kept as the new variant move to the new kept item.
// Linking a name that was already linked elsewhere replaces that link.
// created is false when it replaced or matched one.
func (s *Service) Link(ctx context.Context, actor households.Membership, in Input) (Link, bool, error) {
	if err := authorizeEdit(actor); err != nil {
		return Link{}, false, err
	}
	in, err := in.normalize()
	if err != nil {
		return Link{}, false, err
	}
	existing, err := s.store.ListLinks(ctx, actor.HouseholdID)
	if err != nil {
		return Link{}, false, fmt.Errorf("sameitems: list: %w", err)
	}
	isNew := true
	for _, l := range existing {
		switch l.VariantKey {
		case in.KeptKey:
			in.KeptKey, in.KeptName = l.KeptKey, l.KeptName
		case in.VariantKey:
			isNew = false
		}
	}
	if in.KeptKey == in.VariantKey {
		return Link{}, false, invalid("These are already the same item.")
	}
	if isNew && len(existing) >= MaxPerHousehold {
		return Link{}, false, invalid("You can link up to %d items.", MaxPerHousehold)
	}
	stored, created, err := s.store.PutLink(ctx, Link{
		HouseholdID: actor.HouseholdID, VariantKey: in.VariantKey, VariantName: in.VariantName,
		KeptKey: in.KeptKey, KeptName: in.KeptName, CreatedBy: actor.UserID, CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return Link{}, false, fmt.Errorf("sameitems: put: %w", err)
	}
	if err := s.store.RepointKept(ctx, actor.HouseholdID, in.VariantKey, in.KeptKey, in.KeptName); err != nil {
		return Link{}, false, fmt.Errorf("sameitems: repoint: %w", err)
	}
	return stored, created, nil
}

// Unlink makes the variant its own item again. Unlinking a link that isn't
// there is ErrNotFound.
func (s *Service) Unlink(ctx context.Context, actor households.Membership, id string) error {
	if err := authorizeEdit(actor); err != nil {
		return err
	}
	return s.store.DeleteLink(ctx, actor.HouseholdID, id)
}

// GrocerySameItems returns the household's links in the form the grocery
// engine takes. Each variant answers for every key its lines can carry: the
// key it was linked from, "name:" + its normalized name, and its catalog
// ingredient ID when the catalog knows that name, the same spread a skip gets.
func (s *Service) GrocerySameItems(ctx context.Context, householdID string) (grocery.SameItems, error) {
	links, err := s.List(ctx, householdID)
	if err != nil || len(links) == 0 {
		return nil, err
	}
	var names []string
	for _, l := range links {
		if name := ingredients.NormalizeName(l.VariantName); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	catalogIDs := map[string]string{}
	if s.catalog != nil {
		found, err := s.catalog.IngredientsByKey(ctx, names)
		if err != nil {
			return nil, fmt.Errorf("sameitems: resolve names in the catalog: %w", err)
		}
		for _, ing := range found {
			catalogIDs[ing.Key] = ing.ID
		}
	}
	out := make(grocery.SameItems, 0, len(links))
	for _, l := range links {
		keys := []string{l.VariantKey}
		add := func(k string) {
			if k != "" && k != l.KeptKey && !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
		name := ingredients.NormalizeName(l.VariantName)
		add(UnresolvedKeyPrefix + name)
		// Only a free-text variant is widened to the catalog: a catalogued
		// one already is its catalog ID.
		if strings.HasPrefix(l.VariantKey, UnresolvedKeyPrefix) {
			add(catalogIDs[name])
		}
		out = append(out, grocery.SameItem{
			LinkID: l.ID, VariantKeys: keys, VariantName: l.VariantName, KeptKey: l.KeptKey, KeptName: l.KeptName,
		})
	}
	return out, nil
}

// RecipeLeftOuts is what a household leaves out of one recipe.
// *skips.Service implements it.
type RecipeLeftOuts interface {
	RecipeLeftOut(ctx context.Context, householdID, recipeID string) (grocery.LeftOutSet, error)
}

// LeftOut wraps what a household leaves out of a recipe so a skip under one
// of two linked names covers the other in cooking steps and cook deductions.
type LeftOut struct {
	Skips RecipeLeftOuts
	Same  *Service
}

// RecipeLeftOut implements the cooking views' left-out source.
func (l LeftOut) RecipeLeftOut(ctx context.Context, householdID, recipeID string) (grocery.LeftOutSet, error) {
	set, err := l.Skips.RecipeLeftOut(ctx, householdID, recipeID)
	if err != nil || l.Same == nil || len(set) == 0 {
		return set, err
	}
	same, err := l.Same.GrocerySameItems(ctx, householdID)
	if err != nil {
		return nil, err
	}
	return same.LeftOut(set), nil
}
