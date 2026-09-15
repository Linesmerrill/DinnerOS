package invitations

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// memoryStore is an in-memory Store for unit tests. A single mutex makes each
// method atomic, mirroring MongoStore's conditional updates.
type memoryStore struct {
	mu   sync.Mutex
	next int
	invs []Invitation

	// beforeMarkAccepted, when set, runs once before MarkAccepted so tests can
	// simulate a concurrent acceptance.
	beforeMarkAccepted func()
}

var _ Store = (*memoryStore)(nil)

func (m *memoryStore) Create(_ context.Context, inv Invitation) (Invitation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.invs {
		switch {
		case existing.TokenHash == inv.TokenHash:
			return Invitation{}, fmt.Errorf("%w: tokenHash_unique", ErrDuplicate)
		case existing.CodeHash == inv.CodeHash:
			return Invitation{}, fmt.Errorf("%w: codeHash_unique", ErrDuplicate)
		case existing.HouseholdID == inv.HouseholdID && existing.Email == inv.Email && openLocked(existing):
			return Invitation{}, fmt.Errorf("%w: householdId_email_pending_unique", ErrDuplicate)
		}
	}
	m.next++
	inv.ID = fmt.Sprintf("%024x", m.next)
	m.invs = append(m.invs, inv)
	return inv, nil
}

// openLocked mirrors the Mongo "pending" flag: not accepted and not revoked.
func openLocked(inv Invitation) bool { return inv.AcceptedAt.IsZero() && inv.RevokedAt.IsZero() }

func (m *memoryStore) find(match func(Invitation) bool) (Invitation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.IndexFunc(m.invs, match); i >= 0 {
		return m.invs[i], nil
	}
	return Invitation{}, ErrNotFound
}

func (m *memoryStore) Get(_ context.Context, householdID, id string) (Invitation, error) {
	return m.find(func(inv Invitation) bool { return inv.ID == id && inv.HouseholdID == householdID })
}

func (m *memoryStore) FindByTokenHash(_ context.Context, hash string) (Invitation, error) {
	return m.find(func(inv Invitation) bool { return inv.TokenHash == hash })
}

func (m *memoryStore) FindByCodeHash(_ context.Context, hash string) (Invitation, error) {
	return m.find(func(inv Invitation) bool { return inv.CodeHash == hash })
}

func (m *memoryStore) ListPending(_ context.Context, householdID string, now time.Time) ([]Invitation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Invitation{}
	for i := len(m.invs) - 1; i >= 0; i-- {
		inv := m.invs[i]
		if inv.HouseholdID == householdID && inv.Pending(now) {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (m *memoryStore) RevokePending(_ context.Context, householdID, email string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, inv := range m.invs {
		if inv.HouseholdID == householdID && inv.Email == email && openLocked(inv) {
			m.invs[i].RevokedAt = at
		}
	}
	return nil
}

func (m *memoryStore) Revoke(_ context.Context, householdID, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, inv := range m.invs {
		if inv.ID == id && inv.HouseholdID == householdID && openLocked(inv) {
			m.invs[i].RevokedAt = at
		}
	}
	return nil
}

func (m *memoryStore) MarkAccepted(_ context.Context, id, userID string, at time.Time) error {
	if hook := m.takeHook(); hook != nil {
		hook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, inv := range m.invs {
		if inv.ID == id && openLocked(inv) && at.Before(inv.ExpiresAt) {
			m.invs[i].AcceptedAt = at
			m.invs[i].AcceptedBy = userID
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) takeHook() func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	hook := m.beforeMarkAccepted
	m.beforeMarkAccepted = nil
	return hook
}

func (m *memoryStore) UnmarkAccepted(_ context.Context, id, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, inv := range m.invs {
		if inv.ID == id && inv.AcceptedBy == userID && inv.RevokedAt.IsZero() {
			m.invs[i].AcceptedAt = time.Time{}
			m.invs[i].AcceptedBy = ""
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) snapshot() []Invitation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.invs)
}

// fakeHouseholds is an in-memory stand-in for *households.Service.
type fakeHouseholds struct {
	mu          sync.Mutex
	households  map[string]households.Household
	memberships map[string]households.Membership // key householdID/userID
	failAdd     error
}

var (
	_ Households            = (*fakeHouseholds)(nil)
	_ households.Authorizer = (*fakeHouseholds)(nil)
)

func newFakeHouseholds() *fakeHouseholds {
	return &fakeHouseholds{households: map[string]households.Household{}, memberships: map[string]households.Membership{}}
}

func (f *fakeHouseholds) addHousehold(id, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.households[id] = households.Household{ID: id, Name: name, DefaultServings: 2, TimeZone: "UTC", CreatedAt: testNow, UpdatedAt: testNow}
}

func (f *fakeHouseholds) setMember(householdID, userID string, role households.Role) households.Membership {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := households.Membership{ID: "m-" + userID, HouseholdID: householdID, UserID: userID, Role: role, CreatedAt: testNow, UpdatedAt: testNow}
	f.memberships[householdID+"/"+userID] = m
	return m
}

func (f *fakeHouseholds) removeMember(householdID, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.memberships, householdID+"/"+userID)
}

func (f *fakeHouseholds) memberCount(householdID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.memberships {
		if m.HouseholdID == householdID {
			n++
		}
	}
	return n
}

func (f *fakeHouseholds) Authorize(ctx context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	m, err := f.GetMembership(ctx, householdID, userID)
	if err != nil {
		return households.Membership{}, err
	}
	if !m.Role.Can(perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

func (f *fakeHouseholds) GetHousehold(_ context.Context, id string) (households.Household, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.households[id]
	if !ok {
		return households.Household{}, households.ErrNotFound
	}
	return h, nil
}

func (f *fakeHouseholds) GetMembership(_ context.Context, householdID, userID string) (households.Membership, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.memberships[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	return m, nil
}

func (f *fakeHouseholds) AddMember(_ context.Context, householdID, userID string, role households.Role) (households.Membership, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAdd != nil {
		return households.Membership{}, false, f.failAdd
	}
	if _, ok := f.households[householdID]; !ok {
		return households.Membership{}, false, households.ErrNotFound
	}
	key := householdID + "/" + userID
	if m, ok := f.memberships[key]; ok {
		return m, false, nil
	}
	m := households.Membership{ID: "m-" + userID, HouseholdID: householdID, UserID: userID, Role: role, CreatedAt: testNow, UpdatedAt: testNow}
	f.memberships[key] = m
	return m, true, nil
}

// fakeEmail records sent invitations.
type fakeEmail struct {
	mu   sync.Mutex
	sent []HouseholdInvitationEmail
	err  error
}

func (f *fakeEmail) SendHouseholdInvitation(_ context.Context, e HouseholdInvitationEmail) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, e)
	return nil
}

func (f *fakeEmail) last() HouseholdInvitationEmail {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return HouseholdInvitationEmail{}
	}
	return f.sent[len(f.sent)-1]
}

type fakeUsers map[string]string // id → display name

func (f fakeUsers) GetUser(_ context.Context, id string) (users.User, error) {
	name, ok := f[id]
	if !ok {
		return users.User{}, users.ErrNotFound
	}
	return users.User{ID: id, DisplayName: name}, nil
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
