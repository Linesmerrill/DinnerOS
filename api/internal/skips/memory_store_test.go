package skips

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// memoryStore is an in-memory Store for unit tests. It follows the same
// contract as MongoStore (runStoreContract).
type memoryStore struct {
	mu    sync.Mutex
	next  int
	skips []Skip
}

var _ Store = (*memoryStore)(nil)

func newMemoryStore() *memoryStore { return &memoryStore{} }

// nextID returns an ObjectID-shaped hex string, so the same fixtures work
// against MongoStore.
func (m *memoryStore) nextID() string {
	m.next++
	return fmt.Sprintf("66e5a1f2c3b4a5d6e7f8%04d", m.next)
}

func (m *memoryStore) ListSkips(_ context.Context, householdID string) ([]Skip, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Skip
	for _, s := range m.skips {
		if s.HouseholdID == householdID {
			out = append(out, s)
		}
	}
	// Newest first, as MongoStore sorts.
	slices.Reverse(out)
	return out, nil
}

func (m *memoryStore) CountSkips(_ context.Context, householdID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.skips {
		if s.HouseholdID == householdID {
			n++
		}
	}
	return n, nil
}

func (m *memoryStore) PutSkip(_ context.Context, skip Skip) (Skip, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.skips {
		if s.HouseholdID != skip.HouseholdID || s.IngredientKey != skip.IngredientKey {
			continue
		}
		// Replacing keeps the identity of the skip that is already there.
		m.skips[i].Key, m.skips[i].Name = skip.Key, skip.Name
		m.skips[i].Scope, m.skips[i].Week = skip.Scope, skip.Week
		m.skips[i].UpdatedBy, m.skips[i].UpdatedAt = skip.UpdatedBy, skip.UpdatedAt
		return m.skips[i], false, nil
	}
	skip.ID = m.nextID()
	m.skips = append(m.skips, skip)
	return skip, true, nil
}

func (m *memoryStore) DeleteSkip(_ context.Context, householdID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.skips {
		if s.HouseholdID == householdID && s.ID == id {
			m.skips = append(m.skips[:i], m.skips[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
