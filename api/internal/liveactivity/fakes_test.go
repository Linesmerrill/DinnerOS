package liveactivity

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
)

const (
	hhAda   = "66e5a1f2c3b4a5d6e7f80a01"
	hhBob   = "66e5a1f2c3b4a5d6e7f80a02"
	userAda = "66e5a1f2c3b4a5d6e7f80a21"
	userBob = "66e5a1f2c3b4a5d6e7f80a22"
	jobAda  = "66e5a1f2c3b4a5d6e7f80b01"
	token1  = "80a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f607"
	token2  = "90ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100ffeeddccbbaa9988"
)

// memoryStore is a Store with the same rules as MongoStore: a token attaches
// only to an active job of the right household.
type memoryStore struct {
	mu     sync.Mutex
	active map[string]string // jobID -> householdID, for jobs still in flight
	regs   map[string]Registration
}

func newMemoryStore() *memoryStore {
	return &memoryStore{active: map[string]string{jobAda: hhAda}, regs: map[string]Registration{}}
}

func (m *memoryStore) Register(_ context.Context, householdID, source, jobID, token string, env push.Environment, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[jobID] != householdID || source != "hellofresh" {
		return ErrNotFound
	}
	r := m.regs[jobID]
	r.JobID, r.HouseholdID, r.Token, r.Environment = jobID, householdID, token, env
	m.regs[jobID] = r
	return nil
}

func (m *memoryStore) Unregister(_ context.Context, householdID, _, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.regs[jobID]; ok && r.HouseholdID == householdID {
		delete(m.regs, jobID)
	}
	return nil
}

func (m *memoryStore) Get(_ context.Context, jobID string) (Registration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.regs[jobID]
	if !ok {
		return Registration{}, ErrNotFound
	}
	return r, nil
}

func (m *memoryStore) MarkSent(_ context.Context, jobID string, state ContentState, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.regs[jobID]; ok {
		r.LastSent, r.LastSentAt = state, at
		m.regs[jobID] = r
	}
	return nil
}

func (m *memoryStore) Clear(_ context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.regs, jobID)
	return nil
}

// fakeSender records Live Activity pushes and can fail them.
type fakeSender struct {
	mu   sync.Mutex
	sent []push.LiveActivityMessage
	err  error
}

func (f *fakeSender) SendLiveActivity(_ context.Context, m push.LiveActivityMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return f.err
}

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer grants permissions per (household, user).
type fakeAuthorizer map[string][]households.Permission

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	perms, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	m := households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
	if !slices.Contains(perms, perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

var testAuthorizer = fakeAuthorizer{
	hhAda + "/" + userAda: {households.PermHouseholdView, households.PermRecipesImport},
	hhBob + "/" + userBob: {households.PermHouseholdView},
}
