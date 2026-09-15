package recipes

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// memoryStore is an in-memory Store for unit tests. Its list ordering mirrors
// MongoStore: case-insensitive names, then IDs.
type memoryStore struct {
	mu          sync.Mutex
	next        int
	ingredients []Ingredient
	recipes     []Recipe
	reviews     map[string]ReviewItem // householdID + "/" + reviewKey

	saveRecipesCalls int
	upsertCalls      int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{reviews: map[string]ReviewItem{}}
}

func (m *memoryStore) id() string {
	m.next++
	return fmt.Sprintf("%024x", m.next)
}

func (m *memoryStore) FindIngredients(_ context.Context, refs []SourceRef, keys []string) ([]Ingredient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Ingredient
	for _, ing := range m.ingredients {
		match := slices.Contains(keys, ing.Key)
		for _, ref := range ing.SourceRefs {
			match = match || slices.Contains(refs, ref)
		}
		if match {
			out = append(out, cloneIngredient(ing))
		}
	}
	return out, nil
}

func (m *memoryStore) GetIngredients(_ context.Context, ids []string) ([]Ingredient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Ingredient
	for _, ing := range m.ingredients {
		if slices.Contains(ids, ing.ID) {
			out = append(out, cloneIngredient(ing))
		}
	}
	return out, nil
}

func (m *memoryStore) UpsertIngredients(_ context.Context, list []Ingredient) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertCalls++
	inserted := 0
	for _, ing := range list {
		i := slices.IndexFunc(m.ingredients, func(e Ingredient) bool { return e.Key == ing.Key })
		if i < 0 {
			ing = cloneIngredient(ing)
			ing.ID = m.id()
			m.ingredients = append(m.ingredients, ing)
			inserted++
			continue
		}
		for _, ref := range ing.SourceRefs {
			if !slices.Contains(m.ingredients[i].SourceRefs, ref) {
				m.ingredients[i].SourceRefs = append(m.ingredients[i].SourceRefs, ref)
			}
		}
		m.ingredients[i].UpdatedAt = ing.UpdatedAt
	}
	return inserted, nil
}

func (m *memoryStore) SearchIngredients(_ context.Context, keyPattern string, limit int) ([]Ingredient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	re := regexp.MustCompile(keyPattern)
	var out []Ingredient
	for _, ing := range m.ingredients {
		if re.MatchString(ing.Key) {
			out = append(out, cloneIngredient(ing))
		}
	}
	slices.SortFunc(out, func(a, b Ingredient) int { return cmp.Compare(a.Key, b.Key) })
	if len(out) > limit {
		out = out[:max(limit, 0)]
	}
	return out, nil
}

func (m *memoryStore) FindRecipesBySourceIDs(_ context.Context, householdID, source string, ids []string) ([]Recipe, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Recipe
	for _, r := range m.recipes {
		if r.HouseholdID != householdID || r.Source != source {
			continue
		}
		match := slices.Contains(ids, r.SourceRecipeID)
		for _, alias := range r.SourceAliases {
			match = match || slices.Contains(ids, alias)
		}
		if match {
			out = append(out, cloneRecipe(r))
		}
	}
	return out, nil
}

func (m *memoryStore) SaveRecipes(_ context.Context, householdID string, list []Recipe) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveRecipesCalls++
	for _, r := range list {
		r = cloneRecipe(r)
		r.HouseholdID = householdID
		if r.ID == "" {
			for _, e := range m.recipes {
				if e.HouseholdID == householdID && e.Source == r.Source && e.SourceRecipeID == r.SourceRecipeID {
					return fmt.Errorf("%w: householdId_source_sourceRecipeId_unique", ErrDuplicate)
				}
			}
			r.ID = m.id()
			m.recipes = append(m.recipes, r)
			continue
		}
		for i := range m.recipes {
			if m.recipes[i].ID == r.ID && m.recipes[i].HouseholdID == householdID {
				m.recipes[i] = r
			}
		}
	}
	return nil
}

func (m *memoryStore) GetRecipe(_ context.Context, householdID, id string) (Recipe, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.recipes {
		if r.ID == id && r.HouseholdID == householdID {
			return cloneRecipe(r), nil
		}
	}
	return Recipe{}, ErrNotFound
}

func (m *memoryStore) GetRecipes(_ context.Context, householdID string, ids []string) ([]Recipe, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Recipe
	for _, r := range m.recipes {
		if r.HouseholdID == householdID && slices.Contains(ids, r.ID) {
			out = append(out, cloneRecipe(r))
		}
	}
	slices.SortFunc(out, func(a, b Recipe) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (m *memoryStore) ExistingRecipeIDs(_ context.Context, householdID string, ids []string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, r := range m.recipes {
		if r.HouseholdID == householdID && slices.Contains(ids, r.ID) {
			out = append(out, r.ID)
		}
	}
	return out, nil
}

func (m *memoryStore) ListRecipes(_ context.Context, householdID string, f ListFilter) ([]RecipeSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var search *regexp.Regexp
	if f.SearchPattern != "" {
		search = regexp.MustCompile("(?i)" + f.SearchPattern)
	}
	containsFold := func(values []string, want string) bool {
		return slices.ContainsFunc(values, func(v string) bool { return strings.EqualFold(v, want) })
	}
	var out []RecipeSummary
	for _, r := range m.recipes {
		switch {
		case r.HouseholdID != householdID,
			search != nil && !search.MatchString(r.Name),
			f.Addons != nil && r.IsAddon != *f.Addons,
			f.Tag != "" && !containsFold(r.Tags, f.Tag),
			f.Cuisine != "" && !containsFold(r.Cuisines, f.Cuisine):
			continue
		}
		s := RecipeSummary{
			ID: r.ID, Name: r.Name, Headline: r.Headline, ImageURL: r.ImageURL, TotalMinutes: r.TotalMinutes,
			TimesOrdered: r.TimesOrdered, LastOrderedWeek: r.LastOrderedWeek, IsAddon: r.IsAddon, Tags: slices.Clone(r.Tags),
		}
		if f.After != nil && compareListOrder(f.Sort, positionOf(s), *f.After) <= 0 {
			continue
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b RecipeSummary) int { return compareListOrder(f.Sort, positionOf(a), positionOf(b)) })
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func compareListOrder(s Sort, a, b Position) int {
	switch s {
	case SortRecent:
		if c := cmp.Compare(b.LastOrderedWeek, a.LastOrderedWeek); c != 0 {
			return c
		}
	case SortPopular:
		if c := cmp.Compare(b.TimesOrdered, a.TimesOrdered); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

func (m *memoryStore) SaveReviewItems(_ context.Context, householdID string, items []ReviewItem, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range items {
		key := householdID + "/" + reviewKey(it)
		if _, ok := m.reviews[key]; !ok {
			m.reviews[key] = it
		}
	}
	return nil
}

func cloneIngredient(ing Ingredient) Ingredient {
	ing.SourceRefs = nilIfEmpty(slices.Clone(ing.SourceRefs))
	return ing
}

func cloneRecipe(r Recipe) Recipe {
	r.SourceAliases = nilIfEmpty(slices.Clone(r.SourceAliases))
	r.Servings = nilIfEmpty(slices.Clone(r.Servings))
	r.Cuisines = nilIfEmpty(slices.Clone(r.Cuisines))
	r.Tags = nilIfEmpty(slices.Clone(r.Tags))
	r.Utensils = nilIfEmpty(slices.Clone(r.Utensils))
	r.Allergens = nilIfEmpty(slices.Clone(r.Allergens))
	r.Nutrition = nilIfEmpty(slices.Clone(r.Nutrition))
	r.Steps = nilIfEmpty(slices.Clone(r.Steps))
	r.OrderWeeks = nilIfEmpty(slices.Clone(r.OrderWeeks))
	lines := make([]RecipeIngredient, 0, len(r.Ingredients))
	for _, line := range r.Ingredients {
		line.Category = "" // not stored
		line.Amounts = nilIfEmpty(slices.Clone(line.Amounts))
		lines = append(lines, line)
	}
	r.Ingredients = nilIfEmpty(lines)
	return r
}
