package push

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newMongoClient(t *testing.T) *mongodb.Client {
	t.Helper()
	client := mongotest.Client(t)
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(context.Background(), append(Indexes(), notifications.Indexes()...)...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	return client
}

func TestIntegrationMongoStoreDeviceTokens(t *testing.T) {
	store := NewMongoStore(newMongoClient(t).Database())
	ctx := context.Background()
	t0 := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	first, err := store.Upsert(ctx, DeviceToken{Token: adaPhone, UserID: userAda, Environment: EnvironmentSandbox, Platform: PlatformIOS, CreatedAt: t0, UpdatedAt: t0})
	if err != nil || first.UserID != userAda || !first.CreatedAt.Equal(t0) {
		t.Fatalf("Upsert() = %+v, %v", first, err)
	}
	t1 := t0.Add(time.Hour)
	moved, err := store.Upsert(ctx, DeviceToken{Token: adaPhone, UserID: userBob, Environment: EnvironmentProduction, Platform: PlatformIOS, CreatedAt: t1, UpdatedAt: t1})
	if err != nil || moved.UserID != userBob || moved.Environment != EnvironmentProduction || !moved.CreatedAt.Equal(t0) || !moved.UpdatedAt.Equal(t1) {
		t.Fatalf("re-Upsert() = %+v, %v; want the token moved with its createdAt kept", moved, err)
	}
	if _, err := store.Upsert(ctx, DeviceToken{Token: adaIPad, UserID: userAda, Environment: EnvironmentSandbox, Platform: PlatformIOS, CreatedAt: t0, UpdatedAt: t0}); err != nil {
		t.Fatal(err)
	}

	// Concurrent first registrations of one token end as one document.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := store.Upsert(ctx, DeviceToken{Token: cyPhone, UserID: userCy, Environment: EnvironmentProduction, Platform: PlatformIOS, CreatedAt: t0, UpdatedAt: t0}); err != nil {
				t.Errorf("concurrent Upsert() error = %v", err)
			}
		})
	}
	wg.Wait()

	list, err := store.ListByUsers(ctx, []string{userAda, userBob, userCy, "not-an-id"})
	if err != nil || len(list) != 3 {
		t.Fatalf("ListByUsers() = %+v, %v", list, err)
	}
	if list, _ := store.ListByUsers(ctx, []string{userAda}); len(list) != 1 || list[0].Token != adaIPad {
		t.Errorf("ListByUsers(ada) = %+v", list)
	}

	if err := store.Delete(ctx, userAda, adaPhone); err != nil { // Bob's now
		t.Fatal(err)
	}
	if list, _ := store.ListByUsers(ctx, []string{userBob}); len(list) != 1 {
		t.Errorf("Delete by another user removed the token: %+v", list)
	}
	if err := store.Delete(ctx, userBob, adaPhone); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteToken(ctx, cyPhone); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteToken(ctx, cyPhone); err != nil {
		t.Errorf("DeleteToken(missing) error = %v", err)
	}
	if list, _ := store.ListByUsers(ctx, []string{userAda, userBob, userCy}); len(list) != 1 || list[0].Token != adaIPad {
		t.Errorf("after deletes = %+v", list)
	}
}

// TestIntegrationSweepAgainstMongo runs the sweep with the real notification
// service, outbox, and token store, and a fake sender: a notification is
// pushed once, and a second run finds nothing to send.
func TestIntegrationSweepAgainstMongo(t *testing.T) {
	db := newMongoClient(t).Database()
	ctx := context.Background()
	outbox := notifications.NewMongoStore(db)
	service := notifications.NewService(notifications.ServiceOptions{Store: outbox, Now: func() time.Time { return sweepNow }})
	tokens := NewMongoStore(db)
	for _, d := range []DeviceToken{
		{Token: adaPhone, UserID: userAda, Environment: EnvironmentProduction, Platform: PlatformIOS, CreatedAt: sweepNow, UpdatedAt: sweepNow},
		{Token: bobPhone, UserID: userBob, Environment: EnvironmentSandbox, Platform: PlatformIOS, CreatedAt: sweepNow, UpdatedAt: sweepNow},
	} {
		if _, err := tokens.Upsert(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	// The refresher stands in for the order reminder producer.
	produced := 0
	service.SetRefresher(refresherFunc(func(ctx context.Context, householdID string) error {
		produced++
		_, _, err := service.Create(ctx, notifications.New{
			HouseholdID: householdID, Type: notifications.TypeShoppingOrderDue, Title: "Time to order",
			Subject: notifications.Subject{Kind: notifications.SubjectShoppingWeek, ID: "2026-W38"}, DedupeKey: "shopping.order_due:2026-W38",
		})
		return err
	}))
	sender := &fakeSender{errs: map[string]error{bobPhone: &APNsError{Status: 410, Reason: "Unregistered"}}}
	newSweeper := func() *Sweeper {
		return NewSweeper(SweepOptions{
			Households: fakeHouseholds{
				households: map[string]households.Household{hhA: {ID: hhA, TimeZone: "America/Denver"}},
				members:    map[string][]string{hhA: {userAda, userBob}},
			},
			Refresher: service, Outbox: outbox, Tokens: tokens, Sender: sender,
			Now: func() time.Time { return sweepNow },
		})
	}

	report, err := newSweeper().Run(ctx)
	if err != nil || report.Sent != 1 || report.TokensRemoved != 1 {
		t.Fatalf("Run() = %+v, %v", report, err)
	}
	if got := sender.tokens(); !slices.Equal(got, []string{adaPhone, bobPhone}) {
		t.Errorf("sent to %v", got)
	}
	n, err := outbox.FindByDedupeKey(ctx, hhA, "shopping.order_due:2026-W38")
	if err != nil || n.Push.Status != notifications.PushSent || n.Push.Attempts != 1 || !n.Push.SentAt.Equal(sweepNow) {
		t.Errorf("push = %+v, %v", n.Push, err)
	}
	if left, _ := tokens.ListByUsers(ctx, []string{userBob}); len(left) != 0 {
		t.Errorf("Bob's unregistered token was kept: %+v", left)
	}

	report, err = newSweeper().Run(ctx)
	if err != nil || report.Sent != 0 || len(sender.sent) != 2 || produced != 2 {
		t.Errorf("second Run() = %+v, %v; sent = %d, refreshes = %d", report, err, len(sender.sent), produced)
	}
}

type refresherFunc func(ctx context.Context, householdID string) error

func (f refresherFunc) Refresh(ctx context.Context, householdID string) error {
	return f(ctx, householdID)
}
