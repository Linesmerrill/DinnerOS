package households

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// memoryStore is an in-memory Store for unit tests. A single mutex makes each
// method atomic, mirroring the conditional updates of MongoStore.
type memoryStore struct {
	mu          sync.Mutex
	next        int
	households  map[string]Household
	adminCounts map[string]int
	memberships []Membership

	// failCreateMembership, when set, makes CreateMembership fail.
	failCreateMembership error
}

var _ Store = (*memoryStore)(nil)

func newMemoryStore() *memoryStore {
	return &memoryStore{households: map[string]Household{}, adminCounts: map[string]int{}}
}

func (m *memoryStore) id() string {
	m.next++
	return fmt.Sprintf("%024x", m.next)
}

func (m *memoryStore) CreateHousehold(_ context.Context, h Household) (Household, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h.ID = m.id()
	m.households[h.ID] = h
	m.adminCounts[h.ID] = 1
	return h, nil
}

func (m *memoryStore) DeleteHousehold(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.households, id)
	delete(m.adminCounts, id)
	return nil
}

func (m *memoryStore) GetHousehold(_ context.Context, id string) (Household, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.households[id]
	if !ok {
		return Household{}, ErrNotFound
	}
	return h, nil
}

func (m *memoryStore) ListHouseholds(_ context.Context, ids []string) ([]Household, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Household
	for _, id := range ids {
		if h, ok := m.households[id]; ok {
			out = append(out, h)
		}
	}
	return out, nil
}

func (m *memoryStore) UpdateHousehold(_ context.Context, id string, patch HouseholdPatch, at time.Time) (Household, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.households[id]
	if !ok {
		return Household{}, ErrNotFound
	}
	if patch.Name != nil {
		h.Name = *patch.Name
	}
	if patch.TimeZone != nil {
		h.TimeZone = *patch.TimeZone
	}
	if patch.DefaultServings != nil {
		h.DefaultServings = *patch.DefaultServings
	}
	if patch.OrderDay != nil {
		h.OrderDay = *patch.OrderDay
	}
	if patch.SetMealKit {
		h.MealKit = nil
		if patch.MealKit != nil {
			kit := *patch.MealKit
			h.MealKit = &kit
		}
	}
	h.UpdatedAt = at
	m.households[id] = h
	return h, nil
}

func (m *memoryStore) DecrementAdminCount(_ context.Context, householdID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.adminCounts[householdID]
	if !ok {
		return ErrNotFound
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	m.adminCounts[householdID] = n - 1
	return nil
}

func (m *memoryStore) IncrementAdminCount(_ context.Context, householdID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.adminCounts[householdID]; !ok {
		return ErrNotFound
	}
	m.adminCounts[householdID]++
	return nil
}

func (m *memoryStore) adminCount(householdID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.adminCounts[householdID]
}

func (m *memoryStore) CreateMembership(_ context.Context, ms Membership) (Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCreateMembership != nil {
		return Membership{}, m.failCreateMembership
	}
	for _, existing := range m.memberships {
		if existing.HouseholdID == ms.HouseholdID && existing.UserID == ms.UserID {
			return Membership{}, fmt.Errorf("%w: householdId_userId_unique", ErrDuplicate)
		}
	}
	ms.ID = m.id()
	m.memberships = append(m.memberships, ms)
	return ms, nil
}

func (m *memoryStore) GetMembership(_ context.Context, householdID, userID string) (Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.indexLocked(householdID, userID); i >= 0 {
		return m.memberships[i], nil
	}
	return Membership{}, ErrNotFound
}

func (m *memoryStore) indexLocked(householdID, userID string) int {
	return slices.IndexFunc(m.memberships, func(ms Membership) bool {
		return ms.HouseholdID == householdID && ms.UserID == userID
	})
}

func (m *memoryStore) ListMembershipsByHousehold(_ context.Context, householdID string) ([]Membership, error) {
	return m.filter(func(ms Membership) bool { return ms.HouseholdID == householdID }), nil
}

func (m *memoryStore) ListMembershipsByUser(_ context.Context, userID string) ([]Membership, error) {
	return m.filter(func(ms Membership) bool { return ms.UserID == userID }), nil
}

func (m *memoryStore) filter(keep func(Membership) bool) []Membership {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Membership{}
	for _, ms := range m.memberships {
		if keep(ms) {
			out = append(out, ms)
		}
	}
	// Insertion order is creation order, matching MongoStore's sort.
	return out
}

func (m *memoryStore) UpdateMembershipRole(_ context.Context, householdID, userID string, from, to Role, at time.Time) (Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.indexLocked(householdID, userID)
	if i < 0 || m.memberships[i].Role != from {
		return Membership{}, ErrNotFound
	}
	m.memberships[i].Role = to
	m.memberships[i].UpdatedAt = at
	return m.memberships[i], nil
}

func (m *memoryStore) DeleteMembership(_ context.Context, householdID, userID string, role Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.indexLocked(householdID, userID)
	if i < 0 || m.memberships[i].Role != role {
		return ErrNotFound
	}
	m.memberships = slices.Delete(m.memberships, i, i+1)
	return nil
}
