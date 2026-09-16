package pantry

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// memoryUsageStore is an in-memory UsageStore following the MongoStore
// contract (runUsageStoreContract).
type memoryUsageStore struct {
	mu        sync.Mutex
	next      int
	purchases []Purchase
	cooks     []CookUsage
	settings  map[string]Settings
	// settingsErr, when set, fails GetSettings.
	settingsErr error
}

func newMemoryUsageStore() *memoryUsageStore {
	return &memoryUsageStore{settings: map[string]Settings{}}
}

func (m *memoryUsageStore) InsertPurchase(_ context.Context, p Purchase) (Purchase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ClientPurchaseID != "" && slices.ContainsFunc(m.purchases, func(x Purchase) bool {
		return x.HouseholdID == p.HouseholdID && x.RecordedBy == p.RecordedBy && x.ClientPurchaseID == p.ClientPurchaseID
	}) {
		return Purchase{}, fmt.Errorf("%w: householdId_recordedBy_clientPurchaseId_unique", ErrDuplicate)
	}
	if pr := p.Provider; pr != nil && slices.ContainsFunc(m.purchases, func(x Purchase) bool {
		return x.HouseholdID == p.HouseholdID && x.Provider != nil && x.Provider.HandoffID == pr.HandoffID && x.Provider.LineID == pr.LineID
	}) {
		return Purchase{}, fmt.Errorf("%w: householdId_provider_handoffId_lineId_unique", ErrDuplicate)
	}
	m.purchases = append(m.purchases, p)
	return p, nil
}

func (m *memoryUsageStore) FindPurchaseByProviderLine(_ context.Context, householdID, handoffID, lineID string) (Purchase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.purchases {
		if p.HouseholdID == householdID && p.Provider != nil && p.Provider.HandoffID == handoffID && p.Provider.LineID == lineID {
			return p, nil
		}
	}
	return Purchase{}, ErrNotFound
}

func (m *memoryUsageStore) FindPurchaseByClientID(_ context.Context, householdID, userID, clientPurchaseID string) (Purchase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.purchases {
		if p.HouseholdID == householdID && p.RecordedBy == userID && clientPurchaseID != "" && p.ClientPurchaseID == clientPurchaseID {
			return p, nil
		}
	}
	return Purchase{}, ErrNotFound
}

func (m *memoryUsageStore) ListPurchases(_ context.Context, householdID, itemID string, limit int) ([]Purchase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Purchase
	for i := len(m.purchases) - 1; i >= 0; i-- {
		if p := m.purchases[i]; p.HouseholdID == householdID && p.ItemID == itemID {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b Purchase) int { return b.PurchasedAt.Compare(a.PurchasedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryUsageStore) InsertCookUsage(_ context.Context, u CookUsage) (CookUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if slices.ContainsFunc(m.cooks, func(x CookUsage) bool { return x.HouseholdID == u.HouseholdID && x.SourceKey == u.SourceKey }) {
		return CookUsage{}, fmt.Errorf("%w: householdId_sourceKey_unique", ErrDuplicate)
	}
	m.next++
	u.ID = fmt.Sprintf("%024x", m.next)
	u.Lines = slices.Clone(u.Lines)
	m.cooks = append(m.cooks, u)
	return u, nil
}

func (m *memoryUsageStore) GetSettings(_ context.Context, householdID string) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settingsErr != nil {
		return Settings{}, m.settingsErr
	}
	s, ok := m.settings[householdID]
	if !ok {
		return Settings{}, ErrNotFound
	}
	return s, nil
}

func (m *memoryUsageStore) PutSettings(_ context.Context, s Settings) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings[s.HouseholdID] = s
	return s, nil
}

func (m *memoryUsageStore) SetPurchasePrice(_ context.Context, householdID, purchaseID string, priceCents *int64) (Purchase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.purchases {
		if p.HouseholdID == householdID && p.ID == purchaseID {
			if priceCents != nil {
				v := *priceCents
				priceCents = &v
			}
			m.purchases[i].PriceCents = priceCents
			return m.purchases[i], nil
		}
	}
	return Purchase{}, ErrNotFound
}

func (m *memoryUsageStore) PurchasesByIDs(_ context.Context, householdID string, ids []string) ([]Purchase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Purchase
	for _, p := range m.purchases {
		if p.HouseholdID == householdID && slices.Contains(ids, p.ID) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memoryUsageStore) ListCookUsageByEntries(_ context.Context, householdID string, entryIDs []string) ([]CookUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []CookUsage
	for _, u := range m.cooks {
		if u.HouseholdID == householdID && slices.Contains(entryIDs, strings.TrimPrefix(u.SourceKey, "entry:")) && strings.HasPrefix(u.SourceKey, "entry:") {
			u.Lines = slices.Clone(u.Lines)
			out = append(out, u)
		}
	}
	slices.SortStableFunc(out, func(a, b CookUsage) int { return a.OccurredAt.Compare(b.OccurredAt) })
	return out, nil
}

// cookUsages returns the stored cook usage records, oldest first.
func (m *memoryUsageStore) cookUsages() []CookUsage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.cooks)
}
