package pantry

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Synthetic IDs. They are valid ObjectID hex so the same fixtures work
// against MongoDB.
const (
	testHousehold  = "66e5a1f2c3b4a5d6e7f80a01"
	otherHousehold = "66e5a1f2c3b4a5d6e7f80b01"
	testUser       = "66e5a1f2c3b4a5d6e7f80c01"
)

var testNow = time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC)

func member(householdID string) households.Membership {
	return households.Membership{HouseholdID: householdID, UserID: testUser, Role: households.RoleMember}
}

func ptr[T any](v T) *T { return &v }

// memoryStore is an in-memory Store for unit tests. It follows the same
// contract as MongoStore (runStoreContract).
type memoryStore struct {
	mu    sync.Mutex
	next  int
	items []Item
	// beforeUpdate, when set, runs at the start of every UpdateItem call,
	// outside the lock, to simulate a concurrent writer.
	beforeUpdate func()
}

func newMemoryStore() *memoryStore { return &memoryStore{} }

func (m *memoryStore) index(householdID, id string) int {
	return slices.IndexFunc(m.items, func(it Item) bool { return it.ID == id && it.HouseholdID == householdID })
}

func (m *memoryStore) ListItems(_ context.Context, householdID string, f ListFilter) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var nameRe, keyRe *regexp.Regexp
	if f.NamePattern != "" {
		nameRe = regexp.MustCompile("(?i)" + f.NamePattern)
	}
	if f.KeyPattern != "" {
		keyRe = regexp.MustCompile(f.KeyPattern)
	}
	var out []Item
	for _, it := range m.items {
		switch {
		case it.HouseholdID != householdID,
			f.Status != "" && it.Status != f.Status,
			f.Category != "" && it.Category != f.Category,
			f.Staple != nil && it.IsStaple != *f.Staple:
			continue
		}
		if nameRe != nil || keyRe != nil {
			matched := (nameRe != nil && nameRe.MatchString(it.DisplayName)) || (keyRe != nil && keyRe.MatchString(it.Key))
			if !matched {
				continue
			}
		}
		out = append(out, it)
	}
	return out, nil
}

func (m *memoryStore) GetItem(_ context.Context, householdID, id string) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.index(householdID, id); i >= 0 {
		return cloneItem(m.items[i]), nil
	}
	return Item{}, ErrNotFound
}

func (m *memoryStore) GetItems(_ context.Context, householdID string, ids []string) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Item
	for _, it := range m.items {
		if it.HouseholdID == householdID && slices.Contains(ids, it.ID) {
			out = append(out, it)
		}
	}
	return out, nil
}

func (m *memoryStore) FindItemsByKeys(_ context.Context, householdID string, keys []string) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Item
	for _, it := range m.items {
		if it.HouseholdID == householdID && slices.Contains(keys, it.Key) {
			out = append(out, it)
		}
	}
	return out, nil
}

func (m *memoryStore) CountItems(_ context.Context, householdID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, it := range m.items {
		if it.HouseholdID == householdID {
			n++
		}
	}
	return n, nil
}

func (m *memoryStore) InsertItem(_ context.Context, item Item) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if slices.ContainsFunc(m.items, func(it Item) bool { return it.HouseholdID == item.HouseholdID && it.Key == item.Key }) {
		return Item{}, fmt.Errorf("%w: householdId_key_unique", ErrDuplicate)
	}
	m.next++
	item.ID = fmt.Sprintf("%024x", m.next)
	item.Version = 1
	m.items = append(m.items, cloneItem(item))
	return item, nil
}

func (m *memoryStore) UpdateItem(_ context.Context, item Item) (Item, error) {
	if m.beforeUpdate != nil {
		m.beforeUpdate()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.index(item.HouseholdID, item.ID)
	switch {
	case i < 0:
		return Item{}, ErrNotFound
	case m.items[i].Version != item.Version:
		return Item{}, ErrConflict
	}
	item.Key, item.CreatedAt = m.items[i].Key, m.items[i].CreatedAt
	item.Version++
	m.items[i] = cloneItem(item)
	return item, nil
}

func (m *memoryStore) DeleteItem(_ context.Context, householdID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.index(householdID, id)
	if i < 0 {
		return ErrNotFound
	}
	m.items = slices.Delete(m.items, i, i+1)
	return nil
}

func (m *memoryStore) SetStatus(_ context.Context, householdID string, ids []string, status Status, updatedBy string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, it := range m.items {
		if it.HouseholdID != householdID || !slices.Contains(ids, it.ID) {
			continue
		}
		it.Status, it.StatusSource, it.StatusSetAt, it.UpdatedBy, it.UpdatedAt = status, StatusSourcePerson, at, updatedBy, at
		if status == StatusOut {
			it.Quantity, it.Unit = "", ""
		}
		it.Version++
		m.items[i] = it
	}
	return nil
}

// fakeCatalog is an in-memory Catalog.
type fakeCatalog struct {
	mu    sync.Mutex
	items []recipes.Ingredient
}

func newFakeCatalog(names ...string) *fakeCatalog {
	c := &fakeCatalog{}
	for _, name := range names {
		c.add(name)
	}
	return c
}

// add puts a synthetic ingredient in the catalog, categorized like an import.
func (c *fakeCatalog) add(name string) recipes.Ingredient {
	c.mu.Lock()
	defer c.mu.Unlock()
	category, confident := ingredients.Categorize(name)
	ing := recipes.Ingredient{
		ID:  fmt.Sprintf("cafe%020x", len(c.items)+1),
		Key: ingredients.NormalizeName(name), Name: name, Category: category, CategoryConfident: confident,
	}
	c.items = append(c.items, ing)
	return ing
}

// id returns the catalog ID for a name, or "" when the catalog lacks it.
func (c *fakeCatalog) id(name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := ingredients.NormalizeName(name)
	for _, ing := range c.items {
		if ing.Key == key {
			return ing.ID
		}
	}
	return ""
}

func (c *fakeCatalog) IngredientsByID(_ context.Context, ids []string) ([]recipes.Ingredient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []recipes.Ingredient
	for _, ing := range c.items {
		if slices.Contains(ids, ing.ID) {
			out = append(out, ing)
		}
	}
	return out, nil
}

func (c *fakeCatalog) IngredientsByKey(_ context.Context, keys []string) ([]recipes.Ingredient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []recipes.Ingredient
	for _, ing := range c.items {
		if slices.Contains(keys, ing.Key) {
			out = append(out, ing)
		}
	}
	return out, nil
}
