package siwa

import (
	"context"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func TestIntegrationMongoStore(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, Indexes()...); err != nil {
		t.Fatal(err)
	}
	store := NewMongoStore(client.Database())
	const userBob = "bbbbbbbbbbbbbbbbbbbbbbbb"

	if got, err := store.Get(ctx, userAda); err != nil || got != "" {
		t.Fatalf("empty Get = %q, %v", got, err)
	}
	if err := store.Put(ctx, userAda, "sealed-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, userAda, "sealed-2", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, userBob, "sealed-bob", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(ctx, userAda); err != nil || got != "sealed-2" {
		t.Errorf("Get = %q, %v; want the newest token", got, err)
	}
	if err := store.PurgeUser(ctx, userAda); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(ctx, userAda); got != "" {
		t.Errorf("after purge Get = %q", got)
	}
	if got, _ := store.Get(ctx, userBob); got != "sealed-bob" {
		t.Errorf("Bob's token = %q", got)
	}
	if got, err := store.Get(ctx, "not-an-id"); err != nil || got != "" {
		t.Errorf("bad ID Get = %q, %v", got, err)
	}
	if err := store.Put(ctx, "not-an-id", "x", time.Now()); err == nil {
		t.Error("Put with a bad ID succeeded")
	}
}
