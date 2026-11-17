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

// harness is a Service with every qualify store in memory, and DeviceCheck
// faked so one token is one device.
type harness struct {
	t       *testing.T
	s       *Service
	subs    *memoryStore
	free    *memoryFree
	refs    *memoryReferrals
	setup   *memorySetup
	history *memoryHistory
	devices *memoryDevices
	dc      *MemoryDeviceCheck
	homes   *fakeHouseholds
	ids     *fakeIdentities
	notes   *memoryNotifier
}

var harnesses sync.Map // *Service -> *harness

func newHarness(t *testing.T, now time.Time) *harness {
	t.Helper()
	h := &harness{
		t: t, subs: newMemoryStore(), free: &memoryFree{m: map[string]time.Time{}},
		refs: &memoryReferrals{codes: map[string]string{}}, setup: &memorySetup{m: map[string]Setup{}},
		history: &memoryHistory{}, devices: &memoryDevices{m: map[string]DeviceTie{}}, dc: NewMemoryDeviceCheck(),
		homes: &fakeHouseholds{m: map[string]households.Household{}}, ids: &fakeIdentities{subjects: map[string]string{}},
		notes: &memoryNotifier{},
	}
	h.s = NewService(h.subs, true).WithReferrals(h.free, h.refs).WithQualify(QualifyOptions{
		Setup: h.setup, History: h.history, Devices: h.devices, Locks: &memoryLocks{m: map[string]time.Time{}},
		Households: h.homes, Identities: h.ids, Notifier: h.notes, DeviceCheck: h.dc,
	})
	h.s.now = func() time.Time { return now }
	harnesses.Store(h.s, h)
	t.Cleanup(func() { harnesses.Delete(h.s) })
	return h
}

func harnessOf(s *Service) *harness {
	h, _ := harnesses.Load(s)
	return h.(*harness)
}

func (h *harness) at(t time.Time) { h.s.now = func() time.Time { return t } }

func deviceCtx(ctx context.Context, device string) context.Context {
	if device == "" {
		return ctx
	}
	return households.WithDeviceProof(ctx, households.DeviceProof{Token: device})
}

// create makes household id, named name, by user on device ("" for no
// DeviceCheck token).
func (h *harness) create(id, name, user, device string) {
	h.t.Helper()
	now := h.s.now().UTC()
	h.homes.mu.Lock()
	h.homes.m[id] = households.Household{ID: id, Name: name, CreatedBy: user, CreatedAt: now}
	h.homes.mu.Unlock()
	h.s.MemberAdded(deviceCtx(h.t.Context(), device), households.MemberAdded{HouseholdID: id, UserID: user, Created: true, At: now})
}

// join adds user to household id from device.
func (h *harness) join(id, user, device string) {
	h.t.Helper()
	h.s.MemberAdded(deviceCtx(h.t.Context(), device), households.MemberAdded{HouseholdID: id, UserID: user, At: h.s.now().UTC()})
}

func (h *harness) redeem(id, code, device string) (Status, error) {
	return h.s.Redeem(deviceCtx(h.t.Context(), device), id, code)
}

// setUp plans the dinners and saves the recipes the rules ask for.
func (h *harness) setUp(id string) {
	h.s.DinnersPlanned(h.t.Context(), id, h.s.rules.MinDinners)
	h.s.RecipesSaved(h.t.Context(), id, h.s.rules.MinRecipes)
}

func (h *harness) state(id string) ReferralState {
	r, ok, _ := h.refs.UsedBy(h.t.Context(), id)
	if !ok {
		return ""
	}
	return r.State
}

func (h *harness) code(id string) string {
	info, err := h.s.Referral(h.t.Context(), id)
	if err != nil {
		h.t.Fatal(err)
	}
	return info.Code
}

// referralService is a harness with hh, hhB, and hhC created by different
// people on different devices.
func referralService(t *testing.T, now time.Time) (*Service, *memoryStore, *memoryFree, *memoryReferrals) {
	t.Helper()
	h := newHarness(t, now)
	h.at(now.Add(-time.Hour))
	h.create(hh, "Lines", "66e5a1f2c3b4a5d6e7f80b01", "device-hh")
	h.create(hhB, "Parkers", "66e5a1f2c3b4a5d6e7f80b02", "device-hhB")
	h.create(hhC, "Okafors", "66e5a1f2c3b4a5d6e7f80b03", "device-hhC")
	h.at(now)
	return h.s, h.subs, h.free, h.refs
}

// setUpHousehold gives a household created by referralService the setup
// a referral needs, so its referral qualifies.
func setUpHousehold(s *Service, id string) { harnessOf(s).setUp(id) }

const month = FreeMonthDays * 24 * time.Hour

// A new household that enters another's code gets a free month once it is
// set up, and so does the household that shared it.
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
	if err != nil || st.Active || !st.FreeUntil.IsZero() {
		t.Fatalf("redeem = %+v, %v; want pending, no free time yet", st, err)
	}
	setUpHousehold(s, hhB)
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
	setUpHousehold(s, hhB)
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
	h := harnessOf(s)
	s.rules.MaxPer30Days = 100
	code, _ := s.Referral(ctx, hh)
	for i := range MaxReferralMonths + 2 {
		id := "66e5a1f2c3b4a5d6e7f8" + string(rune('a'+i/16)) + string("0123456789abcdef"[i%16]) + "ff"
		h.at(now.Add(-time.Minute))
		h.create(id, "Household", "66e5a1f2c3b4a5d6e7f9"+id[20:], "device-"+id)
		h.at(now)
		if _, err := s.Redeem(ctx, id, code.Code); err != nil {
			t.Fatalf("redeem %d (%s): %v", i, id, err)
		}
		h.setUp(id)
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
	setUpHousehold(s, hhB)
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

func TestNewCode(t *testing.T) {
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
	rec = do(http.MethodPost, "/households/"+hhB+"/plus/referral/redeem", "u2",
		`{"code":"`+ref.Code+`","deviceCheck":{"token":"device-hhB","development":true}}`)
	var st StatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != http.StatusOK || st.Active || st.FreeUntil != nil || st.ReferralProgress == nil ||
		st.ReferralProgress.State != "pending" || len(st.ReferralProgress.Steps) != 3 ||
		st.ReferralProgress.Summary != "Free month: 1 of 3 done" || st.ReferralProgress.TimeLeft != "14 days left" ||
		!strings.Contains(rec.Body.String(), `"plusTitle":"What is DinnerOS Plus?"`) || len(st.ReferralProgress.PlusLines) != len(PlusExplainerLines) {
		t.Errorf("redeem = %d %s", rec.Code, rec.Body.String())
	}
	setUpHousehold(s, hhB)
	rec = do(http.MethodGet, "/households/"+hhB+"/plus", "u2", "")
	st = StatusResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.Active || st.FreeUntil == nil || st.ReferralProgress == nil || st.ReferralProgress.State != "qualified" {
		t.Errorf("after setup = %s", rec.Body.String())
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
	if len(o.Links) != 2 || o.Links[0].URL != PrivacyURL || o.Links[1].URL != TermsURL {
		t.Errorf("links = %+v, want privacy then terms", o.Links)
	}
	for _, l := range o.Links {
		if !strings.HasPrefix(l.URL, "https://dinneros.app/") || l.Title == "" {
			t.Errorf("link %+v", l)
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

	if err := refs.Record(ctx, &Referral{State: StateQualified, ReferrerID: hh, ReferredID: hhB, At: at, ReferrerRewarded: true}); err != nil {
		t.Fatal(err)
	}
	if err := refs.Record(ctx, &Referral{State: StateQualified, ReferrerID: hhC, ReferredID: hhB, At: at}); !errors.Is(err, ErrAlreadyRedeemed) {
		t.Errorf("second record = %v", err)
	}
	if err := refs.Record(ctx, &Referral{State: StateQualified, ReferrerID: hh, ReferredID: hhC, At: at}); err != nil {
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

// With Plus off, a test household still sees Plus and its paywall, and
// everyone else keeps everything open.
func TestTestHouseholds(t *testing.T) {
	s := NewService(newMemoryStore(), false).
		WithReferrals(&memoryFree{m: map[string]time.Time{}}, &memoryReferrals{codes: map[string]string{}}).
		WithTestHouseholds([]string{hh})
	ctx := context.Background()
	if st, err := s.Status(ctx, hh); err != nil || !st.Enabled || st.Active {
		t.Errorf("tester status = %+v, %v; want enabled, not active", st, err)
	}
	if st, err := s.Status(ctx, hhB); err != nil || st.Enabled {
		t.Errorf("other status = %+v, %v; want off", st, err)
	}
	if _, err := s.Referral(ctx, hh); err != nil {
		t.Errorf("tester referral: %v", err)
	}
	if _, err := s.Referral(ctx, hhB); !errors.Is(err, ErrReferralsOff) {
		t.Errorf("other referral err = %v, want off", err)
	}

	r := chi.NewRouter()
	r.With(Require(s, nil)).Get("/households/{householdId}/x", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for id, want := range map[string]int{hh: http.StatusPaymentRequired, hhB: http.StatusNoContent} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/households/"+id+"/x", nil))
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", id, rec.Code, want)
		}
	}
}
