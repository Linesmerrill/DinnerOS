package recipes

import (
	"context"
	"errors"
	"fmt"
)

// CatalogPublisher publishes recipes into the global recipe catalog.
// *catalog.Service implements it.
//
// This is the seam every writer of recipes shares. Anything that brings
// recipes in from a public source — the HTTP import endpoint, the operator
// importrecipes command, a background account importer — calls Import, which
// calls PublishToCatalog for it; a writer that does not go through Import can
// call PublishToCatalog directly. The publisher decides nothing about
// permission: it publishes exactly the recipes Publishable accepts and copies
// only recipe content, never a household's identity, order history, notes, or
// ratings.
//
// Recipes flow one way. The catalog never writes back into a household's
// library, so a publisher failing, lagging, or being absent can only mean a
// thinner catalog, never a wrong library.
type CatalogPublisher interface {
	// PublishRecipes upserts each recipe into the global catalog by its
	// CatalogKey and returns how many were written. It is idempotent:
	// publishing the same recipe twice writes it once.
	PublishRecipes(ctx context.Context, rs []Recipe) (int, error)
}

// WithCatalog wires the global recipe catalog and returns s, so it can be
// chained onto NewService. Without it the service never publishes and
// everything else works unchanged.
func (s *Service) WithCatalog(p CatalogPublisher) *Service {
	s.catalog = p
	return s
}

// PublishToCatalog publishes the publishable subset of rs to the global
// catalog and returns how many entries were written. It is a no-op without a
// publisher.
func (s *Service) PublishToCatalog(ctx context.Context, rs []Recipe) (int, error) {
	if s.catalog == nil || len(rs) == 0 {
		return 0, nil
	}
	publishable := make([]Recipe, 0, len(rs))
	for _, r := range rs {
		if Publishable(r) {
			publishable = append(publishable, r)
		}
	}
	if len(publishable) == 0 {
		return 0, nil
	}
	n, err := s.catalog.PublishRecipes(ctx, publishable)
	if err != nil {
		return 0, fmt.Errorf("publish to catalog: %w", err)
	}
	return n, nil
}

// BackfillResult reports what Backfill did (or would do) for one household.
type BackfillResult struct {
	// Read is how many of the household's recipes were examined.
	Read int
	// Keyed is how many were rewritten to give them a catalogKey.
	Keyed int
	// Publishable is how many were eligible for the global catalog, and
	// Published how many entries the catalog actually wrote.
	Publishable int
	Published   int
}

// BackfillBatchSize is how many recipes Backfill reads at a time.
const BackfillBatchSize = 200

// Backfill gives a household's existing recipes their catalogKey and
// publishes the public-source ones to the global catalog.
//
// Recipes stored before the catalog existed have no catalogKey, so nothing
// marks them as already-in-library and discovery would offer a household the
// recipes it cooks every week. The key is derived on write, so re-saving a
// recipe unchanged is the backfill; re-running is safe and writes only what
// is still missing.
//
// It is a dry run unless apply is true.
func (s *Service) Backfill(ctx context.Context, householdID string, apply bool) (BackfillResult, error) {
	if householdID == "" {
		return BackfillResult{}, errHouseholdRequired
	}
	var res BackfillResult
	after := ""
	for {
		page, err := s.store.ListRecipesAfter(ctx, householdID, after, BackfillBatchSize)
		if err != nil {
			return res, fmt.Errorf("list recipes: %w", err)
		}
		if len(page) == 0 {
			return res, nil
		}
		after = page[len(page)-1].ID
		res.Read += len(page)

		var rewrite, publish []Recipe
		for _, r := range page {
			if r.CatalogKey != CatalogKey(r) {
				rewrite = append(rewrite, r)
			}
			if Publishable(r) {
				publish = append(publish, r)
			}
		}
		res.Keyed += len(rewrite)
		res.Publishable += len(publish)
		if !apply {
			continue
		}
		if len(rewrite) > 0 {
			if err := s.store.SaveRecipes(ctx, householdID, rewrite); err != nil {
				return res, fmt.Errorf("save recipes: %w", err)
			}
		}
		published, err := s.PublishToCatalog(ctx, publish)
		if err != nil {
			return res, err
		}
		res.Published += published
	}
}

// Share sets a recipe's catalog opt-in. Sharing a recipe from a private
// source publishes it; un-sharing stops it being re-published but does not
// retract the catalog entry, because other households may already have copied
// it into their own libraries.
//
// Callers authorize first: the HTTP route requires households.PermRecipesEdit.
func (s *Service) Share(ctx context.Context, householdID, recipeID string, shared bool) (Recipe, error) {
	if householdID == "" {
		return Recipe{}, errHouseholdRequired
	}
	r, err := s.store.SetRecipeShared(ctx, householdID, recipeID, shared, s.now().UTC())
	if err != nil {
		return Recipe{}, err
	}
	if shared {
		if _, err := s.PublishToCatalog(ctx, []Recipe{r}); err != nil {
			return Recipe{}, err
		}
	}
	return r, nil
}

// AddFromCatalog copies a catalog recipe into the household's library and
// returns the stored recipe and whether this call created it.
//
// It is a copy, not a reference. From here on the household owns the recipe:
// it plans, cooks, rates, and edits its own document, and a later change to
// the catalog entry leaves it alone. That is the point — a household that
// cooked a meal last Tuesday should still be able to read the steps it
// cooked from, and the grocery list of an accepted week should not change
// because a meal-kit service reworded a recipe.
//
// It is idempotent: a household that already has this recipe, whether it
// added it here or imported it itself, gets its existing one back untouched,
// with its ratings and order history intact.
//
// content must be household-free (catalog.Content). Callers authorize first:
// the HTTP route requires households.PermRecipesEdit.
func (s *Service) AddFromCatalog(ctx context.Context, householdID string, content Recipe) (Recipe, bool, error) {
	if householdID == "" {
		return Recipe{}, false, errHouseholdRequired
	}
	key := CatalogKey(content)
	if key == "" {
		return Recipe{}, false, fmt.Errorf("%w: recipe has no catalog identity", ErrInvalidImport)
	}
	if existing, err := s.existingByCatalogKey(ctx, householdID, key); err != nil {
		return Recipe{}, false, err
	} else if existing.ID != "" {
		return existing, false, nil
	}

	now := s.now().UTC()
	r := content
	r.ID = ""
	r.HouseholdID = householdID
	r.OrderWeeks, r.TimesOrdered, r.LastOrderedWeek = nil, 0, ""
	r.SharedToCatalog = false
	r.CreatedAt, r.UpdatedAt = now, now
	if err := s.store.SaveRecipes(ctx, householdID, []Recipe{r}); err != nil {
		// The unique {householdId, source, sourceRecipeId} index is the last
		// word when two members add the same recipe at once.
		if !errors.Is(err, ErrDuplicate) {
			return Recipe{}, false, fmt.Errorf("save recipes: %w", err)
		}
	}
	stored, err := s.existingByCatalogKey(ctx, householdID, key)
	if err != nil {
		return Recipe{}, false, err
	}
	if stored.ID == "" {
		return Recipe{}, false, fmt.Errorf("recipes: added recipe %q is missing after the write", key)
	}
	return stored, true, nil
}

func (s *Service) existingByCatalogKey(ctx context.Context, householdID, key string) (Recipe, error) {
	found, err := s.store.LibraryCatalogKeys(ctx, householdID, []string{key})
	if err != nil {
		return Recipe{}, fmt.Errorf("library catalog keys: %w", err)
	}
	id, ok := found[key]
	if !ok {
		return Recipe{}, nil
	}
	r, err := s.Get(ctx, householdID, id)
	if err != nil {
		return Recipe{}, err
	}
	return r, nil
}

// InLibrary maps each of keys the household already has a recipe for to that
// recipe's ID. Discovery and catalog search use it to mark results, so a
// household never sees a catalog entry it already owns presented as new.
func (s *Service) InLibrary(ctx context.Context, householdID string, keys []string) (map[string]string, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	found, err := s.store.LibraryCatalogKeys(ctx, householdID, keys)
	if err != nil {
		return nil, fmt.Errorf("library catalog keys: %w", err)
	}
	if found == nil {
		found = map[string]string{}
	}
	return found, nil
}
