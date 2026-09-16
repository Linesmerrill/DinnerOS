package push

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
)

// Synthetic IDs, valid ObjectID hex so fixtures work against MongoDB.
const (
	hhA     = "66e5a1f2c3b4a5d6e7f80a01"
	hhB     = "66e5a1f2c3b4a5d6e7f80b01"
	userAda = "66e5a1f2c3b4a5d6e7f80a21"
	userBob = "66e5a1f2c3b4a5d6e7f80a22"
	userCy  = "66e5a1f2c3b4a5d6e7f80a23"
)

// fakeSender records pushes and answers with errs[token] (nil: accepted).
type fakeSender struct {
	mu   sync.Mutex
	sent []Message
	errs map[string]error
}

func (f *fakeSender) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return f.errs[m.Token]
}

func (f *fakeSender) tokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.sent {
		out = append(out, m.Token)
	}
	return out
}

// memoryTokens is an in-memory Store following the MongoStore contract.
type memoryTokens struct {
	mu     sync.Mutex
	tokens []DeviceToken
}

func (m *memoryTokens) Upsert(_ context.Context, t DeviceToken) (DeviceToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.tokens {
		if existing.Token == t.Token {
			t.CreatedAt = existing.CreatedAt
			m.tokens[i] = t
			return t, nil
		}
	}
	m.tokens = append(m.tokens, t)
	return t, nil
}

func (m *memoryTokens) Delete(_ context.Context, userID, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens = slices.DeleteFunc(m.tokens, func(t DeviceToken) bool { return t.Token == token && t.UserID == userID })
	return nil
}

func (m *memoryTokens) DeleteToken(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens = slices.DeleteFunc(m.tokens, func(t DeviceToken) bool { return t.Token == token })
	return nil
}

func (m *memoryTokens) ListByUsers(_ context.Context, userIDs []string) ([]DeviceToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []DeviceToken
	for _, t := range m.tokens {
		if slices.Contains(userIDs, t.UserID) {
			out = append(out, t)
		}
	}
	return out, nil
}

// fakeHouseholds serves fixed households and memberships.
type fakeHouseholds struct {
	households map[string]households.Household
	members    map[string][]string
}

func (f fakeHouseholds) ListHouseholdIDs(_ context.Context, after string, limit int) ([]string, error) {
	var ids []string
	for id := range f.households {
		if id > after {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (f fakeHouseholds) GetHousehold(_ context.Context, id string) (households.Household, error) {
	h, ok := f.households[id]
	if !ok {
		return households.Household{}, households.ErrNotFound
	}
	return h, nil
}

func (f fakeHouseholds) ListMembershipsByHousehold(_ context.Context, id string) ([]households.Membership, error) {
	var out []households.Membership
	for _, userID := range f.members[id] {
		out = append(out, households.Membership{HouseholdID: id, UserID: userID, Role: households.RoleMember})
	}
	return out, nil
}

// memoryOutbox is an in-memory notifications.Outbox. Its Refresh creates
// the notifications queued in produce, standing in for the producers.
type memoryOutbox struct {
	mu        sync.Mutex
	items     []notifications.Notification
	refreshed []string
	produce   map[string][]notifications.Notification
}

func (m *memoryOutbox) Refresh(_ context.Context, householdID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshed = append(m.refreshed, householdID)
	for _, n := range m.produce[householdID] {
		if !slices.ContainsFunc(m.items, func(x notifications.Notification) bool { return x.ID == n.ID }) {
			n.Push.Status = notifications.PushPending
			m.items = append(m.items, n)
		}
	}
}

func (m *memoryOutbox) PendingPush(_ context.Context, limit int) ([]notifications.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []notifications.Notification
	for _, n := range m.items {
		if n.Push.Status == notifications.PushPending {
			out = append(out, n)
		}
	}
	slices.SortStableFunc(out, func(a, b notifications.Notification) int { return a.CreatedAt.Compare(b.CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryOutbox) ClaimPush(_ context.Context, id string, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, n := range m.items {
		if n.ID == id && n.Push.Status == notifications.PushPending {
			m.items[i].Push.Status = notifications.PushSending
			m.items[i].Push.Attempts++
			m.items[i].Push.LastAttemptAt = at
			return true, nil
		}
	}
	return false, nil
}

func (m *memoryOutbox) FinishPush(_ context.Context, id string, status notifications.PushStatus, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, n := range m.items {
		if n.ID == id {
			m.items[i].Push.Status = status
			if status == notifications.PushSent {
				m.items[i].Push.SentAt = at
			}
			return nil
		}
	}
	return notifications.ErrNotFound
}

func (m *memoryOutbox) status(id string) notifications.PushStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.items {
		if n.ID == id {
			return n.Push.Status
		}
	}
	return ""
}
