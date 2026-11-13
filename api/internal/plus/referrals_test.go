package plus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

const (
	hhB = "66e5a1f2c3b4a5d6e7f80a02"
	hhC = "66e5a1f2c3b4a5d6e7f80a03"
)

type memoryFree struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (s *memoryFree) FreeUntil(_ context.Context, id string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id], nil
}

func (s *memoryFree) Extend(_ context.Context, id string, from time.Time, d time.Duration) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := s.m[id]
	if from.After(start) {
		start = from
	}
	s.m[id] = start.Add(d)
	return s.m[id], nil
}

func (s *memoryFree) PurgeHousehold(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}

type memoryReferrals struct {
	mu    sync.Mutex
	codes map[string]string // household -> code
	refs  []Referral
}

func (s *memoryReferrals) Code(_ context.Context, id string, newCode func() string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.codes[id]; ok {
		return c, nil
	}
	s.codes[id] = newCode()
	return s.codes[id], nil
}

func (s *memoryReferrals) Owner(_ context.Context, code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.codes {
		if c == code {
			return id, nil
		}
	}
	return "", ErrCodeNotFound
}

func (s *memoryReferrals) Record(_ context.Context, r Referral) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.refs {
		if old.ReferredID == r.ReferredID {
			return ErrAlreadyRedeemed
		}
	}
	s.refs = append(s.refs, r)
	return nil
}

func (s *memoryReferrals) Used(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.refs {
		if r.ReferredID == id {
			return true, nil
		}
	}
	return false, nil
}

func (s *memoryReferrals) Rewarded(_ context.Context, id string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.refs {
		if r.ReferrerID == id && r.ReferrerRewarded {
			n++
		}
	}
	return n, nil
}

func (s *memoryReferrals) PurgeHousehold(context.Context, string) error { return nil }

func referralService(t *testing.T, now time.Time) (*Service, *memoryStore, *memoryFree, *memoryReferrals) {
	t.Helper()
	subs, free := newMemoryStore(), &memoryFree{m: map[string]time.Time{}}
	refs := &memoryReferrals{codes: map[string]string{}}
	s := NewService(subs, true).WithReferrals(free, refs)
	s.now = func() time.Time { return now }
	return s, subs, free, refs
}

const month = FreeMonthDays * 24 * time.Hour

// A new household that enters another's code gets a free month, and so does
// the household that shared it.
func TestReferralGivesBothHouseholdsAFreeMonth(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, _, _ := referralService(t, now)

	info, err := s.Referral(ctx, hh)
	if err != nil || len(info.Code) != CodeLength || info.Rewarded != 0 || info.MonthsLeft != MaxReferralMonths {
		t.Fatalf("referral = %+v, %v", info, err)
	}
	if again, _ := s.Referral(ctx, hh); again.Code != info.Code {
		t.Error("a household's code changed")
	}

	// Typed the way a person might: lowercase, with a dash and a space.
	typed := strings.ToLower(info.Code[:4]) + "- " + info.Code[4:]
	st, err := s.Redeem(ctx, hhB, typed)
	if err != nil || !st.Active || !st.FreeUntil.Equal(now.Add(month)) {
		t.Fatalf("redeem = %+v, %v", st, err)
	}
	if ok, _ := s.HasPlus(ctx, hhB); !ok {
		t.Error("new household has no Plus")
	}
	if st, _ := s.Status(ctx, hh); !st.Active || !st.FreeUntil.Equal(now.Add(month)) {
		t.Errorf("sharer = %+v", st)
	}
	if info, _ := s.Referral(ctx, hh); info.Rewarded != 1 || info.MonthsLeft != MaxReferralMonths-1 {
		t.Errorf("after one = %+v", info)
	}

	// Free time ends.
	s.now = func() time.Time { return now.Add(month + time.Hour) }
	if ok, _ := s.HasPlus(ctx, hhB); ok {
		t.Error("still Plus after the free month")
	}
}

func TestReferralRules(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, subs, _, _ := referralService(t, now)
	code, _ := s.Referral(ctx, hh)

	for name, tc := range map[string]struct {
		household, code string
		want            error
	}{
		"unknown code":  {hhB, "ZZZZZZZZ", ErrCodeNotFound},
		"too short":     {hhB, "ABC", ErrCodeNotFound},
		"empty":         {hhB, "", ErrCodeNotFound},
		"your own code": {hh, code.Code, ErrOwnCode},
	} {
		if _, err := s.Redeem(ctx, tc.household, tc.code); !errors.Is(err, tc.want) {
			t.Errorf("%s = %v, want %v", name, err, tc.want)
		}
	}

	if _, err := s.Redeem(ctx, hhB, code.Code); err != nil {
		t.Fatal(err)
	}
	// Once per household, whichever code.
	other, _ := s.Referral(ctx, hhC)
	if _, err := s.Redeem(ctx, hhB, other.Code); !errors.Is(err, ErrAlreadyRedeemed) {
		t.Errorf("second code = %v", err)
	}
	// The sharer earned free months, so it isn't new to Plus either.
	if _, err := s.Redeem(ctx, hh, other.Code); !errors.Is(err, ErrNotNewToPlus) {
		t.Errorf("sharer using a code = %v", err)
	}

	// A household that ever subscribed isn't new to Plus.
	subs.m[hhC] = Entitlement{HouseholdID: hhC, Ended: true, LastEventAt: now.Add(-48 * time.Hour)}
	if _, err := s.Redeem(ctx, hhC, code.Code); !errors.Is(err, ErrNotNewToPlus) {
		t.Errorf("past subscriber = %v", err)
	}
}

// The sharer earns at most MaxReferralMonths; the new household still gets
// its month.
func TestReferralMonthsAreCapped(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, free, _ := referralService(t, now)
	code, _ := s.Referral(ctx, hh)
	for i := range MaxReferralMonths + 2 {
		id := "66e5a1f2c3b4a5d6e7f8" + string(rune('a'+i/16)) + string("0123456789abcdef"[i%16]) + "ff"
		if _, err := s.Redeem(ctx, id, code.Code); err != nil {
			t.Fatalf("redeem %d (%s): %v", i, id, err)
		}
		if !free.m[id].Equal(now.Add(month)) {
			t.Errorf("household %d got %v", i, free.m[id])
		}
	}
	if want := now.Add(MaxReferralMonths * month); !free.m[hh].Equal(want) {
		t.Errorf("sharer free until %v, want %v", free.m[hh], want)
	}
	info, _ := s.Referral(ctx, hh)
	if info.MonthsLeft != 0 || info.Rewarded != MaxReferralMonths {
		t.Errorf("info = %+v", info)
	}
	if _, explainer := ReferralText(info.Code, info.Rewarded, info.MonthsLeft); !strings.Contains(explainer, "every free month") {
		t.Errorf("explainer = %q", explainer)
	}
}

// A paying household's free month starts when its paid time ends, since
// Apple keeps billing it.
func TestReferralMonthWaitsForPaidTime(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, subs, free, _ := referralService(t, now)
	paidTo := now.Add(10 * 24 * time.Hour)
	subs.m[hh] = Entitlement{HouseholdID: hh, WillRenew: true, ExpiresAt: paidTo, LastEventAt: now}
	code, _ := s.Referral(ctx, hh)
	if _, err := s.Redeem(ctx, hhB, code.Code); err != nil {
		t.Fatal(err)
	}
	if !free.m[hh].Equal(paidTo.Add(month)) {
		t.Errorf("sharer free until %v, want %v", free.m[hh], paidTo.Add(month))
	}
}

func TestReferralsOffWhilePlusIsOff(t *testing.T) {
	s := NewService(newMemoryStore(), false).WithReferrals(&memoryFree{m: map[string]time.Time{}}, &memoryReferrals{codes: map[string]string{}})
	if _, err := s.Referral(t.Context(), hh); !errors.Is(err, ErrReferralsOff) {
		t.Errorf("referral = %v", err)
	}
	if _, err := s.Redeem(t.Context(), hhB, "ABCDEFGH"); !errors.Is(err, ErrReferralsOff) {
		t.Errorf("redeem = %v", err)
	}
}

func TestNormalizeCode(t *testing.T) {
	for in, want := range map[string]string{
		"abcd-efgh":                            "ABCDEFGH",
		" AB CD EF GH ":                        "ABCDEFGH",
		"https://dinneros.app/invite/ABCD2345": "ABCD2345",
		"dinneros://plus?code=abcd2345":        "ABCD2345",
	} {
		if got := NormalizeCode(in); got != want {
			t.Errorf("NormalizeCode(%q) = %q, want %q", in, got, want)
		}
	}
	for range 50 {
		c := NewCode()
		if len(c) != CodeLength || strings.ContainsAny(c, "01OIL") {
			t.Fatalf("NewCode = %q", c)
		}
	}
}

func TestReferralRoutes(t *testing.T) {
	now := time.Now()
	s, _, _, _ := referralService(t, now)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: s, Authorizer: twoHouseholds{}, Tokens: fakeTokens{}}).Mount)
	do := func(method, path, user, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	rec := do(http.MethodGet, "/households/"+hh+"/plus/referral", "u1", "")
	var ref ReferralResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &ref)
	if rec.Code != http.StatusOK || !strings.Contains(ref.ShareText, ref.Code) || ref.Explainer == "" {
		t.Fatalf("referral = %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/households/"+hh+"/plus/referral/redeem", "u1", `{"code":"`+ref.Code+`"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "your own code") {
		t.Errorf("own code = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/households/"+hhB+"/plus/referral/redeem", "u2", `{"code":"ZZZZZZZZ"}`)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "couldn't find that code") {
		t.Errorf("unknown = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/households/"+hhB+"/plus/referral/redeem", "u2", `{"code":"`+ref.Code+`"}`)
	var st StatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != http.StatusOK || !st.Active || st.FreeUntil == nil {
		t.Errorf("redeem = %d %s", rec.Code, rec.Body.String())
	}

	// The status carries the offer while Plus is on.
	rec = do(http.MethodGet, "/households/"+hh+"/plus", "u1", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.Offer == nil || len(st.Offer.Plans) != 2 || st.Offer.TrialDays != TrialDays {
		t.Errorf("offer = %s", rec.Body.String())
	}
}

// The offer's words are plain: no em dashes and nothing internal.
func TestOfferCopy(t *testing.T) {
	o := DefaultOffer()
	raw, _ := json.Marshal(o)
	for _, bad := range []string{"—", "entitlement", "RevenueCat", "household_id"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("offer contains %q", bad)
		}
	}
	for _, err := range []error{ErrCodeNotFound, ErrOwnCode, ErrAlreadyRedeemed, ErrNotNewToPlus, errors.New("x")} {
		if m := RedeemMessage(err); m == "" || strings.Contains(m, "—") {
			t.Errorf("message for %v = %q", err, m)
		}
	}
}

func TestRequirePlus(t *testing.T) {
	now := time.Now()
	s, _, free, _ := referralService(t, now)
	r := chi.NewRouter()
	r.With(Require(s, nil)).Get("/households/{householdId}/x", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/households/"+hh+"/x", nil))
		return rec
	}
	if rec := get(); rec.Code != http.StatusPaymentRequired || !strings.Contains(rec.Body.String(), "plus_required") ||
		!strings.Contains(rec.Body.String(), RequiredMessage) {
		t.Errorf("free household = %d %s", rec.Code, rec.Body.String())
	}
	free.m[hh] = now.Add(time.Hour)
	if rec := get(); rec.Code != http.StatusNoContent {
		t.Errorf("Plus household = %d", rec.Code)
	}

	// Plus off: everything open.
	off := chi.NewRouter()
	off.With(Require(NewService(newMemoryStore(), false), nil)).Get("/households/{householdId}/x", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	off.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/households/"+hhB+"/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("off = %d", rec.Code)
	}
}

type twoHouseholds struct{}

func (twoHouseholds) Authorize(_ context.Context, householdID, userID string, _ households.Permission) (households.Membership, error) {
	if (householdID == hh && userID == "u1") || (householdID == hhB && userID == "u2") {
		return households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}, nil
	}
	return households.Membership{}, households.ErrNotFound
}

func TestIntegrationReferralMongoStores(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	if err := client.EnsureIndexes(ctx, ReferralIndexes()...); err != nil {
		t.Fatal(err)
	}
	free, refs := NewMongoFreeTime(client.Database()), NewMongoReferrals(client.Database())

	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	if u, _ := free.FreeUntil(ctx, hh); !u.IsZero() {
		t.Errorf("empty = %v", u)
	}
	if u, err := free.Extend(ctx, hh, at, month); err != nil || !u.Equal(at.Add(month)) {
		t.Fatalf("first = %v, %v", u, err)
	}
	// Stacks on the current end, not on from.
	if u, _ := free.Extend(ctx, hh, at, month); !u.Equal(at.Add(2 * month)) {
		t.Errorf("second = %v", u)
	}
	// A later from wins over an earlier end.
	later := at.Add(100 * 24 * time.Hour)
	if u, _ := free.Extend(ctx, hh, later, month); !u.Equal(later.Add(month)) {
		t.Errorf("later = %v", u)
	}

	// A taken code is drawn again.
	codes := []string{"AAAAAAAA", "AAAAAAAA", "BBBBBBBB"}
	next := func() string { c := codes[0]; codes = codes[1:]; return c }
	a, err := refs.Code(ctx, hh, next)
	if err != nil || a != "AAAAAAAA" {
		t.Fatalf("code a = %q, %v", a, err)
	}
	b, err := refs.Code(ctx, hhB, next)
	if err != nil || b != "BBBBBBBB" {
		t.Fatalf("code b = %q, %v", b, err)
	}
	if again, _ := refs.Code(ctx, hh, NewCode); again != a {
		t.Errorf("code changed to %q", again)
	}
	if owner, err := refs.Owner(ctx, b); err != nil || owner != hhB {
		t.Errorf("owner = %q, %v", owner, err)
	}
	if _, err := refs.Owner(ctx, "ZZZZZZZZ"); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("unknown owner = %v", err)
	}

	if err := refs.Record(ctx, Referral{ReferrerID: hh, ReferredID: hhB, At: at, ReferrerRewarded: true}); err != nil {
		t.Fatal(err)
	}
	if err := refs.Record(ctx, Referral{ReferrerID: hhC, ReferredID: hhB, At: at}); !errors.Is(err, ErrAlreadyRedeemed) {
		t.Errorf("second record = %v", err)
	}
	if err := refs.Record(ctx, Referral{ReferrerID: hh, ReferredID: hhC, At: at}); err != nil {
		t.Fatal(err)
	}
	if used, _ := refs.Used(ctx, hhB); !used {
		t.Error("hhB used a code")
	}
	if used, _ := refs.Used(ctx, hh); used {
		t.Error("hh never used a code")
	}
	if n, _ := refs.Rewarded(ctx, hh); n != 1 {
		t.Errorf("rewarded = %d", n)
	}

	if err := refs.PurgeHousehold(ctx, hh); err != nil {
		t.Fatal(err)
	}
	if _, err := refs.Owner(ctx, a); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("purged code still works: %v", err)
	}
	if n, _ := refs.Rewarded(ctx, hh); n != 0 {
		t.Errorf("purged rewarded = %d", n)
	}
	if err := free.PurgeHousehold(ctx, hh); err != nil {
		t.Fatal(err)
	}
	if u, _ := free.FreeUntil(ctx, hh); !u.IsZero() {
		t.Errorf("purged free = %v", u)
	}
}
