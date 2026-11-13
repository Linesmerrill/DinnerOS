package plus

import (
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

// The store keeps one entitlement per household and never lets an older
// event overwrite a newer one.
func TestIntegrationPlusMongoStore(t *testing.T) {
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(t.Context(), Indexes()...); err != nil {
		t.Fatal(err)
	}
	store := NewMongoStore(client.Database())
	ctx := t.Context()
	if _, err := store.Get(ctx, hh); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty get = %v", err)
	}
	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	e := Entitlement{HouseholdID: hh, ProductID: "p", Trial: true, WillRenew: true, ExpiresAt: at.Add(time.Hour),
		LastEventID: "e2", LastEventAt: at, UpdatedAt: at}
	if ok, err := store.Put(ctx, e); err != nil || !ok {
		t.Fatalf("put = %v, %v", ok, err)
	}
	older := e
	older.LastEventID, older.LastEventAt, older.Trial = "e1", at.Add(-time.Minute), false
	if ok, err := store.Put(ctx, older); err != nil || ok {
		t.Errorf("older put = %v, %v", ok, err)
	}
	got, err := store.Get(ctx, hh)
	if err != nil || got.LastEventID != "e2" || !got.Trial || !got.ExpiresAt.Equal(at.Add(time.Hour)) {
		t.Errorf("get = %+v, %v", got, err)
	}
	if err := store.Delete(ctx, hh); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, hh); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete = %v", err)
	}
}
