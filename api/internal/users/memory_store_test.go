package users

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// memoryStore is an in-memory Store for unit tests.
type memoryStore struct {
	mu         sync.Mutex
	next       int
	users      map[string]User
	identities []AuthIdentity

	// beforeCreateIdentity, when set, runs before CreateIdentity so tests can
	// simulate a concurrent sign-up winning the race.
	beforeCreateIdentity func()
}

func newMemoryStore() *memoryStore {
	return &memoryStore{users: map[string]User{}}
}

func (m *memoryStore) id() string {
	m.next++
	return fmt.Sprintf("%024x", m.next)
}

func (m *memoryStore) CreateUser(_ context.Context, u User) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.ID = m.id()
	m.users[u.ID] = u
	return u, nil
}

func (m *memoryStore) DeleteUser(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, id)
	return nil
}

func (m *memoryStore) GetUser(_ context.Context, id string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *memoryStore) CreateIdentity(_ context.Context, identity AuthIdentity) (AuthIdentity, error) {
	if hook := m.beforeCreateIdentity; hook != nil {
		m.beforeCreateIdentity = nil
		hook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.identities {
		if existing.Provider == identity.Provider && existing.Subject == identity.Subject {
			return AuthIdentity{}, fmt.Errorf("%w: provider_subject_unique", ErrDuplicate)
		}
	}
	identity.ID = m.id()
	m.identities = append(m.identities, identity)
	return identity, nil
}

func (m *memoryStore) FindIdentity(_ context.Context, provider Provider, subject string) (AuthIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, identity := range m.identities {
		if identity.Provider == provider && identity.Subject == subject {
			return identity, nil
		}
	}
	return AuthIdentity{}, ErrNotFound
}

func (m *memoryStore) TouchIdentity(_ context.Context, id string, email string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.identities {
		if m.identities[i].ID == id {
			m.identities[i].LastUsedAt = at
			if email != "" {
				m.identities[i].Email = email
				m.identities[i].EmailVerified = true
			}
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) ListIdentities(_ context.Context, userID string) ([]AuthIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AuthIdentity
	for _, identity := range m.identities {
		if identity.UserID == userID {
			out = append(out, identity)
		}
	}
	return slices.Clip(out), nil
}
