package plus

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// memoryOfferCodes is an in-memory OfferCodeStore following the Mongo
// contract.
type memoryOfferCodes struct {
	mu     sync.Mutex
	codes  []memoryCode
	keys   map[string]bool
	months []FreeMonth
	alerts bool
	next   int
}

type memoryCode struct {
	code, key, state, household, month string
	expires                            time.Time
	revealed                           bool
}

func newMemoryOfferCodes() *memoryOfferCodes { return &memoryOfferCodes{keys: map[string]bool{}} }

// stock adds n codes ending at expires.
func (s *memoryOfferCodes) stock(n int, expires time.Time) {
	codes := make([]string, 0, n)
	for range n {
		codes = append(codes, devCode())
	}
	_, _ = s.Import(context.Background(), codes, OfferBatch{Offer: "test", Batch: "test", ExpiresAt: expires}, time.Now())
}

func (s *memoryOfferCodes) AddMonth(_ context.Context, m FreeMonth) (FreeMonth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.months {
		if MonthKey(old.ReferralID, old.Role) == MonthKey(m.ReferralID, m.Role) {
			return old, nil
		}
	}
	s.next++
	m.ID, m.State = fmt.Sprintf("%024x", 0xf000+s.next), MonthOwed
	s.months = append(s.months, m)
	return m, nil
}

func (s *memoryOfferCodes) Months(_ context.Context, householdID string) ([]FreeMonth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []FreeMonth
	for _, m := range s.months {
		if m.HouseholdID == householdID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *memoryOfferCodes) Owed(_ context.Context, limit int) ([]FreeMonth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []FreeMonth
	for _, m := range s.months {
		if m.State == MonthOwed && len(out) < limit {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *memoryOfferCodes) monthLocked(id string) int {
	for i, m := range s.months {
		if m.ID == id {
			return i
		}
	}
	return -1
}

func (s *memoryOfferCodes) Give(_ context.Context, monthID string, now, notBefore time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.monthLocked(monthID)
	if i < 0 {
		return false, ErrFreeMonthNotFound
	}
	if s.months[i].State != MonthOwed {
		return true, nil
	}
	best := -1
	for j, c := range s.codes {
		if c.state == codeAvailable && c.expires.After(notBefore) && (best < 0 || c.expires.Before(s.codes[best].expires)) {
			best = j
		}
	}
	if best < 0 {
		return false, nil
	}
	c := &s.codes[best]
	c.state, c.household, c.month = codeGiven, s.months[i].HouseholdID, monthID
	s.months[i].State, s.months[i].GivenAt, s.months[i].CodeExpiresAt = MonthReady, now, c.expires
	return true, nil
}

func (s *memoryOfferCodes) ExpiredGiven(_ context.Context, now time.Time, limit int) ([]FreeMonth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []FreeMonth
	for _, m := range s.months {
		if m.State == MonthReady && !m.CodeExpiresAt.After(now) && len(out) < limit {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *memoryOfferCodes) Requeue(_ context.Context, monthID string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for j := range s.codes {
		if s.codes[j].month == monthID {
			s.codes[j].state, s.codes[j].month, s.codes[j].code = codeExpired, "", ""
		}
	}
	if i := s.monthLocked(monthID); i >= 0 && s.months[i].State == MonthReady {
		s.months[i].State, s.months[i].GivenAt, s.months[i].CodeExpiresAt, s.months[i].RevealedAt = MonthOwed, time.Time{}, time.Time{}, time.Time{}
	}
	return nil
}

func (s *memoryOfferCodes) Reveal(_ context.Context, householdID, monthID string, now time.Time) (string, FreeMonth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.monthLocked(monthID)
	if i < 0 || s.months[i].HouseholdID != householdID {
		return "", FreeMonth{}, ErrFreeMonthNotFound
	}
	switch s.months[i].State {
	case MonthOwed:
		return "", FreeMonth{}, ErrFreeMonthOwed
	case MonthRedeemed:
		return "", FreeMonth{}, ErrFreeMonthRedeemed
	}
	for j := range s.codes {
		if s.codes[j].month == monthID {
			s.codes[j].revealed = true
			if s.months[i].RevealedAt.IsZero() {
				s.months[i].RevealedAt = now
			}
			return s.codes[j].code, s.months[i], nil
		}
	}
	return "", FreeMonth{}, ErrFreeMonthNotFound
}

func (s *memoryOfferCodes) MarkRedeemed(_ context.Context, householdID string, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	best := -1
	for i, m := range s.months {
		if m.HouseholdID == householdID && m.State == MonthReady && !m.RevealedAt.IsZero() &&
			(best < 0 || m.RevealedAt.Before(s.months[best].RevealedAt)) {
			best = i
		}
	}
	if best < 0 {
		return false, nil
	}
	s.months[best].State, s.months[best].RedeemedAt = MonthRedeemed, at
	for j := range s.codes {
		if s.codes[j].month == s.months[best].ID {
			s.codes[j].state, s.codes[j].code = codeRedeemed, ""
		}
	}
	return true, nil
}

func (s *memoryOfferCodes) Import(_ context.Context, codes []string, b OfferBatch, _ time.Time) (ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res ImportResult
	for _, c := range codes {
		k := OfferCodeKey(c)
		if s.keys[k] {
			res.Duplicates++
			continue
		}
		s.keys[k] = true
		s.codes = append(s.codes, memoryCode{code: c, key: k, state: codeAvailable, expires: b.ExpiresAt})
		res.Added++
	}
	if res.Added > 0 {
		s.alerts = false
	}
	return res, nil
}

func (s *memoryOfferCodes) Stock(_ context.Context, now time.Time) (OfferStock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st OfferStock
	for _, c := range s.codes {
		switch c.state {
		case codeAvailable:
			if c.expires.After(now.Add(CodeMinLife)) {
				st.Available++
			} else {
				st.Expired++
			}
		case codeGiven:
			st.Given++
			if c.revealed {
				st.Revealed++
			}
		case codeRedeemed:
			st.Redeemed++
		}
	}
	for _, m := range s.months {
		if m.State == MonthOwed {
			st.Owed++
		}
	}
	return st, nil
}

func (s *memoryOfferCodes) ClaimShortageAlert(context.Context, time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.alerts {
		return false, nil
	}
	s.alerts = true
	return true, nil
}

func (s *memoryOfferCodes) PurgeHousehold(_ context.Context, householdID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for j := range s.codes {
		c := &s.codes[j]
		if c.household != householdID {
			continue
		}
		if c.state == codeGiven && !c.revealed {
			c.state, c.household, c.month = codeAvailable, "", ""
		} else {
			c.state, c.household, c.month, c.code = codeDeleted, "", "", ""
		}
	}
	s.months = slices.DeleteFunc(s.months, func(m FreeMonth) bool { return m.HouseholdID == householdID })
	return nil
}

// memoryAlerts records alert emails.
type memoryAlerts struct {
	mu   sync.Mutex
	sent []string
}

func (a *memoryAlerts) SendPlain(_ context.Context, _, subject, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, subject+"\n"+text)
	return nil
}
