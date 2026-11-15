package plus

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// memoryReferrals is an in-memory ReferralStore following the Mongo
// contract. A referral with no State is one from before decision 698:
// qualified and settled.
type memoryReferrals struct {
	mu    sync.Mutex
	codes map[string]string // household -> code
	// retired are the keys of replaced codes; changes are each household's
	// code change times.
	retired map[string]bool
	changes map[string][]time.Time
	refs    []Referral
	flags   map[string]map[string]bool // referred -> flag -> set
	next    int
}

func (s *memoryReferrals) Code(_ context.Context, id string, newCode func() string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.codes[id]; ok {
		return c, nil
	}
	for range 5 {
		if c := newCode(); !s.takenLocked(CodeKey(c)) {
			s.codes[id] = c
			return c, nil
		}
	}
	return "", fmt.Errorf("no code")
}

func (s *memoryReferrals) Owner(_ context.Context, code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := CodeKey(code)
	for id, c := range s.codes {
		if CodeKey(c) == key {
			return id, nil
		}
	}
	if s.retired[key] {
		return "", ErrCodeRetired
	}
	return "", ErrCodeNotFound
}

func (s *memoryReferrals) takenLocked(key string) bool {
	if s.retired[key] {
		return true
	}
	for _, c := range s.codes {
		if CodeKey(c) == key {
			return true
		}
	}
	return false
}

func (s *memoryReferrals) Taken(_ context.Context, code string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.takenLocked(CodeKey(code)), nil
}

func (s *memoryReferrals) CodeChanges(_ context.Context, id string) ([]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.changes[id]), nil
}

func (s *memoryReferrals) Replace(_ context.Context, id, old, code string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.takenLocked(CodeKey(code)) {
		return ErrCodeTaken
	}
	if s.codes[id] != old {
		return ErrCodeChangedNow
	}
	if s.retired == nil {
		s.retired, s.changes = map[string]bool{}, map[string][]time.Time{}
	}
	s.retired[CodeKey(old)] = true
	s.codes[id] = code
	s.changes[id] = append(s.changes[id], at)
	return nil
}

func norm(r Referral) Referral {
	if r.State == "" {
		r.State, r.Settled = StateQualified, true
	}
	return r
}

func (s *memoryReferrals) Record(_ context.Context, r *Referral) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.refs {
		if old.ReferredID == r.ReferredID {
			return ErrAlreadyRedeemed
		}
	}
	s.next++
	r.ID = fmt.Sprintf("%024x", s.next)
	if r.State == "" {
		r.State = StatePending
	}
	s.refs = append(s.refs, *r)
	return nil
}

func (s *memoryReferrals) find(referred string) int {
	for i, r := range s.refs {
		if r.ReferredID == referred {
			return i
		}
	}
	return -1
}

func (s *memoryReferrals) Used(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.find(id) >= 0, nil
}

func (s *memoryReferrals) Rewarded(_ context.Context, id string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.refs {
		if r = norm(r); r.ReferrerID == id && r.State == StateQualified && r.ReferrerRewarded {
			n++
		}
	}
	return n, nil
}

func (s *memoryReferrals) QualifiedSince(_ context.Context, id string, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.refs {
		if r = norm(r); r.ReferrerID == id && r.State == StateQualified && !r.EarnedAt().Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *memoryReferrals) Joins(_ context.Context, id string, limit int) (ReferralCounts, []Referral, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var counts ReferralCounts
	var out []Referral
	for _, r := range s.refs {
		if r.ReferrerID == id {
			r = norm(r)
			counts.add(r.State, 1)
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b Referral) int { return b.At.Compare(a.At) })
	return counts, out[:min(limit, len(out))], nil
}

func (s *memoryReferrals) UsedBy(_ context.Context, id string) (Referral, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.find(id); i >= 0 {
		return norm(s.refs[i]), true, nil
	}
	return Referral{}, false, nil
}

func (s *memoryReferrals) Open(_ context.Context, after string, limit int) ([]Referral, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Referral
	for _, r := range s.refs {
		r = norm(r)
		if r.ID <= after && after != "" {
			continue
		}
		if r.State.Open() || (r.State == StateQualified && !r.Settled) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Referral) int { return strings.Compare(a.ID, b.ID) })
	return out[:min(limit, len(out))], nil
}

func (s *memoryReferrals) edit(referred string, f func(r *Referral) bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(referred)
	if i < 0 {
		return false
	}
	r := norm(s.refs[i])
	ok := f(&r)
	if ok {
		s.refs[i] = r
	}
	return ok
}

func (s *memoryReferrals) SetSetupMet(_ context.Context, referred string, at time.Time) error {
	s.edit(referred, func(r *Referral) bool {
		if !r.SetupMetAt.IsZero() {
			return false
		}
		r.SetupMetAt = at
		return true
	})
	return nil
}

func (s *memoryReferrals) SetChecks(_ context.Context, referred string, c Checks) error {
	s.edit(referred, func(r *Referral) bool { r.Checks = c; return true })
	return nil
}

func (s *memoryReferrals) Move(_ context.Context, referred string, from []ReferralState, to ReferralState) (bool, error) {
	return s.edit(referred, func(r *Referral) bool {
		if !slices.Contains(from, r.State) {
			return false
		}
		r.State = to
		return true
	}), nil
}

func (s *memoryReferrals) Qualify(_ context.Context, referred string, at time.Time, rewarded bool) (bool, error) {
	return s.edit(referred, func(r *Referral) bool {
		if !r.State.Open() {
			return false
		}
		r.State, r.QualifiedAt, r.ReferrerRewarded = StateQualified, at, rewarded
		return true
	}), nil
}

func (s *memoryReferrals) Claim(_ context.Context, referred, flag string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flags == nil {
		s.flags = map[string]map[string]bool{}
	}
	if s.flags[referred] == nil {
		s.flags[referred] = map[string]bool{}
	}
	if s.flags[referred][flag] {
		return false, nil
	}
	s.flags[referred][flag] = true
	if flag == FlagSettled {
		if i := s.find(referred); i >= 0 {
			s.refs[i].Settled = true
		}
	}
	return true, nil
}

func (s *memoryReferrals) Report(_ context.Context, since time.Time, top int) (ReferralReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ReferralReport{Joined: len(s.refs)}
	counts := map[string]int{}
	for _, r := range s.refs {
		counts[r.ReferrerID]++
		out.Counts.add(norm(r).State, 1)
		if !r.At.Before(since) {
			out.JoinedSince++
		}
	}
	out.Codes = len(counts)
	for id, n := range counts {
		out.Top = append(out.Top, ReferrerCount{HouseholdID: id, Name: "Household " + id[len(id)-2:], Count: n})
	}
	slices.SortFunc(out.Top, func(a, b ReferrerCount) int { return b.Count - a.Count })
	out.Top = out.Top[:min(top, len(out.Top))]
	return out, nil
}

func (s *memoryReferrals) PurgeHousehold(context.Context, string) error { return nil }

type memorySetup struct {
	mu sync.Mutex
	m  map[string]Setup
}

func (s *memorySetup) Add(_ context.Context, id string, dinners, recipes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.m[id]
	v.Dinners += dinners
	v.Recipes += recipes
	s.m[id] = v
	return nil
}

func (s *memorySetup) Get(_ context.Context, id string) (Setup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id], nil
}

func (s *memorySetup) PurgeHousehold(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}

type memoryHistory struct {
	mu   sync.Mutex
	recs []MembershipRecord
}

func (s *memoryHistory) Add(_ context.Context, recs []MembershipRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range recs {
		i := slices.IndexFunc(s.recs, func(o MembershipRecord) bool { return o.Identity == r.Identity && o.HouseholdID == r.HouseholdID })
		if i < 0 {
			s.recs = append(s.recs, r)
		} else if r.At.Before(s.recs[i].At) {
			s.recs[i].At = r.At
		}
	}
	return nil
}

func (s *memoryHistory) ForIdentities(_ context.Context, ids []string) ([]MembershipRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []MembershipRecord
	for _, r := range s.recs {
		if slices.Contains(ids, r.Identity) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *memoryHistory) ForHousehold(_ context.Context, id string) ([]MembershipRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []MembershipRecord
	for _, r := range s.recs {
		if r.HouseholdID == id {
			out = append(out, r)
		}
	}
	return out, nil
}

type memoryDevices struct {
	mu sync.Mutex
	m  map[string]DeviceTie
}

func (s *memoryDevices) MarkOwned(_ context.Context, id string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.m[id]
	v.Owns = true
	s.m[id] = v
	return nil
}

func (s *memoryDevices) MarkElsewhere(_ context.Context, id string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.m[id]
	v.Elsewhere = true
	s.m[id] = v
	return nil
}

func (s *memoryDevices) Get(_ context.Context, id string) (DeviceTie, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id], nil
}

func (s *memoryDevices) PurgeHousehold(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}

type memoryLocks struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (s *memoryLocks) Acquire(_ context.Context, key string, now, until time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if held, ok := s.m[key]; ok && held.After(now) {
		return false, nil
	}
	s.m[key] = until
	return true, nil
}

func (s *memoryLocks) Release(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

type fakeHouseholds struct {
	mu sync.Mutex
	m  map[string]households.Household
}

func (f *fakeHouseholds) GetHousehold(_ context.Context, id string) (households.Household, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.m[id]
	if !ok {
		return households.Household{}, households.ErrNotFound
	}
	return h, nil
}

// fakeIdentities gives each user one Apple identity, its subject the user ID
// unless set.
type fakeIdentities struct {
	mu       sync.Mutex
	subjects map[string]string
}

func (f *fakeIdentities) ListIdentities(_ context.Context, userID string) ([]users.AuthIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sub := userID
	if s, ok := f.subjects[userID]; ok {
		sub = s
	}
	return []users.AuthIdentity{{UserID: userID, Provider: users.ProviderApple, Subject: sub}}, nil
}

type memoryNotifier struct {
	mu   sync.Mutex
	sent []notifications.New
	keys map[string]bool
}

func (m *memoryNotifier) Create(_ context.Context, in notifications.New) (notifications.Notification, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil {
		m.keys = map[string]bool{}
	}
	key := in.HouseholdID + "|" + in.DedupeKey
	if m.keys[key] {
		return notifications.Notification{}, false, nil
	}
	m.keys[key] = true
	m.sent = append(m.sent, in)
	return notifications.Notification{HouseholdID: in.HouseholdID, Title: in.Title, Body: in.Body}, true, nil
}

func (m *memoryNotifier) to(householdID string) []notifications.New {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []notifications.New
	for _, n := range m.sent {
		if n.HouseholdID == householdID {
			out = append(out, n)
		}
	}
	return out
}
