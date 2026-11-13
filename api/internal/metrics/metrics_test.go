package metrics

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
)

func TestSummarize(t *testing.T) {
	mon := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC) // 2026-W46
	sent := string(events.TypeShoppingHandoffCreated)
	rows := []Row{
		{HouseholdID: "a", Type: "recipe.viewed", OccurredAt: mon},
		{HouseholdID: "a", Type: sent, OccurredAt: mon.Add(time.Hour), Provider: "walmart", EstimatedCents: 8000},
		{HouseholdID: "a", Type: sent, OccurredAt: mon.Add(2 * time.Hour), Provider: "walmart", EstimatedCents: 4000},
		{HouseholdID: "b", Type: sent, OccurredAt: mon.Add(3 * time.Hour), Provider: "instacart"},
		{HouseholdID: "c", Type: "recipe.cooked", OccurredAt: mon.AddDate(0, 0, 7)},
	}
	got := Summarize(rows)
	if len(got) != 2 || got[0].Week != "2026-W46" || got[1].Week != "2026-W47" {
		t.Fatalf("weeks = %+v", got)
	}
	w := got[0]
	if w.ActiveHouseholds != 2 || w.CartsSent != 3 {
		t.Errorf("week 46 = %+v", w)
	}
	if s := w.Stores["walmart"]; s.Carts != 2 || s.PricedCarts != 2 || s.AverageCents() != 6000 {
		t.Errorf("walmart = %+v avg %d", s, s.AverageCents())
	}
	if s := w.Stores["instacart"]; s.Carts != 1 || s.PricedCarts != 0 || s.AverageCents() != 0 {
		t.Errorf("instacart = %+v", s)
	}
	if got[1].ActiveHouseholds != 1 || got[1].CartsSent != 0 {
		t.Errorf("week 47 = %+v", got[1])
	}
}

func TestIntegrationWeeksReadsEvents(t *testing.T) {
	client := mongotest.Client(t)
	db := client.Database()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) // Friday of 2026-W46
	hh := bson.NewObjectID()
	docs := []any{
		bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "householdId", Value: hh}, {Key: "type", Value: string(events.TypeShoppingHandoffCreated)},
			{Key: "occurredAt", Value: now.Add(-time.Hour)},
			{Key: "payload", Value: bson.D{{Key: "provider", Value: "walmart"}, {Key: "estimatedCents", Value: int64(5000)}, {Key: "pricedLines", Value: 4}}}},
		// Outside the window: three weeks before the first reported week.
		bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "householdId", Value: hh}, {Key: "type", Value: "recipe.viewed"},
			{Key: "occurredAt", Value: now.AddDate(0, 0, -30)}},
	}
	if _, err := db.Collection(events.Collection).InsertMany(t.Context(), docs); err != nil {
		t.Fatal(err)
	}
	weeks, err := Weeks(t.Context(), db, now, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 1 || weeks[0].Week != "2026-W46" || weeks[0].ActiveHouseholds != 1 ||
		weeks[0].Stores["walmart"].AverageCents() != 5000 {
		t.Errorf("weeks = %+v", weeks)
	}
}

func TestIntegrationPlusNow(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	db := client.Database()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	id := func(n byte) bson.ObjectID { var o bson.ObjectID; o[11] = n; return o }
	later, earlier := now.Add(24*time.Hour), now.Add(-24*time.Hour)
	for _, d := range []bson.D{
		{{Key: "householdId", Value: id(1)}, {Key: "trial", Value: false}, {Key: "expiresAt", Value: later}},
		{{Key: "householdId", Value: id(2)}, {Key: "trial", Value: true}, {Key: "expiresAt", Value: later}},
		{{Key: "householdId", Value: id(3)}, {Key: "trial", Value: false}, {Key: "expiresAt", Value: earlier}},
		{{Key: "householdId", Value: id(4)}, {Key: "ended", Value: true}, {Key: "expiresAt", Value: later}},
	} {
		if _, err := db.Collection(plus.Collection).InsertOne(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []bson.D{
		{{Key: "householdId", Value: id(1)}, {Key: "until", Value: later}}, // also paying: counted once
		{{Key: "householdId", Value: id(5)}, {Key: "until", Value: later}},
		{{Key: "householdId", Value: id(6)}, {Key: "until", Value: earlier}},
	} {
		if _, err := db.Collection(plus.FreeTimeCollection).InsertOne(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	for _, at := range []time.Time{now, now.Add(-time.Hour), now.AddDate(0, 0, -7), now.AddDate(0, 0, -60)} {
		if _, err := db.Collection(plus.ReferralsCollection).InsertOne(ctx, bson.D{{Key: "at", Value: at}}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := PlusNow(ctx, db, now, now.AddDate(0, 0, -14))
	if err != nil {
		t.Fatal(err)
	}
	if p.Paying != 1 || p.Trial != 1 || p.FreeMonths != 1 {
		t.Errorf("plus = %+v", p)
	}
	if p.Referrals["2026-W47"] != 2 || p.Referrals["2026-W46"] != 1 || len(p.Referrals) != 2 {
		t.Errorf("referrals = %v", p.Referrals)
	}
}
