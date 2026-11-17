package plus

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// Households for the qualify tests, and their creators.
const (
	hhNew   = "66e5a1f2c3b4a5d6e7f80c01"
	userNew = "66e5a1f2c3b4a5d6e7f80d01"
	userHH  = "66e5a1f2c3b4a5d6e7f80b01" // created hh in referralService
)

var qualifyNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// newReferral makes hhNew, created by userNew on device, and enters hh's
// code from it.
func newReferral(t *testing.T, device string) *harness {
	t.Helper()
	s, _, _, _ := referralService(t, qualifyNow)
	h := harnessOf(s)
	h.at(qualifyNow.Add(-time.Minute))
	h.create(hhNew, "Garcias", userNew, device)
	h.at(qualifyNow)
	if _, err := h.redeem(hhNew, h.code(hh), device); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) noMonths(ids ...string) {
	h.t.Helper()
	for _, id := range ids {
		if until, _ := h.free.FreeUntil(h.t.Context(), id); !until.IsZero() {
			h.t.Errorf("%s got free time until %v", id, until)
		}
	}
}

func (h *harness) sweep() {
	h.t.Helper()
	if _, err := h.s.Sweep(h.t.Context()); err != nil {
		h.t.Fatal(err)
	}
}

// A real household: both get a month, and each notification goes out once,
// however often events and sweeps retry, even at the same time.
func TestReferralQualifiesOnceForARealHousehold(t *testing.T) {
	h := newReferral(t, "device-new")
	ctx := t.Context()
	if h.state(hhNew) != StatePending {
		t.Fatalf("state = %q, want pending", h.state(hhNew))
	}
	h.noMonths(hhNew, hh)
	if len(h.notes.sent) != 0 {
		t.Fatalf("notified before qualifying: %+v", h.notes.sent)
	}

	h.s.DinnersPlanned(ctx, hhNew, 1)
	if h.state(hhNew) != StatePending {
		t.Fatalf("qualified with no recipes")
	}
	h.s.RecipesSaved(ctx, hhNew, 1)
	if h.state(hhNew) != StateQualified {
		t.Fatalf("state = %q, want qualified", h.state(hhNew))
	}

	// Retries, sweeps, and racing events change nothing.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(3)
		go func() { defer wg.Done(); _ = h.s.Evaluate(ctx, hhNew) }()
		go func() { defer wg.Done(); _, _ = h.s.Sweep(ctx) }()
		go func() { defer wg.Done(); h.s.RecipesSaved(ctx, hhNew, 1) }()
	}
	wg.Wait()
	h.sweep()

	for _, id := range []string{hhNew, hh} {
		if until, _ := h.free.FreeUntil(ctx, id); !until.Equal(qualifyNow.Add(month)) {
			t.Errorf("%s free until %v, want one month", id, until)
		}
		if got := h.notes.to(id); len(got) != 1 || !got[0].Banner {
			t.Errorf("%s notifications = %+v, want one banner", id, got)
		}
	}
	if got := h.notes.to(hhNew)[0]; got.Body != "Your free month of Plus is on. Thanks for joining from Lines's invite." {
		t.Errorf("referred body = %q", got.Body)
	}
	if got := h.notes.to(hh)[0]; got.Body != "Garcias joined from your invite. You earned a free month of Plus." {
		t.Errorf("referrer body = %q", got.Body)
	}
	if r, _, _ := h.refs.UsedBy(ctx, hhNew); !r.Settled || r.Checks != ChecksPassed {
		t.Errorf("referral = %+v", r)
	}
}

// A throwaway household made by someone already in a household, by the same
// Apple ID, never qualifies, even after the account is deleted and made
// again (a new user ID, the same Apple subject).
func TestReferralThrowawayFromTheSameAppleIDNeverQualifies(t *testing.T) {
	for name, creator := range map[string]string{
		"same account":       userHH,
		"account made again": "66e5a1f2c3b4a5d6e7f80d99",
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _, _ := referralService(t, qualifyNow)
			h := harnessOf(s)
			h.ids.subjects[creator] = userHH // the hh creator's Apple subject
			h.create(hhNew, "Spare", creator, "device-spare")
			if _, err := h.redeem(hhNew, h.code(hh), "device-spare"); err != nil {
				t.Fatal(err)
			}
			h.setUp(hhNew)
			h.sweep()
			if h.state(hhNew) != StatePending {
				t.Fatalf("state = %q, want pending", h.state(hhNew))
			}
			// The sharer sees plain Pending, nothing about why.
			info, _ := h.s.Referral(t.Context(), hh)
			if state, detail := h.s.rules.InviteDetail(info.Recent[0]); state != "pending" || detail != "" {
				t.Errorf("detail = %q %q", state, detail)
			}
			h.at(qualifyNow.Add(15 * 24 * time.Hour))
			h.sweep()
			if h.state(hhNew) != StateExpired {
				t.Errorf("state = %q, want expired", h.state(hhNew))
			}
			h.noMonths(hhNew, hh)
		})
	}
}

// The same rule holds for Google sign-in identities.
func TestReferralThrowawayFromTheSameGoogleIDNeverQualifies(t *testing.T) {
	s, _, _, _ := referralService(t, qualifyNow)
	h := harnessOf(s)
	google := &googleIdentities{subject: "google-sub-1"}
	h.s.identities = google
	h.at(qualifyNow.Add(-time.Hour))
	h.join(hhB, "66e5a1f2c3b4a5d6e7f80d77", "device-g")
	h.at(qualifyNow)
	h.create(hhNew, "Spare", "66e5a1f2c3b4a5d6e7f80d78", "device-g2")
	if _, err := h.redeem(hhNew, h.code(hh), "device-g2"); err != nil {
		t.Fatal(err)
	}
	h.setUp(hhNew)
	if h.state(hhNew) != StatePending {
		t.Errorf("state = %q, want pending", h.state(hhNew))
	}
}

// googleIdentities gives every user the same Google identity.
type googleIdentities struct{ subject string }

func (g *googleIdentities) ListIdentities(_ context.Context, userID string) ([]users.AuthIdentity, error) {
	return []users.AuthIdentity{{UserID: userID, Provider: users.ProviderGoogle, Subject: g.subject}}, nil
}

// A device another household tied first never qualifies, though it was
// deleted and reinstalled (DeviceCheck's bit lives at Apple).
func TestReferralFromATiedDeviceNeverQualifies(t *testing.T) {
	h := newReferral(t, "device-hh") // hh was made on this device
	h.setUp(hhNew)
	h.sweep()
	if h.state(hhNew) != StatePending {
		t.Fatalf("state = %q, want pending", h.state(hhNew))
	}
	if tie, _ := h.devices.Get(t.Context(), hhNew); !tie.Elsewhere || tie.Owns {
		t.Errorf("tie = %+v", tie)
	}
	h.at(qualifyNow.Add(15 * 24 * time.Hour))
	h.sweep()
	if h.state(hhNew) != StateExpired {
		t.Errorf("state = %q, want expired", h.state(hhNew))
	}
	h.noMonths(hhNew, hh)
}

// The household that tied a device isn't held back by its own bit.
func TestReferralOwnDeviceBitDoesNotCount(t *testing.T) {
	h := newReferral(t, "device-new")
	// Entering the code on the same device asked again: still its own.
	if tie, _ := h.devices.Get(t.Context(), hhNew); !tie.Owns || tie.Elsewhere {
		t.Errorf("tie = %+v", tie)
	}
	h.setUp(hhNew)
	if h.state(hhNew) != StateQualified {
		t.Errorf("state = %q, want qualified", h.state(hhNew))
	}
}

// Joining a household from a phone that has its own is normal.
func TestReferralJoiningFromATiedDeviceIsFine(t *testing.T) {
	h := newReferral(t, "device-new")
	h.join(hhNew, "66e5a1f2c3b4a5d6e7f80d55", "device-hhB")
	h.setUp(hhNew)
	if h.state(hhNew) != StateQualified {
		t.Errorf("state = %q, want qualified", h.state(hhNew))
	}
}

// A household that never plans a dinner expires at 14 days with no months.
func TestReferralWithoutADinnerExpires(t *testing.T) {
	h := newReferral(t, "device-new")
	h.s.RecipesSaved(t.Context(), hhNew, 5)
	h.at(qualifyNow.Add(13 * 24 * time.Hour))
	h.sweep()
	if h.state(hhNew) != StatePending {
		t.Fatalf("expired early: %q", h.state(hhNew))
	}
	p, _ := h.s.ReferralProgress(t.Context(), hhNew)
	if p == nil || p.State != "pending" || p.Message != "Plan a dinner to unlock your free month" ||
		!p.Steps[0].Done || !p.Steps[1].Done || p.Steps[2].Done || !p.ExpiresAt.Equal(qualifyNow.Add(14*24*time.Hour)) ||
		p.Summary != "Free month: 2 of 3 done" || p.TimeLeft != "1 day left" {
		t.Errorf("progress = %+v", p)
	}
	h.at(qualifyNow.Add(14*24*time.Hour + time.Minute))
	h.sweep()
	if h.state(hhNew) != StateExpired {
		t.Fatalf("state = %q, want expired", h.state(hhNew))
	}
	// A dinner after the window changes nothing.
	h.s.DinnersPlanned(t.Context(), hhNew, 1)
	h.sweep()
	if h.state(hhNew) != StateExpired {
		t.Errorf("state = %q after a late dinner", h.state(hhNew))
	}
	h.noMonths(hhNew, hh)
	if p, _ := h.s.ReferralProgress(t.Context(), hhNew); p.State != "expired" || p.Message != ProgressExpired {
		t.Errorf("progress = %+v", p)
	}
	if len(h.notes.sent) != 0 {
		t.Errorf("notified: %+v", h.notes.sent)
	}
}

// Setup met with no event to trigger it (counts written directly) is
// qualified by the next sweep.
func TestReferralSweepQualifiesWithoutAnEvent(t *testing.T) {
	h := newReferral(t, "device-new")
	_ = h.setup.Add(t.Context(), hhNew, 1, 3)
	if h.state(hhNew) != StatePending {
		t.Fatalf("state = %q", h.state(hhNew))
	}
	h.at(qualifyNow.Add(time.Hour))
	h.sweep()
	if h.state(hhNew) != StateQualified {
		t.Errorf("state = %q, want qualified", h.state(hhNew))
	}
}

// A fourth referral in 30 days waits, shown as pending, past its own 14
// days, and qualifies when the window frees.
func TestReferralFourthIn30DaysWaitsForTheWindow(t *testing.T) {
	s, _, _, _ := referralService(t, qualifyNow)
	h := harnessOf(s)
	code := h.code(hh)
	ids := []string{"66e5a1f2c3b4a5d6e7f80c11", "66e5a1f2c3b4a5d6e7f80c12", "66e5a1f2c3b4a5d6e7f80c13", "66e5a1f2c3b4a5d6e7f80c14"}
	for i, id := range ids {
		h.at(qualifyNow.Add(time.Duration(i) * time.Hour))
		h.create(id, "Household "+id[22:], "66e5a1f2c3b4a5d6e7f80e"+id[22:], "device-"+id)
		if _, err := h.redeem(id, code, "device-"+id); err != nil {
			t.Fatal(err)
		}
		h.setUp(id)
	}
	for _, id := range ids[:3] {
		if h.state(id) != StateQualified {
			t.Errorf("%s = %q, want qualified", id, h.state(id))
		}
	}
	last := ids[3]
	if h.state(last) != StateReady {
		t.Fatalf("fourth = %q, want ready", h.state(last))
	}
	h.noMonths(last)
	info, _ := h.s.Referral(t.Context(), hh)
	if state, detail := h.s.rules.InviteDetail(info.Recent[0]); state != "pending" || detail != InviteCountsSoon {
		t.Errorf("fourth shows %q %q", state, detail)
	}
	if p, _ := h.s.ReferralProgress(t.Context(), last); p.State != "pending" || p.Message != ProgressWaiting || !p.ExpiresAt.IsZero() {
		t.Errorf("fourth progress = %+v", p)
	}

	// Past its 14 days it still waits; 30 days after the first, it counts.
	h.at(qualifyNow.Add(20 * 24 * time.Hour))
	h.sweep()
	if h.state(last) != StateReady {
		t.Fatalf("fourth at 20 days = %q, want ready", h.state(last))
	}
	h.at(qualifyNow.Add(30*24*time.Hour + time.Minute))
	h.sweep()
	if h.state(last) != StateQualified {
		t.Fatalf("fourth at 30 days = %q, want qualified", h.state(last))
	}
	if until, _ := h.free.FreeUntil(t.Context(), last); until.IsZero() {
		t.Error("fourth got no month")
	}
	// Four months back to back from the first.
	if until, _ := h.free.FreeUntil(t.Context(), hh); !until.Equal(qualifyNow.Add(4 * month)) {
		t.Errorf("sharer free until %v, want %v", until, qualifyNow.Add(4*month))
	}
}

// A sharer that has earned every month: the new household still gets its
// month, the sharer gets the "most free months" words.
func TestReferralAtTheCapStillPaysTheNewHousehold(t *testing.T) {
	s, _, _, refs := referralService(t, qualifyNow)
	h := harnessOf(s)
	for i := range MaxReferralMonths {
		refs.refs = append(refs.refs, Referral{
			ID: "old" + string(rune('a'+i)), ReferrerID: hh, ReferredID: "x" + string(rune('a'+i)),
			At: qualifyNow.Add(-time.Duration(60+i) * 24 * time.Hour), ReferrerRewarded: true,
		})
	}
	h.create(hhNew, "Garcias", userNew, "device-new")
	if _, err := h.redeem(hhNew, h.code(hh), "device-new"); err != nil {
		t.Fatal(err)
	}
	h.setUp(hhNew)
	if h.state(hhNew) != StateQualified {
		t.Fatalf("state = %q", h.state(hhNew))
	}
	if until, _ := h.free.FreeUntil(t.Context(), hhNew); !until.Equal(qualifyNow.Add(month)) {
		t.Errorf("new household free until %v", until)
	}
	h.noMonths(hh)
	got := h.notes.to(hh)
	if len(got) != 1 || got[0].Body != "Garcias joined from your invite. You've already earned the most free months." {
		t.Errorf("sharer notified %+v", got)
	}
	info, _ := h.s.Referral(t.Context(), hh)
	if _, detail := h.s.rules.InviteDetail(info.Recent[0]); detail != InviteEarnedAtCap {
		t.Errorf("detail = %q", detail)
	}
}

// A household that shares a member with the sharer's, now or before, never
// qualifies.
func TestReferralWithASharedMemberNeverQualifies(t *testing.T) {
	t.Run("current", func(t *testing.T) {
		h := newReferral(t, "device-new")
		h.join(hhNew, userHH, "") // the sharer's own creator joins
		h.setUp(hhNew)
		h.sweep()
		if h.state(hhNew) != StatePending {
			t.Errorf("state = %q, want pending", h.state(hhNew))
		}
		h.noMonths(hhNew, hh)
	})
	t.Run("past", func(t *testing.T) {
		h := newReferral(t, "device-new")
		// Someone who was in hh and left (memberships go, history stays).
		past := "66e5a1f2c3b4a5d6e7f80d44"
		_ = h.history.Add(t.Context(), []MembershipRecord{{Identity: IdentityKey(users.ProviderApple, past), HouseholdID: hh, UserID: past, At: qualifyNow.Add(-90 * 24 * time.Hour)}})
		h.join(hhNew, past, "")
		h.setUp(hhNew)
		h.sweep()
		if h.state(hhNew) != StatePending {
			t.Errorf("state = %q, want pending", h.state(hhNew))
		}
		h.noMonths(hhNew, hh)
	})
}

// In production with no DeviceCheck key, nothing can be checked, so a
// referral stays pending (and expires); in development a request without a
// token counts as a new device.
func TestReferralWithoutDeviceCheck(t *testing.T) {
	t.Run("production, no key", func(t *testing.T) {
		s, _, _, _ := referralService(t, qualifyNow)
		h := harnessOf(s)
		h.s.deviceCheck = nil
		h.create(hhNew, "Garcias", userNew, "device-new")
		if _, err := h.redeem(hhNew, h.code(hh), "device-new"); err != nil {
			t.Fatal(err)
		}
		h.setUp(hhNew)
		h.sweep()
		if h.state(hhNew) != StatePending {
			t.Errorf("state = %q, want pending", h.state(hhNew))
		}
		h.noMonths(hhNew, hh)
	})
	t.Run("production, no token", func(t *testing.T) {
		h := newReferral(t, "")
		h.setUp(hhNew)
		if h.state(hhNew) != StatePending {
			t.Errorf("state = %q, want pending", h.state(hhNew))
		}
	})
	t.Run("development, no token", func(t *testing.T) {
		s, _, _, _ := referralService(t, qualifyNow)
		h := harnessOf(s)
		h.s.deviceCheck, h.s.allowWithoutToken = nil, true
		h.create(hhNew, "Garcias", userNew, "")
		if _, err := h.redeem(hhNew, h.code(hh), ""); err != nil {
			t.Fatal(err)
		}
		h.setUp(hhNew)
		if h.state(hhNew) != StateQualified {
			t.Errorf("state = %q, want qualified", h.state(hhNew))
		}
	})
}

// A DeviceCheck outage leaves the referral unchecked, not blocked.
func TestReferralDeviceCheckDown(t *testing.T) {
	s, _, _, _ := referralService(t, qualifyNow)
	h := harnessOf(s)
	h.dc.Err = context.DeadlineExceeded
	h.create(hhNew, "Garcias", userNew, "device-new")
	if _, err := h.redeem(hhNew, h.code(hh), "device-new"); err != nil {
		t.Fatal(err)
	}
	h.setUp(hhNew)
	if r, _, _ := h.refs.UsedBy(t.Context(), hhNew); r.State != StatePending || r.Checks != ChecksUnknown {
		t.Errorf("referral = %+v", r)
	}
	// Back up: entering nothing new, a later join from a fresh device ties it.
	h.dc.Err = nil
	h.join(hhNew, "66e5a1f2c3b4a5d6e7f80d66", "device-partner")
	h.sweep()
	if h.state(hhNew) != StateQualified {
		t.Errorf("state = %q, want qualified", h.state(hhNew))
	}
}

func TestReferralHowItWorksFollowsTheRules(t *testing.T) {
	how := DefaultReferralRules().HowItWorks()
	want := []string{
		"Share your code with friends, family, even that one aunt.",
		"They download DinnerOS, start a new household, and enter your code.",
		"Once they plan a dinner and add a recipe, the invite counts.",
		"Within 24 hours you both get a free month of Plus. We'll send you a notification when it's on.",
	}
	if how.Title != "How it works" || strings.Join(how.Steps, "|") != strings.Join(want, "|") {
		t.Errorf("how it works = %+v", how)
	}
	r := ReferralRules{QualifyDays: 7, MinDinners: 2, MinRecipes: 1, MaxPer30Days: 3}
	if got := r.HowItWorks().Steps[2]; got != "Once they plan 2 dinners and add a recipe, the invite counts." {
		t.Errorf("2 dinners, 1 recipe = %q", got)
	}
	if got := r.SummaryFooter(); !strings.Contains(got, "plan 2 dinners and add a recipe") || !strings.Contains(got, "7 days") {
		t.Errorf("footer = %q", got)
	}
	p := r.ProgressFor(Referral{State: StatePending}, Setup{})
	if p.Message != "Add a recipe and plan 2 dinners to unlock your free month" || p.Steps[0].Title != "Create your household" ||
		!p.Steps[0].Done || p.Steps[1].Title != "Add a recipe: write your own, import one from a website, or pick one from Shared Recipes." ||
		p.Steps[2].Title != "Plan 2 dinners" || p.Steps[2].Target != 2 || p.Summary != "Free month: 1 of 3 done" {
		t.Errorf("progress = %+v", p)
	}
	// The default is one recipe (decision 706).
	if p := DefaultReferralRules().ProgressFor(Referral{State: StatePending}, Setup{}); p.Message != "Add a recipe and plan a dinner to unlock your free month" ||
		p.Steps[1].Count != 0 || p.Steps[1].Target != 1 || p.Steps[1].Done {
		t.Errorf("default progress = %+v", p)
	}
	three := ReferralRules{QualifyDays: 14, MinDinners: 1, MinRecipes: 3, MaxPer30Days: 3}
	if p := three.ProgressFor(Referral{State: StatePending}, Setup{Recipes: 2}); p.Message != "Add 3 recipes and plan a dinner to unlock your free month" ||
		p.Steps[1].Title != "Add 3 recipes: write your own, import them from a website, or pick them from Shared Recipes." ||
		p.Steps[1].Count != 2 || p.Steps[1].Target != 3 || p.Steps[1].Done {
		t.Errorf("3 recipes progress = %+v", p)
	}
	if p := three.ProgressFor(Referral{State: StatePending}, Setup{Dinners: 1}); p.Message != "Add 3 recipes to unlock your free month" {
		t.Errorf("only recipes left = %q", p.Message)
	}
}

// The checklist says what Plus is, in a title and a few lines (decision 708).
func TestReferralProgressExplainsPlus(t *testing.T) {
	for _, state := range []ReferralState{StatePending, StateQualified, StateExpired} {
		p := DefaultReferralRules().ProgressFor(Referral{State: state}, Setup{})
		if p.PlusTitle != "What is DinnerOS Plus?" || len(p.PlusLines) < 2 || len(p.PlusLines) > 3 {
			t.Errorf("%s: plus = %q %q", state, p.PlusTitle, p.PlusLines)
		}
		joined := strings.Join(p.PlusLines, " ")
		for _, want := range []string{"Autopilot", "costs", "saved"} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s: plus lines %q don't mention %q", state, joined, want)
			}
		}
	}
	// Each progress gets its own copy of the lines.
	p := DefaultReferralRules().ProgressFor(Referral{State: StatePending}, Setup{})
	p.PlusLines[0] = "changed"
	if PlusExplainerLines[0] == "changed" {
		t.Error("changing a progress changed the shared lines")
	}
}

func TestReferralInviteDetails(t *testing.T) {
	rules := DefaultReferralRules()
	pending := func(setup Setup) InviteItem {
		return InviteItem{Referral: Referral{State: StatePending, Checks: ChecksPassed}, Setup: setup}
	}
	for name, tc := range map[string]struct {
		item  InviteItem
		state string
		want  string
	}{
		"nothing yet":     {pending(Setup{}), "pending", "Needs a dinner planned and a recipe"},
		"a dinner":        {pending(Setup{Dinners: 1}), "pending", "Needs a recipe"},
		"recipes":         {pending(Setup{Recipes: 1}), "pending", "Needs a dinner planned"},
		"one recipe left": {pending(Setup{Dinners: 1, Recipes: 0}), "pending", "Needs a recipe"},
		"set up":          {InviteItem{Referral: Referral{State: StateReady, Checks: ChecksPassed, SetupMetAt: qualifyNow}}, "pending", InviteCountsSoon},
		"blocked":         {InviteItem{Referral: Referral{State: StatePending, Checks: ChecksBlocked}}, "pending", ""},
		"unchecked":       {InviteItem{Referral: Referral{State: StatePending}}, "pending", ""},
		"earned":          {InviteItem{Referral: Referral{State: StateQualified, ReferrerRewarded: true}}, "qualified", InviteEarned},
		"at the cap":      {InviteItem{Referral: Referral{State: StateQualified}}, "qualified", InviteEarnedAtCap},
		"expired":         {InviteItem{Referral: Referral{State: StateExpired}}, "expired", InviteExpired},
	} {
		if state, got := rules.InviteDetail(tc.item); state != tc.state || got != tc.want {
			t.Errorf("%s = %q %q, want %q %q", name, state, got, tc.state, tc.want)
		}
	}
	two := ReferralRules{MinDinners: 2, MinRecipes: 3}
	if _, got := two.InviteDetail(pending(Setup{Dinners: 2, Recipes: 1})); got != "Needs 2 more recipes" {
		t.Errorf("2 of 3 recipes left = %q", got)
	}
	if _, got := two.InviteDetail(pending(Setup{Dinners: 2, Recipes: 2})); got != "Needs 1 more recipe" {
		t.Errorf("1 of 3 recipes left = %q", got)
	}
	if _, got := two.InviteDetail(pending(Setup{Recipes: 3})); got != "Needs 2 dinners planned" {
		t.Errorf("2 dinners = %q", got)
	}
	if _, got := two.InviteDetail(pending(Setup{Dinners: 1, Recipes: 3})); got != "Needs a dinner planned" {
		t.Errorf("1 of 2 dinners = %q", got)
	}
}

// Every word a person sees is plain: no em dashes, nothing internal.
func TestReferralCopyIsPlain(t *testing.T) {
	rules := DefaultReferralRules()
	texts := append([]string{
		rules.SummaryFooter(), ReferredTitle, ReferrerTitle, ReferredBody("Lines"), ReferredBody(""),
		ReferrerBody("Garcias", false), ReferrerBody("", true), ProgressWaiting, ProgressQualified, ProgressExpired,
		InviteCountsSoon, InviteEarned, InviteEarnedAtCap, InviteExpired, InviteStatusPending,
	}, rules.HowItWorks().Steps...)
	raw, _ := json.Marshal(rules.ProgressFor(Referral{State: StatePending}, Setup{}))
	texts = append(texts, string(raw), PlusExplainerTitle)
	texts = append(texts, PlusExplainerLines...)
	for _, s := range texts {
		for _, bad := range []string{"—", "referral", "household_id", "DeviceCheck", "qualif"} {
			if strings.Contains(strings.ToLower(s), strings.ToLower(bad)) && !strings.Contains(s, `"state"`) {
				t.Errorf("%q contains %q", s, bad)
			}
		}
	}
}

// Recording memberships keeps the earliest time, and a backfill is safe to
// run again.
func TestReferralHistoryBackfill(t *testing.T) {
	s, _, _, _ := referralService(t, qualifyNow)
	h := harnessOf(s)
	members := func(yield func(string, string, time.Time) bool) error {
		yield(hhB, userHH, qualifyNow.Add(-100*24*time.Hour))
		yield(hhB, userHH, qualifyNow)
		return nil
	}
	for range 2 {
		if n, err := h.s.BackfillHistory(t.Context(), members); err != nil || n != 2 {
			t.Fatalf("backfill = %d %v", n, err)
		}
	}
	recs, _ := h.history.ForHousehold(t.Context(), hhB)
	var mine []MembershipRecord
	for _, r := range recs {
		if r.UserID == userHH {
			mine = append(mine, r)
		}
	}
	if len(mine) != 1 || !mine[0].At.Equal(qualifyNow.Add(-100*24*time.Hour)) {
		t.Errorf("records = %+v", mine)
	}
	if IdentityKey(users.ProviderApple, "x") == IdentityKey(users.ProviderGoogle, "x") || strings.Contains(IdentityKey(users.ProviderApple, "abc"), "abc") {
		t.Error("identity keys must differ by provider and hide the subject")
	}
}

var _ households.MemberListener = (*Service)(nil)
