package pantry

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// defaultStaple is an always-have item offered to new households. Aliases are
// other names the catalog may use for the same thing ("Pepper" for black
// pepper), so the staple links to the ingredient recipes actually reference.
type defaultStaple struct {
	Name    string
	Aliases []string
}

// DefaultStaples lists the staples AddDefaultStaples offers, in order.
var defaultStaples = []defaultStaple{
	{Name: "Salt", Aliases: []string{"Kosher Salt"}},
	{Name: "Black Pepper", Aliases: []string{"Pepper"}},
	{Name: "Cooking Oil", Aliases: []string{"Vegetable Oil"}},
	{Name: "Olive Oil"},
	{Name: "Butter", Aliases: []string{"Unsalted Butter"}},
	{Name: "Sugar", Aliases: []string{"Granulated Sugar"}},
	{Name: "Flour", Aliases: []string{"All-Purpose Flour"}},
	{Name: "Garlic Powder"},
	{Name: "Onion Powder"},
}

// DefaultStapleNames returns the names AddDefaultStaples uses.
func DefaultStapleNames() []string {
	out := make([]string, 0, len(defaultStaples))
	for _, d := range defaultStaples {
		out = append(out, d.Name)
	}
	return out
}

func (d defaultStaple) keys() []string {
	keys := []string{ingredients.NormalizeName(d.Name)}
	for _, alias := range d.Aliases {
		keys = append(keys, ingredients.NormalizeName(alias))
	}
	return keys
}

// AddDefaultStaples adds the default staples the pantry doesn't already have,
// in stock and marked isStaple. It is idempotent: a staple is skipped when the
// pantry has an item under its name or any alias, whatever that item's status
// or isStaple flag, so a household's own choices are never overwritten.
func (s *Service) AddDefaultStaples(ctx context.Context, actor households.Membership) (StaplesResult, error) {
	if err := authorizeEdit(actor); err != nil {
		return StaplesResult{}, err
	}
	var keys []string
	for _, d := range defaultStaples {
		keys = append(keys, d.keys()...)
	}
	catalog, err := s.catalog.IngredientsByKey(ctx, keys)
	if err != nil {
		return StaplesResult{}, fmt.Errorf("look up catalog ingredients: %w", err)
	}
	byKey := make(map[string]recipes.Ingredient, len(catalog))
	for _, ing := range catalog {
		byKey[ing.Key] = ing
	}
	existing, err := s.store.FindItemsByKeys(ctx, actor.HouseholdID, keys)
	if err != nil {
		return StaplesResult{}, fmt.Errorf("find pantry items: %w", err)
	}
	have := map[string]bool{}
	for _, item := range existing {
		have[item.Key] = true
	}

	var res StaplesResult
	var pending []Item
	now := s.timestamp()
	for _, d := range defaultStaples {
		candidates := d.keys()
		if slices.ContainsFunc(candidates, func(k string) bool { return have[k] }) {
			res.Skipped++
			continue
		}
		item := Item{
			HouseholdID: actor.HouseholdID, Key: candidates[0], DisplayName: d.Name,
			Status: StatusInStock, IsStaple: true,
			CreatedAt: now, UpdatedBy: actor.UserID, UpdatedAt: now,
		}
		item.Category, _ = ingredients.Categorize(d.Name)
		for _, k := range candidates {
			if ing, ok := byKey[k]; ok {
				item.IngredientID, item.Key, item.Category = ing.ID, ing.Key, catalogCategory(ing.Category)
				break
			}
		}
		pending = append(pending, item)
	}
	if len(pending) > 0 {
		if err := s.checkCapacity(ctx, actor.HouseholdID, len(pending)); err != nil {
			return StaplesResult{}, err
		}
	}
	for _, item := range pending {
		saved, err := s.store.InsertItem(ctx, item)
		if errors.Is(err, ErrDuplicate) {
			res.Skipped++ // added concurrently
			continue
		}
		if err != nil {
			return StaplesResult{}, fmt.Errorf("insert staple: %w", err)
		}
		res.Added = append(res.Added, saved)
	}
	return res, nil
}
