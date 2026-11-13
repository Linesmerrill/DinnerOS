package plus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

type memoryBilling struct {
	mu sync.Mutex
	m  []BillingEvent
}

func (s *memoryBilling) Add(_ context.Context, e BillingEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.m {
		if old.EventID == e.EventID {
			return nil
		}
	}
	s.m = append(s.m, e)
	return nil
}

func (s *memoryBilling) List(_ context.Context, id string, limit int) ([]BillingEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []BillingEvent
	for _, e := range s.m {
		if e.HouseholdID == id {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b BillingEvent) int { return b.At.Compare(a.At) })
	return out[:min(limit, len(out))], nil
}

func (s *memoryBilling) PurgeHousehold(context.Context, string) error { return nil }

type fixedStats Stats

func (f fixedStats) PlusStats(context.Context, string, time.Time) (Stats, error) {
	return Stats(f), nil
}

func price(v float64) *float64 { return &v }

func detailsService(t *testing.T, now time.Time) (*Service, *memoryStore, *memoryBilling, *memoryReferrals) {
	t.Helper()
	s, subs, _, refs := referralService(t, now)
	billing := &memoryBilling{}
	s.WithBilling(billing)
	return s, subs, billing, refs
}

func admin(user string) households.Membership {
	return households.Membership{HouseholdID: hh, UserID: user, Role: households.RoleAdmin}
}

func member(user string) households.Membership {
	return households.Membership{HouseholdID: hh, UserID: user, Role: households.RoleMember}
}

// Webhook events land in billing history once each, with the right words.
func TestBillingHistoryFromWebhook(t *testing.T) {
	ctx := t.Context()
	start := time.Date(2026, 11, 13, 21, 2, 54, 0, time.UTC)
	s, subs, billing, _ := detailsService(t, start)

	trial := event("e1", "INITIAL_PURCHASE", start, start.Add(14*24*time.Hour))
	trial.PeriodType = "TRIAL"
	trial.Price, trial.Currency = price(0), "usd"
	trial.SubscriberAttributes = map[string]struct {
		Value string `json:"value"`
	}{StartedByAttribute: {Value: "u1"}}
	renew := event("e2", "RENEWAL", start.Add(14*24*time.Hour), start.Add(44*24*time.Hour))
	renew.Price, renew.Currency = price(4.99), "USD"
	refund := event("e3", "CANCELLATION", start.Add(15*24*time.Hour), start.Add(44*24*time.Hour))
	refund.CancelReason = "CUSTOMER_SUPPORT"
	for _, ev := range []Event{trial, trial, renew, refund} {
		if _, err := s.Apply(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(billing.m) != 3 {
		t.Fatalf("billing = %+v, want 3 (repeat ignored)", billing.m)
	}
	titles := []string{}
	list, _ := billing.List(ctx, hh, 10)
	for _, b := range list {
		titles = append(titles, b.Title())
	}
	if want := []string{"Refunded", "Renewed", "Trial started"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %v, want %v", titles, want)
	}
	if list[1].Amount != 4.99 || list[1].Currency != "USD" || list[2].Amount != 0 {
		t.Errorf("amounts = %+v", list)
	}
	e := subs.m[hh]
	if e.StartedBy != "u1" || !e.Since.Equal(start) {
		t.Errorf("entitlement = %+v", e)
	}
	// A later purchase event doesn't move Since or who started it.
	again := event("e4", "INITIAL_PURCHASE", start.Add(60*24*time.Hour), start.Add(90*24*time.Hour))
	again.SubscriberAttributes = map[string]struct {
		Value string `json:"value"`
	}{StartedByAttribute: {Value: "u2"}}
	if _, err := s.Apply(ctx, again); err != nil {
		t.Fatal(err)
	}
	if e := subs.m[hh]; !e.Since.Equal(start) || e.StartedBy != "u2" {
		t.Errorf("after a new purchase = %+v", e)
	}

	for _, typ := range []string{"INITIAL_PURCHASE", "RENEWAL", "PRODUCT_CHANGE", "CANCELLATION", "UNCANCELLATION",
		"EXPIRATION", "BILLING_ISSUE", "REFUND_REVERSED", "NON_RENEWING_PURCHASE"} {
		if title := (BillingEvent{Type: typ}).Title(); title == "" || strings.Contains(title, "—") {
			t.Errorf("%s title = %q", typ, title)
		}
	}
}

func TestYourPlusStates(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 22, 0, 0, 0, time.UTC)
	s, subs, _, _ := detailsService(t, now)
	s.WithStats(fixedStats{WeeksPlanned: 1, MealsPlanned: 5, CartsSent: 2, SavedCents: ptr64(4200)})

	base := Entitlement{HouseholdID: hh, ProductID: "com.linesmerrill.dinneros.plus.monthly", LastEventAt: now.Add(-time.Hour),
		Since: now.Add(-time.Hour), StartedBy: "u1", ExpiresAt: now.Add(24 * time.Hour)}
	for name, tc := range map[string]struct {
		edit             func(*Entitlement)
		state, text, lbl string
	}{
		"trial":    {func(e *Entitlement) { e.Trial, e.WillRenew = true, true }, "trial", "Free trial", "Renews on"},
		"active":   {func(e *Entitlement) { e.WillRenew = true }, "active", "Active", "Renews on"},
		"canceled": {func(e *Entitlement) {}, "canceled", "Canceled", "Ends on"},
		"expired":  {func(e *Entitlement) { e.Ended = true }, "expired", "Expired", "Ended on"},
	} {
		e := base
		tc.edit(&e)
		subs.m[hh] = e
		d, err := s.Details(ctx, hh, member("u1"))
		if err != nil {
			t.Fatal(err)
		}
		resp := DetailsFor(d, "Merrill", now)
		if resp.State != tc.state || resp.StateText != tc.text || resp.DateLabel != tc.lbl || resp.Date == nil {
			t.Errorf("%s: %+v", name, resp)
		}
		if resp.Plan == nil || resp.Plan.Name != "Monthly" || resp.Plan.PriceText != "$4.99 a month" {
			t.Errorf("%s plan = %+v", name, resp.Plan)
		}
	}

	// Free months only.
	delete(subs.m, hh)
	_, _ = s.free.Extend(ctx, hh, now, month)
	d, _ := s.Details(ctx, hh, admin("u9"))
	if resp := DetailsFor(d, "", now); resp.State != "free" || resp.DateLabel != "Free until" || resp.Plan != nil {
		t.Errorf("free = %+v", resp)
	}
}

func ptr64(v int64) *int64 { return &v }

// The member who started Plus manages it; others see status only.
func TestYourPlusWhoCanManage(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 22, 0, 0, 0, time.UTC)
	s, subs, billing, refs := detailsService(t, now)
	subs.m[hh] = Entitlement{HouseholdID: hh, ProductID: "com.linesmerrill.dinneros.plus.yearly", WillRenew: true,
		ExpiresAt: now.Add(300 * 24 * time.Hour), Since: now.Add(-48 * time.Hour), StartedBy: "u1", LastEventAt: now}
	_ = billing.Add(ctx, BillingEvent{HouseholdID: hh, EventID: "b1", Type: "INITIAL_PURCHASE", At: now.Add(-48 * time.Hour), Amount: 39.99, Currency: "USD"})
	refs.refs = append(refs.refs,
		Referral{ReferrerID: hh, ReferredID: hhB, At: now.Add(-24 * time.Hour), ReferrerRewarded: true},
		Referral{ReferrerID: hhC, ReferredID: hh, At: now.Add(-72 * time.Hour)})

	d, _ := s.Details(ctx, hh, member("u1"))
	resp := DetailsFor(d, "Merrill", now)
	if !resp.CanManage || resp.ManageNote != "" || resp.StartedBy != "Merrill" || resp.Badge != FoundingBadge {
		t.Errorf("starter = %+v", resp)
	}
	// Billing and free months, newest first; Plus started with the code.
	got := []string{}
	for _, h := range resp.History {
		got = append(got, h.Kind+":"+h.Title)
	}
	want := []string{"invite:Free month from a household you invited", "billing:Subscribed", "invite:Free month from a code you used"}
	if !slices.Equal(got, want) {
		t.Errorf("history = %v, want %v", got, want)
	}
	if a := resp.History[1].Amount; a == nil || *a != 39.99 || resp.History[1].Currency != "USD" {
		t.Errorf("amount = %+v", resp.History[1])
	}
	if !resp.Since.Equal(now.Add(-72*time.Hour)) || resp.MonthsEarned != 1 || resp.MonthsCap != MaxReferralMonths {
		t.Errorf("since/months = %v %d", resp.Since, resp.MonthsEarned)
	}

	other, _ := s.Details(ctx, hh, admin("u2"))
	resp = DetailsFor(other, "Merrill", now)
	if resp.CanManage || resp.ManageNote == "" || len(resp.History) != 0 || resp.State != "active" {
		t.Errorf("other member = %+v", resp)
	}

	// Who started it isn't known (bought before the app said): any admin manages.
	e := subs.m[hh]
	e.StartedBy = ""
	subs.m[hh] = e
	if d, _ := s.Details(ctx, hh, admin("u2")); !d.CanManage {
		t.Error("admin can't manage an unknown starter's Plus")
	}
	if d, _ := s.Details(ctx, hh, member("u3")); d.CanManage {
		t.Error("member can manage an unknown starter's Plus")
	}

	// Late households aren't founding members.
	late := Details{Since: FoundingMemberBefore.Add(time.Hour)}
	if DetailsFor(late, "", now).Badge != "" {
		t.Error("late household has the badge")
	}
}

func TestYourPlusStats(t *testing.T) {
	now := time.Date(2026, 11, 13, 22, 0, 0, 0, time.UTC)
	d := Details{Since: now, Stats: &Stats{WeeksPlanned: 1, MealsPlanned: 5, CartsSent: 0, SavedCents: ptr64(4200)}}
	resp := DetailsFor(d, "", now)
	labels := []string{}
	for _, s := range resp.Stats {
		labels = append(labels, s.Label)
	}
	// Carts sent is zero, so it's left out.
	want := []string{"week planned by Autopilot", "meals planned", "saved vs meal kits"}
	if !slices.Equal(labels, want) {
		t.Errorf("labels = %v", labels)
	}
	if c := resp.Stats[2].Cents; c == nil || *c != 4200 || resp.StatsTitle != StatsTitle {
		t.Errorf("saved = %+v", resp.Stats[2])
	}
	// No meal kit price: no savings line.
	d.Stats.SavedCents = nil
	if len(DetailsFor(d, "", now).Stats) != 2 {
		t.Error("savings shown without a meal kit price")
	}
	for _, s := range []string{HistoryNote, ManageNote, StatsTitle, SinceText, FoundingBadge} {
		if strings.Contains(s, "—") {
			t.Errorf("%q has an em dash", s)
		}
	}
}

func TestYourPlusRoute(t *testing.T) {
	now := time.Now().UTC()
	s, subs, _, _ := detailsService(t, now)
	subs.m[hh] = Entitlement{HouseholdID: hh, ProductID: "com.linesmerrill.dinneros.plus.monthly", Trial: true, WillRenew: true,
		ExpiresAt: now.Add(24 * time.Hour), Since: now, StartedBy: "u1", LastEventAt: now}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: s, Authorizer: twoHouseholds{}, Tokens: fakeTokens{},
		MemberName: func(_ context.Context, id string) string { return map[string]string{"u1": "Merrill"}[id] },
	}).Mount)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+hh+"/plus/details", nil)
	req.Header.Set("Authorization", "Bearer token-u1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var resp DetailsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("details = %d %s", rec.Code, rec.Body.String())
	}
	if resp.State != "trial" || resp.StartedBy != "Merrill" || !resp.CanManage || resp.HistoryNote != HistoryNote ||
		resp.History == nil || resp.Stats == nil {
		t.Errorf("details = %s", rec.Body.String())
	}

	// Plus off for the household: 404, like the other Plus routes.
	off := chi.NewRouter()
	off.Route("/api/v1", NewHandler(HandlerOptions{Service: NewService(newMemoryStore(), false), Authorizer: twoHouseholds{}, Tokens: fakeTokens{}}).Mount)
	rec = httptest.NewRecorder()
	off.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("off = %d", rec.Code)
	}
}

func TestIntegrationBillingAndEntitlementFields(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	if err := client.EnsureIndexes(ctx, append(BillingIndexes(), Indexes()...)...); err != nil {
		t.Fatal(err)
	}
	b := NewMongoBilling(client.Database())
	at := time.Date(2026, 11, 13, 21, 2, 54, 0, time.UTC)
	for _, e := range []BillingEvent{
		{HouseholdID: hh, EventID: "a", Type: "INITIAL_PURCHASE", At: at, Trial: true},
		{HouseholdID: hh, EventID: "a", Type: "INITIAL_PURCHASE", At: at, Trial: true},
		{HouseholdID: hh, EventID: "b", Type: "RENEWAL", At: at.Add(time.Hour), Amount: 4.99, Currency: "USD"},
		{HouseholdID: hhB, EventID: "c", Type: "RENEWAL", At: at},
	} {
		if err := b.Add(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	list, err := b.List(ctx, hh, 10)
	if err != nil || len(list) != 2 || list[0].EventID != "b" || list[0].Amount != 4.99 || !list[1].Trial {
		t.Fatalf("list = %+v %v", list, err)
	}
	if err := b.PurgeHousehold(ctx, hh); err != nil {
		t.Fatal(err)
	}
	if list, _ := b.List(ctx, hh, 10); len(list) != 0 {
		t.Errorf("purged = %+v", list)
	}

	store := NewMongoStore(client.Database())
	if _, err := store.Put(ctx, Entitlement{HouseholdID: hh, LastEventAt: at, Since: at, StartedBy: "u1"}); err != nil {
		t.Fatal(err)
	}
	if e, err := store.Get(ctx, hh); err != nil || !e.Since.Equal(at) || e.StartedBy != "u1" {
		t.Errorf("entitlement = %+v %v", e, err)
	}
}
