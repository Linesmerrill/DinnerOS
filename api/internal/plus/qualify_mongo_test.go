package plus

import (
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

func TestIntegrationReferralStateMongo(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	if err := client.EnsureIndexes(ctx, append(ReferralIndexes(), QualifyIndexes()...)...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	refs := NewMongoReferrals(db)
	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	// A referral from before decision 698 reads as qualified and paid, and
	// the migration marks it so.
	hhOID, _ := bson.ObjectIDFromHex(hh)
	oldOID, _ := bson.ObjectIDFromHex("66e5a1f2c3b4a5d6e7f80a09")
	if _, err := db.Collection(ReferralsCollection).InsertOne(ctx, bson.D{
		{Key: "referrerId", Value: hhOID}, {Key: "householdId", Value: oldOID}, {Key: "at", Value: at.Add(-10 * 24 * time.Hour)}, {Key: "referrerRewarded", Value: true},
	}); err != nil {
		t.Fatal(err)
	}
	if r, ok, _ := refs.UsedBy(ctx, oldOID.Hex()); !ok || r.State != StateQualified || !r.Settled || !r.EarnedAt().Equal(at.Add(-10*24*time.Hour)) {
		t.Errorf("old referral = %+v", r)
	}
	if n, _ := refs.Rewarded(ctx, hh); n != 1 {
		t.Errorf("rewarded before migration = %d", n)
	}
	if n, _ := refs.QualifiedSince(ctx, hh, at.Add(-30*24*time.Hour)); n != 1 {
		t.Errorf("recent before migration = %d", n)
	}
	if n, err := refs.MigrateReferrals(ctx); err != nil || n != 1 {
		t.Fatalf("migrate = %d %v", n, err)
	}
	if n, _ := refs.MigrateReferrals(ctx); n != 0 {
		t.Errorf("second migrate = %d", n)
	}
	if n, _ := refs.Rewarded(ctx, hh); n != 1 {
		t.Errorf("rewarded after migration = %d", n)
	}
	if n, _ := refs.QualifiedSince(ctx, hh, at.Add(-30*24*time.Hour)); n != 1 {
		t.Errorf("recent after migration = %d", n)
	}
	var raw bson.M
	_ = db.Collection(ReferralsCollection).FindOne(ctx, bson.D{{Key: "householdId", Value: oldOID}}).Decode(&raw)
	if raw["state"] != "qualified" || raw["settled"] != true || raw["referredGranted"] != true || raw["referrerGranted"] != true {
		t.Errorf("migrated = %+v", raw)
	}

	// A new one is pending until moved; every move happens once.
	r := Referral{ReferrerID: hh, ReferredID: hhB, At: at, ExpiresAt: at.Add(14 * 24 * time.Hour)}
	if err := refs.Record(ctx, &r); err != nil || r.ID == "" || r.State != StatePending {
		t.Fatalf("record = %+v %v", r, err)
	}
	if got, _, _ := refs.UsedBy(ctx, hhB); got.State != StatePending || !got.ExpiresAt.Equal(r.ExpiresAt) {
		t.Errorf("pending = %+v", got)
	}
	_ = refs.SetSetupMet(ctx, hhB, at.Add(time.Hour))
	_ = refs.SetSetupMet(ctx, hhB, at.Add(2*time.Hour))
	_ = refs.SetChecks(ctx, hhB, ChecksPassed)
	if got, _, _ := refs.UsedBy(ctx, hhB); !got.SetupMetAt.Equal(at.Add(time.Hour)) || got.Checks != ChecksPassed {
		t.Errorf("setup met = %+v", got)
	}
	if ok, _ := refs.Move(ctx, hhB, []ReferralState{StatePending}, StateReady); !ok {
		t.Error("move to ready")
	}
	if ok, _ := refs.Move(ctx, hhB, []ReferralState{StatePending}, StateExpired); ok {
		t.Error("expired a ready referral")
	}
	counts, _, _ := refs.Joins(ctx, hh, 10)
	if counts.Pending != 1 || counts.Qualified != 1 || counts.Total() != 2 {
		t.Errorf("counts = %+v", counts)
	}
	open, _ := refs.Open(ctx, "", 10)
	if len(open) != 1 || open[0].ReferredID != hhB {
		t.Errorf("open = %+v", open)
	}
	if more, _ := refs.Open(ctx, open[0].ID, 10); len(more) != 0 {
		t.Errorf("open after = %+v", more)
	}
	var wins int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := refs.Qualify(ctx, hhB, at.Add(3*time.Hour), true); ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Errorf("qualified %d times", wins)
	}
	if n, _ := refs.QualifiedSince(ctx, hh, at); n != 1 {
		t.Errorf("qualified since = %d", n)
	}
	if open, _ := refs.Open(ctx, "", 10); len(open) != 1 || open[0].State != StateQualified || open[0].Settled {
		t.Errorf("unsettled = %+v", open)
	}
	for _, flag := range []string{FlagReferredGranted, FlagReferrerGranted, FlagSettled} {
		if ok, _ := refs.Claim(ctx, hhB, flag); !ok {
			t.Errorf("claim %s", flag)
		}
		if ok, _ := refs.Claim(ctx, hhB, flag); ok {
			t.Errorf("claimed %s twice", flag)
		}
	}
	if _, err := refs.Claim(ctx, hhB, "state"); err == nil {
		t.Error("claimed an unknown flag")
	}
	if open, _ := refs.Open(ctx, "", 10); len(open) != 0 {
		t.Errorf("settled still open = %+v", open)
	}
	rep, _ := refs.Report(ctx, at.Add(-time.Hour), 5)
	if rep.Counts.Qualified != 2 || rep.Counts.Pending != 0 {
		t.Errorf("report counts = %+v", rep.Counts)
	}
}

func TestIntegrationQualifyStoresMongo(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	if err := client.EnsureIndexes(ctx, QualifyIndexes()...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	setup := NewMongoSetup(db)
	_ = setup.Add(ctx, hh, 1, 0)
	_ = setup.Add(ctx, hh, 0, 2)
	if s, _ := setup.Get(ctx, hh); s != (Setup{Dinners: 1, Recipes: 2}) {
		t.Errorf("setup = %+v", s)
	}
	if s, _ := setup.Get(ctx, hhB); s != (Setup{}) {
		t.Errorf("empty setup = %+v", s)
	}

	history := NewMongoHistory(db)
	key := IdentityKey(users.ProviderApple, "sub")
	recs := []MembershipRecord{{Identity: key, HouseholdID: hh, UserID: "u1", At: at}}
	_ = history.Add(ctx, recs)
	_ = history.Add(ctx, []MembershipRecord{{Identity: key, HouseholdID: hh, UserID: "u1", At: at.Add(-time.Hour)}, {Identity: key, HouseholdID: hhB, UserID: "u2", At: at}})
	_ = history.Add(ctx, recs) // later again: keeps the earliest
	got, _ := history.ForIdentities(ctx, []string{key})
	if len(got) != 2 {
		t.Fatalf("history = %+v", got)
	}
	if mine, _ := history.ForHousehold(ctx, hh); len(mine) != 1 || !mine[0].At.Equal(at.Add(-time.Hour)) || mine[0].UserID != "u1" {
		t.Errorf("household history = %+v", mine)
	}

	devices := NewMongoDevices(db)
	if tie, _ := devices.Get(ctx, hh); tie != (DeviceTie{}) {
		t.Errorf("no tie = %+v", tie)
	}
	_ = devices.MarkOwned(ctx, hh, at)
	_ = devices.MarkElsewhere(ctx, hh, at)
	if tie, _ := devices.Get(ctx, hh); !tie.Owns || !tie.Elsewhere {
		t.Errorf("tie = %+v", tie)
	}

	locks := NewMongoLocks(db)
	if ok, err := locks.Acquire(ctx, "k", at, at.Add(time.Minute)); !ok || err != nil {
		t.Fatalf("first = %v %v", ok, err)
	}
	if ok, err := locks.Acquire(ctx, "k", at.Add(time.Second), at.Add(time.Minute)); ok || err != nil {
		t.Errorf("held = %v %v", ok, err)
	}
	if ok, _ := locks.Acquire(ctx, "k", at.Add(2*time.Minute), at.Add(3*time.Minute)); !ok {
		t.Error("an expired lock wasn't taken")
	}
	_ = locks.Release(ctx, "k")
	if ok, _ := locks.Acquire(ctx, "k", at.Add(2*time.Minute), at.Add(3*time.Minute)); !ok {
		t.Error("a released lock wasn't taken")
	}

	m := NewMigrations(db)
	if done, _ := m.Done(ctx, "x"); done {
		t.Error("done before")
	}
	_ = m.MarkDone(ctx, "x", at)
	if done, _ := m.Done(ctx, "x"); !done {
		t.Error("not done after")
	}
}

// The whole flow on MongoDB: a real household qualifies once, with events
// and sweeps racing.
func TestIntegrationReferralQualifiesOnceMongo(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	if err := client.EnsureIndexes(ctx, append(append(ReferralIndexes(), QualifyIndexes()...), Indexes()...)...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	homes := &fakeHouseholds{m: map[string]households.Household{
		hh:  {ID: hh, Name: "Lines", CreatedBy: "66e5a1f2c3b4a5d6e7f80b01", CreatedAt: now.Add(-time.Hour)},
		hhB: {ID: hhB, Name: "Parkers", CreatedBy: "66e5a1f2c3b4a5d6e7f80b02", CreatedAt: now.Add(-time.Hour)},
	}}
	notes := &memoryNotifier{}
	free := NewMongoFreeTime(db)
	s := NewService(NewMongoStore(db), true).WithReferrals(free, NewMongoReferrals(db)).WithQualify(QualifyOptions{
		Setup: NewMongoSetup(db), History: NewMongoHistory(db), Devices: NewMongoDevices(db), Locks: NewMongoLocks(db),
		Households: homes, Identities: &fakeIdentities{subjects: map[string]string{}}, Notifier: notes, DeviceCheck: NewMemoryDeviceCheck(),
	})
	s.now = func() time.Time { return now }
	for _, id := range []string{hh, hhB} {
		h := homes.m[id]
		s.MemberAdded(deviceCtx(ctx, "device-"+id), households.MemberAdded{HouseholdID: id, UserID: h.CreatedBy, Created: true, At: h.CreatedAt})
	}
	info, _ := s.Referral(ctx, hh)
	if _, err := s.Redeem(deviceCtx(ctx, "device-"+hhB), hhB, info.Code); err != nil {
		t.Fatal(err)
	}
	s.DinnersPlanned(ctx, hhB, 1)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(2)
		go func() { defer wg.Done(); s.RecipesSaved(ctx, hhB, 1) }()
		go func() { defer wg.Done(); _, _ = s.Sweep(ctx) }()
	}
	wg.Wait()
	_, _ = s.Sweep(ctx)
	for _, id := range []string{hh, hhB} {
		if until, _ := free.FreeUntil(ctx, id); !until.Equal(now.Add(month)) {
			t.Errorf("%s free until %v, want one month", id, until)
		}
		if n := len(notes.to(id)); n != 1 {
			t.Errorf("%s notified %d times", id, n)
		}
	}
	if p, _ := s.ReferralProgress(ctx, hhB); p == nil || p.State != "qualified" {
		t.Errorf("progress = %+v", p)
	}
}
