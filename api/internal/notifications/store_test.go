package notifications

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

// Synthetic IDs, valid ObjectID hex so fixtures work against MongoDB.
const (
	hhA   = "66e5a1f2c3b4a5d6e7f80a01"
	hhB   = "66e5a1f2c3b4a5d6e7f80b01"
	userA = "66e5a1f2c3b4a5d6e7f80a21"
	userB = "66e5a1f2c3b4a5d6e7f80a22"
)

var testNow = time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)

// memoryStore is an in-memory Store following the MongoStore contract.
type memoryStore struct {
	mu    sync.Mutex
	next  int
	items []Notification
	err   error
}

func (m *memoryStore) Insert(_ context.Context, n Notification) (Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return Notification{}, m.err
	}
	if slices.ContainsFunc(m.items, func(x Notification) bool { return x.HouseholdID == n.HouseholdID && x.DedupeKey == n.DedupeKey }) {
		return Notification{}, fmt.Errorf("%w: householdId_dedupeKey_unique", ErrDuplicate)
	}
	m.next++
	n.ID = fmt.Sprintf("%024x", m.next)
	n.ReadBy = slices.Clone(n.ReadBy)
	m.items = append(m.items, n)
	return n, nil
}

func (m *memoryStore) FindByDedupeKey(_ context.Context, householdID, key string) (Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.items {
		if n.HouseholdID == householdID && n.DedupeKey == key {
			n.ReadBy = slices.Clone(n.ReadBy)
			return n, nil
		}
	}
	return Notification{}, ErrNotFound
}

func (m *memoryStore) List(_ context.Context, householdID string, f ListFilter) ([]Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	var out []Notification
	for i := len(m.items) - 1; i >= 0; i-- {
		n := m.items[i]
		switch {
		case n.HouseholdID != householdID,
			f.UnreadBy != "" && n.ReadByUser(f.UnreadBy),
			f.Before != "" && n.ID >= f.Before:
			continue
		}
		n.ReadBy = slices.Clone(n.ReadBy)
		out = append(out, n)
		if f.Limit > 0 && len(out) == f.Limit {
			break
		}
	}
	return out, nil
}

func (m *memoryStore) CountUnread(_ context.Context, householdID, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, n := range m.items {
		if n.HouseholdID == householdID && !n.ReadByUser(userID) {
			count++
		}
	}
	return count, nil
}

func (m *memoryStore) MarkRead(_ context.Context, householdID, userID string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, n := range m.items {
		if n.HouseholdID != householdID || (ids != nil && !slices.Contains(ids, n.ID)) || n.ReadByUser(userID) {
			continue
		}
		m.items[i].ReadBy = append(slices.Clone(n.ReadBy), userID)
	}
	return nil
}

func TestMemoryStoreContract(t *testing.T) {
	runStoreContract(t, &memoryStore{})
}

func TestIntegrationMongoStoreContract(t *testing.T) {
	client := mongotest.Client(t)
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	runStoreContract(t, NewMongoStore(client.Database()))
}

func ids(list []Notification) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		out = append(out, n.ID)
	}
	return out
}

func runStoreContract(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	insert := func(householdID, key string, at time.Time) Notification {
		t.Helper()
		n, err := store.Insert(ctx, Notification{
			HouseholdID: householdID, Type: TypePantryLow, Title: "Butter is running low", Body: "About 20% left.",
			Subject: Subject{Kind: SubjectPantryItem, ID: "66e5a1f2c3b4a5d6e7f80d01"}, DedupeKey: key,
			Push: Push{Status: PushPending}, CreatedAt: at,
		})
		if err != nil || n.ID == "" {
			t.Fatalf("Insert(%s) = %+v, %v", key, n, err)
		}
		return n
	}
	first := insert(hhA, "k1", testNow)
	if first.Title != "Butter is running low" || first.Subject.Kind != SubjectPantryItem || first.Push.Status != PushPending || !first.CreatedAt.Equal(testNow) || len(first.ReadBy) != 0 {
		t.Errorf("inserted = %+v", first)
	}
	second := insert(hhA, "k2", testNow.Add(time.Minute))
	third := insert(hhA, "k3", testNow.Add(2*time.Minute))
	other := insert(hhB, "k1", testNow) // keys are per household

	if _, err := store.Insert(ctx, Notification{HouseholdID: hhA, Type: TypePantryLow, Title: "x", DedupeKey: "k1", Push: Push{Status: PushPending}, CreatedAt: testNow}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate Insert() error = %v, want ErrDuplicate", err)
	}
	if got, err := store.FindByDedupeKey(ctx, hhA, "k2"); err != nil || got.ID != second.ID {
		t.Errorf("FindByDedupeKey() = %+v, %v", got, err)
	}
	if _, err := store.FindByDedupeKey(ctx, hhB, "k2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindByDedupeKey(other household) error = %v", err)
	}

	list, err := store.List(ctx, hhA, ListFilter{Limit: 10})
	if err != nil || strings.Join(ids(list), ",") != strings.Join([]string{third.ID, second.ID, first.ID}, ",") {
		t.Fatalf("List() = %v, %v", ids(list), err)
	}
	if list, _ := store.List(ctx, hhA, ListFilter{Limit: 2}); len(list) != 2 {
		t.Errorf("List(limit 2) = %v", ids(list))
	}
	if list, _ := store.List(ctx, hhA, ListFilter{Before: second.ID, Limit: 10}); len(list) != 1 || list[0].ID != first.ID {
		t.Errorf("List(before second) = %v", ids(list))
	}

	if err := store.MarkRead(ctx, hhA, userA, []string{second.ID, other.ID, "not-an-id"}); err != nil {
		t.Fatal(err)
	}
	if n, err := store.CountUnread(ctx, hhA, userA); err != nil || n != 2 {
		t.Errorf("CountUnread(userA) = %d, %v, want 2", n, err)
	}
	if n, _ := store.CountUnread(ctx, hhB, userA); n != 1 {
		t.Errorf("CountUnread(hhB) = %d, want 1: marking must stay in the household", n)
	}
	if n, _ := store.CountUnread(ctx, hhA, userB); n != 3 {
		t.Errorf("CountUnread(userB) = %d, want 3: reads are per member", n)
	}
	unread, _ := store.List(ctx, hhA, ListFilter{UnreadBy: userA, Limit: 10})
	if strings.Join(ids(unread), ",") != third.ID+","+first.ID {
		t.Errorf("List(unread) = %v", ids(unread))
	}
	if got, _ := store.FindByDedupeKey(ctx, hhA, "k2"); !got.ReadByUser(userA) || got.ReadByUser(userB) {
		t.Errorf("readBy = %v", got.ReadBy)
	}
	// Marking again doesn't duplicate the reader; nil marks everything.
	if err := store.MarkRead(ctx, hhA, userA, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRead(ctx, hhA, userA, nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.CountUnread(ctx, hhA, userA); n != 0 {
		t.Errorf("CountUnread after all = %d", n)
	}
	if got, _ := store.FindByDedupeKey(ctx, hhA, "k2"); len(got.ReadBy) != 1 {
		t.Errorf("readBy after repeated marks = %v", got.ReadBy)
	}
}
