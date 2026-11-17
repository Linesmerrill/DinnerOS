package plus

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func mongoOfferCodes(t *testing.T) (*MongoOfferCodes, *mongodb.Client) {
	t.Helper()
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(t.Context(), OfferCodeIndexes()...); err != nil {
		t.Fatal(err)
	}
	return NewMongoOfferCodes(client.Database()), client
}

func householdHex(i int) string { return fmt.Sprintf("66e5a1f2c3b4a5d6e7f9%04x", i) }

// Many months racing for fewer codes: each code goes to one month, each
// month gets at most one, and the rest are owed.
func TestIntegrationOfferCodeTakeIsAtomic(t *testing.T) {
	store, _ := mongoOfferCodes(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	var codes []string
	for i := range 20 {
		codes = append(codes, fmt.Sprintf("RACE%08d", i))
	}
	if res, err := store.Import(ctx, codes, OfferBatch{Offer: "o", Batch: "b", ExpiresAt: now.Add(90 * 24 * time.Hour)}, now); err != nil || res.Added != 20 {
		t.Fatalf("import = %+v, %v", res, err)
	}
	var months []FreeMonth
	for i := range 30 {
		m, err := store.AddMonth(ctx, FreeMonth{HouseholdID: householdHex(i), ReferralID: fmt.Sprintf("ref%d", i), Role: RoleReferred, EarnedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		months = append(months, m)
	}
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		given int
	)
	// Every month asks three times at once.
	for _, m := range months {
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := store.Give(ctx, m.ID, now, now.Add(CodeMinLife))
				if err != nil {
					t.Error(err)
					return
				}
				if ok {
					mu.Lock()
					given++
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	ready, owed := 0, 0
	seen := map[string]bool{}
	for i := range 30 {
		ms, _ := store.Months(ctx, householdHex(i))
		if len(ms) != 1 {
			t.Fatalf("household %d has %d months", i, len(ms))
		}
		switch ms[0].State {
		case MonthReady:
			ready++
			code, _, err := store.Reveal(ctx, householdHex(i), ms[0].ID, now)
			if err != nil || code == "" || seen[code] {
				t.Errorf("household %d code %q (seen %v): %v", i, code, seen[code], err)
			}
			seen[code] = true
		case MonthOwed:
			owed++
		}
	}
	if ready != 20 || owed != 10 {
		t.Errorf("ready %d owed %d, want 20 and 10", ready, owed)
	}
	if st, _ := store.Stock(ctx, now); st.Available != 0 || st.Given != 20 || st.Revealed != 20 || st.Owed != 10 {
		t.Errorf("stock = %+v", st)
	}
	// Recording the same month again returns the stored one.
	again, _ := store.AddMonth(ctx, FreeMonth{HouseholdID: householdHex(0), ReferralID: "ref0", Role: RoleReferred, EarnedAt: now.Add(time.Hour)})
	if again.ID != months[0].ID {
		t.Errorf("a month was recorded twice")
	}
	// A shortage alerts once until codes come in.
	if ok, _ := store.ClaimShortageAlert(ctx, now); !ok {
		t.Error("first alert not claimed")
	}
	if ok, _ := store.ClaimShortageAlert(ctx, now); ok {
		t.Error("second alert claimed")
	}
	// The owed months fill from the next import; the old codes are dupes.
	res, err := store.Import(ctx, append(codes[:5:5], "RACENEW00001", "RACENEW00002"), OfferBatch{Offer: "o", Batch: "c", ExpiresAt: now.Add(90 * 24 * time.Hour)}, now)
	if err != nil || res.Added != 2 || res.Duplicates != 5 {
		t.Errorf("second import = %+v, %v", res, err)
	}
	if ok, _ := store.ClaimShortageAlert(ctx, now); !ok {
		t.Error("an import didn't reset the alert")
	}
	owedNow, _ := store.Owed(ctx, 100)
	for _, m := range owedNow[:2] {
		if ok, err := store.Give(ctx, m.ID, now, now.Add(CodeMinLife)); !ok || err != nil {
			t.Errorf("fill = %v %v", ok, err)
		}
	}
	if st, _ := store.Stock(ctx, now); st.Owed != 8 || st.Available != 0 {
		t.Errorf("after fill = %+v", st)
	}
}

// A code too close to its end is never given; one that ran out is swapped.
func TestIntegrationOfferCodeExpiry(t *testing.T) {
	store, _ := mongoOfferCodes(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	_, _ = store.Import(ctx, []string{"SOON00000001"}, OfferBatch{Offer: "o", Batch: "soon", ExpiresAt: now.Add(3 * 24 * time.Hour)}, now)
	m, _ := store.AddMonth(ctx, FreeMonth{HouseholdID: hh, ReferralID: "r", Role: RoleReferred, EarnedAt: now})
	if ok, _ := store.Give(ctx, m.ID, now, now.Add(CodeMinLife)); ok {
		t.Fatal("gave a code that ends in 3 days")
	}
	if st, _ := store.Stock(ctx, now); st.Available != 0 || st.Expired != 1 {
		t.Errorf("stock = %+v", st)
	}
	_, _ = store.Import(ctx, []string{"LATE00000001"}, OfferBatch{Offer: "o", Batch: "late", ExpiresAt: now.Add(20 * 24 * time.Hour)}, now)
	if ok, _ := store.Give(ctx, m.ID, now, now.Add(CodeMinLife)); !ok {
		t.Fatal("no code given")
	}
	later := now.Add(21 * 24 * time.Hour)
	exp, _ := store.ExpiredGiven(ctx, later, 10)
	if len(exp) != 1 || exp[0].ID != m.ID {
		t.Fatalf("expired = %+v", exp)
	}
	if err := store.Requeue(ctx, m.ID, later); err != nil {
		t.Fatal(err)
	}
	if ms, _ := store.Months(ctx, hh); ms[0].State != MonthOwed || !ms[0].CodeExpiresAt.IsZero() {
		t.Errorf("requeued = %+v", ms[0])
	}
	_, _ = store.Import(ctx, []string{"NEXT00000001"}, OfferBatch{Offer: "o", Batch: "next", ExpiresAt: later.Add(90 * 24 * time.Hour)}, later)
	if ok, _ := store.Give(ctx, m.ID, later, later.Add(CodeMinLife)); !ok {
		t.Fatal("no fresh code")
	}
	if code, _, _ := store.Reveal(ctx, hh, m.ID, later); code != "NEXT00000001" {
		t.Errorf("code = %q", code)
	}
}

// Reveal is for the month's own household; redeeming forgets the code's
// text and keeps its hash.
func TestIntegrationOfferCodeRevealAndRedeem(t *testing.T) {
	store, client := mongoOfferCodes(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	_, _ = store.Import(ctx, []string{"MINE00000001", "MINE00000002"}, OfferBatch{Offer: "o", Batch: "b", ExpiresAt: now.Add(90 * 24 * time.Hour)}, now)
	m, _ := store.AddMonth(ctx, FreeMonth{HouseholdID: hh, ReferralID: "r", Role: RoleReferred, EarnedAt: now})
	if _, _, err := store.Reveal(ctx, hh, m.ID, now); err != ErrFreeMonthOwed {
		t.Errorf("owed reveal = %v", err)
	}
	_, _ = store.Give(ctx, m.ID, now, now.Add(CodeMinLife))
	if _, _, err := store.Reveal(ctx, hhB, m.ID, now); err != ErrFreeMonthNotFound {
		t.Errorf("other household reveal = %v", err)
	}
	if ok, _ := store.MarkRedeemed(ctx, hh, now); ok {
		t.Error("marked an unrevealed month redeemed")
	}
	code, got, err := store.Reveal(ctx, hh, m.ID, now)
	if err != nil || code != "MINE00000001" || got.RevealedAt.IsZero() {
		t.Fatalf("reveal = %q %+v %v", code, got, err)
	}
	// Revealing again keeps the first time.
	if _, got, _ := store.Reveal(ctx, hh, m.ID, now.Add(time.Hour)); !got.RevealedAt.Equal(now) {
		t.Errorf("revealed at = %v", got.RevealedAt)
	}
	if ok, err := store.MarkRedeemed(ctx, hh, now.Add(2*time.Hour)); !ok || err != nil {
		t.Fatalf("redeemed = %v %v", ok, err)
	}
	if _, _, err := store.Reveal(ctx, hh, m.ID, now); err != ErrFreeMonthRedeemed {
		t.Errorf("reveal after redeem = %v", err)
	}
	var doc bson.M
	_ = client.Database().Collection(OfferCodesCollection).FindOne(ctx, bson.D{{Key: "key", Value: OfferCodeKey("MINE00000001")}}).Decode(&doc)
	if _, has := doc["code"]; has || doc["state"] != codeRedeemed {
		t.Errorf("redeemed code doc = %v", doc)
	}
	// Its hash stops the same download bringing it back.
	if res, _ := store.Import(ctx, []string{"MINE00000001"}, OfferBatch{Offer: "o", Batch: "b2", ExpiresAt: now.Add(90 * 24 * time.Hour)}, now); res.Added != 0 {
		t.Errorf("a redeemed code was imported again")
	}
}

// Deleting a household: a code it never saw goes back to the pool, one it
// saw keeps only its hash, and its months go.
func TestIntegrationOfferCodePurge(t *testing.T) {
	store, client := mongoOfferCodes(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	_, _ = store.Import(ctx, []string{"PURGE0000001", "PURGE0000002", "PURGE0000003"}, OfferBatch{Offer: "o", Batch: "b", ExpiresAt: now.Add(90 * 24 * time.Hour)}, now)
	seen, _ := store.AddMonth(ctx, FreeMonth{HouseholdID: hh, ReferralID: "r1", Role: RoleReferred, EarnedAt: now})
	unseen, _ := store.AddMonth(ctx, FreeMonth{HouseholdID: hh, ReferralID: "r2", Role: RoleReferrer, EarnedAt: now.Add(time.Minute)})
	other, _ := store.AddMonth(ctx, FreeMonth{HouseholdID: hhB, ReferralID: "r1", Role: RoleReferrer, EarnedAt: now})
	for _, m := range []FreeMonth{seen, unseen, other} {
		if ok, _ := store.Give(ctx, m.ID, now, now.Add(CodeMinLife)); !ok {
			t.Fatal("no code")
		}
	}
	seenCode, _, _ := store.Reveal(ctx, hh, seen.ID, now)
	if err := store.PurgeHousehold(ctx, hh); err != nil {
		t.Fatal(err)
	}
	if ms, _ := store.Months(ctx, hh); len(ms) != 0 {
		t.Errorf("months left: %+v", ms)
	}
	if ms, _ := store.Months(ctx, hhB); len(ms) != 1 || ms[0].State != MonthReady {
		t.Errorf("the other household's month = %+v", ms)
	}
	if st, _ := store.Stock(ctx, now); st.Available != 1 || st.Given != 1 {
		t.Errorf("stock = %+v, want the unseen code back", st)
	}
	coll := client.Database().Collection(OfferCodesCollection)
	hid, _ := bson.ObjectIDFromHex(hh)
	if n, _ := coll.CountDocuments(ctx, bson.D{{Key: "householdId", Value: hid}}); n != 0 {
		t.Errorf("%d codes still name the household", n)
	}
	if n, _ := coll.CountDocuments(ctx, bson.D{{Key: "code", Value: seenCode}}); n != 0 {
		t.Error("the seen code's text is kept")
	}
	// The code that went back is given to the next month.
	next, _ := store.AddMonth(ctx, FreeMonth{HouseholdID: hhC, ReferralID: "r3", Role: RoleReferred, EarnedAt: now})
	if ok, _ := store.Give(ctx, next.ID, now, now.Add(CodeMinLife)); !ok {
		t.Error("the returned code wasn't given again")
	}
}
