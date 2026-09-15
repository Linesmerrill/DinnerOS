package recipes

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Ingredient catalog search limits.
const (
	DefaultIngredientSearchLimit = 20
	MaxIngredientSearchLimit     = 50
)

// IngredientsByID returns the catalog ingredients with the given IDs, skipping
// IDs that do not exist or are malformed. Other modules (the pantry) use it to
// resolve references without reaching into this package's store.
func (s *Service) IngredientsByID(ctx context.Context, ids []string) ([]Ingredient, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return s.store.GetIngredients(ctx, ids)
}

// IngredientsByKey returns the catalog ingredients whose Key
// (ingredients.NormalizeName of the name) is any of keys.
func (s *Service) IngredientsByKey(ctx context.Context, keys []string) ([]Ingredient, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	return s.store.FindIngredients(ctx, nil, keys)
}

// SearchIngredients finds catalog ingredients for autocomplete. The query is
// normalized like catalog keys (case, accents, and punctuation are ignored)
// and matches the start of any word in the name. Results are ranked: the exact
// name first, then names starting with the query, then names with a later
// word starting with it; each group alphabetical. A limit of 0 means
// DefaultIngredientSearchLimit, and larger limits are capped at
// MaxIngredientSearchLimit.
//
// The catalog is global, so no household is involved.
func (s *Service) SearchIngredients(ctx context.Context, query string, limit int) ([]Ingredient, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > maxFilterLength {
		return nil, fmt.Errorf("%w: q must be at most %d characters", ErrInvalidQuery, maxFilterLength)
	}
	key := ingredients.NormalizeName(query)
	if key == "" {
		return nil, fmt.Errorf("%w: q must contain letters or digits", ErrInvalidQuery)
	}
	switch {
	case limit < 0:
		return nil, fmt.Errorf("%w: limit must not be negative", ErrInvalidQuery)
	case limit == 0:
		limit = DefaultIngredientSearchLimit
	case limit > MaxIngredientSearchLimit:
		limit = MaxIngredientSearchLimit
	}

	// Two queries keep ranking correct however many names match: prefix
	// matches can never be crowded out by alphabetically earlier word matches.
	// Within the prefix matches, ordered by key, an exact match always sorts
	// first because it is a prefix of every other one.
	quoted := regexp.QuoteMeta(key)
	out, err := s.store.SearchIngredients(ctx, "^"+quoted, limit)
	if err != nil {
		return nil, fmt.Errorf("search ingredients: %w", err)
	}
	if len(out) < limit {
		words, err := s.store.SearchIngredients(ctx, " "+quoted, limit)
		if err != nil {
			return nil, fmt.Errorf("search ingredients: %w", err)
		}
		for _, ing := range words {
			if len(out) == limit {
				break
			}
			if !slices.ContainsFunc(out, func(o Ingredient) bool { return o.ID == ing.ID }) {
				out = append(out, ing)
			}
		}
	}
	return out, nil
}
