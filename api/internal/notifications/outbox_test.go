package notifications

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func TestIntegrationMongoOutbox(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, Indexes()...); err != nil {
		t.Fatal(err)
	}
	store := NewMongoStore(client.Database())
	insert := func(key string, at time.Time) Notification {
		t.Helper()
		n, err := store.Insert(ctx, Notification{
			HouseholdID: hhA, Type: TypeShoppingOrderDue, Title: "Time to order", DedupeKey: key,
			Push: Push{Status: PushPending}, CreatedAt: at,
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	newer := insert("newer", testNow.Add(time.Hour))
	older := insert("older", testNow)

	pending, err := store.PendingPush(ctx, 10)
	if err != nil || len(pending) != 2 || pending[0].ID != older.ID || pending[1].ID != newer.ID {
		t.Fatalf("PendingPush() = %v, %v; want oldest first", ids(pending), err)
	}

	// Many sweeps racing for one notification: exactly one wins the claim.
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			ok, err := store.ClaimPush(ctx, older.ID, testNow)
			if err != nil {
				t.Errorf("ClaimPush() error = %v", err)
			}
			if ok {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("claims won = %d, want 1", wins.Load())
	}
	if pending, _ := store.PendingPush(ctx, 10); len(pending) != 1 || pending[0].ID != newer.ID {
		t.Errorf("PendingPush() after claim = %v", ids(pending))
	}
	claimed, _ := store.FindByDedupeKey(ctx, hhA, "older")
	if claimed.Push.Status != PushSending || claimed.Push.Attempts != 1 || !claimed.Push.LastAttemptAt.Equal(testNow) {
		t.Errorf("claimed push = %+v", claimed.Push)
	}

	sentAt := testNow.Add(time.Minute)
	if err := store.FinishPush(ctx, older.ID, PushSent, sentAt); err != nil {
		t.Fatal(err)
	}
	if done, _ := store.FindByDedupeKey(ctx, hhA, "older"); done.Push.Status != PushSent || !done.Push.SentAt.Equal(sentAt) {
		t.Errorf("finished push = %+v", done.Push)
	}
	if ok, err := store.ClaimPush(ctx, older.ID, testNow); ok || err != nil {
		t.Errorf("ClaimPush(sent) = %v, %v; a sent push is never claimed again", ok, err)
	}
	if ok, err := store.ClaimPush(ctx, "not-an-id", testNow); ok || err != nil {
		t.Errorf("ClaimPush(malformed) = %v, %v", ok, err)
	}
	if err := store.FinishPush(ctx, "66e5a1f2c3b4a5d6e7f8ffff", PushSkipped, testNow); err == nil {
		t.Error("FinishPush(missing) succeeded")
	}
}
