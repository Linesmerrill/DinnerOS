package events

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
)

// memoryStore is an in-memory Store for unit tests. Like MongoStore, it stores
// a (household, user, clientEventId) at most once.
type memoryStore struct {
	mu         sync.Mutex
	next       int
	events     []Event
	insertErr  error
	insertCall int
}

func (m *memoryStore) Insert(_ context.Context, list []Event) (inserted, duplicates int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insertCall++
	if m.insertErr != nil {
		return 0, 0, m.insertErr
	}
	for _, e := range list {
		if e.ClientEventID != "" && slices.ContainsFunc(m.events, func(s Event) bool {
			return s.HouseholdID == e.HouseholdID && s.UserID == e.UserID && s.ClientEventID == e.ClientEventID
		}) {
			duplicates++
			continue
		}
		m.next++
		e.ID = fmt.Sprintf("%024x", m.next)
		m.events = append(m.events, e)
		inserted++
	}
	return inserted, duplicates, nil
}

func (m *memoryStore) List(_ context.Context, q Query) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Event
	for _, e := range m.events {
		switch {
		case e.HouseholdID != q.HouseholdID,
			q.RecipeID != "" && e.RecipeID != q.RecipeID,
			len(q.Types) > 0 && !slices.Contains(q.Types, e.Type),
			!q.Since.IsZero() && e.OccurredAt.Before(q.Since):
			continue
		}
		out = append(out, e)
	}
	slices.SortStableFunc(out, func(a, b Event) int {
		if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryStore) all() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.events)
}
