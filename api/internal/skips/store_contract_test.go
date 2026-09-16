package skips

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Synthetic IDs, valid ObjectID hex so the fixtures work against MongoDB.
const (
	testHousehold  = "66e5a1f2c3b4a5d6e7f80a01"
	otherHousehold = "66e5a1f2c3b4a5d6e7f80b01"
	testUser       = "66e5a1f2c3b4a5d6e7f80c01"
	otherUser      = "66e5a1f2c3b4a5d6e7f80c02"
)

var testNow = time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC)

func cilantro(householdID string, scope Scope, week string) Skip {
	return Skip{
		HouseholdID: householdID, IngredientKey: "name:cilantro", Key: "cilantro", Name: "Cilantro",
		Scope: scope, Week: week,
		CreatedBy: testUser, CreatedAt: testNow, UpdatedBy: testUser, UpdatedAt: testNow,
	}
}

// runStoreContract checks behavior both stores must share.
func runStoreContract(t *testing.T, store Store) {
	ctx := context.Background()

	t.Run("put creates then replaces one skip per ingredient", func(t *testing.T) {
		stored, created, err := store.PutSkip(ctx, cilantro(testHousehold, ScopeWeek, "2026-W38"))
		if err != nil || !created {
			t.Fatalf("PutSkip() = %+v, created %v, error %v; want created", stored, created, err)
		}
		if stored.ID == "" {
			t.Fatal("PutSkip() must assign an ID")
		}
		if stored.Scope != ScopeWeek || stored.Week != "2026-W38" {
			t.Errorf("stored scope = %q week = %q, want week/2026-W38", stored.Scope, stored.Week)
		}

		// "Skip once" becomes "skip forever" without stacking a second skip.
		later := cilantro(testHousehold, ScopeAlways, "")
		later.UpdatedBy, later.UpdatedAt = otherUser, testNow.Add(time.Hour)
		replaced, created, err := store.PutSkip(ctx, later)
		if err != nil {
			t.Fatalf("PutSkip() replace error = %v", err)
		}
		if created {
			t.Error("replacing an existing skip must not report created")
		}
		if replaced.ID != stored.ID {
			t.Errorf("replace changed the ID: %q → %q", stored.ID, replaced.ID)
		}
		if replaced.Scope != ScopeAlways || replaced.Week != "" {
			t.Errorf("replaced scope = %q week = %q, want always and no week", replaced.Scope, replaced.Week)
		}
		if replaced.CreatedBy != testUser || !replaced.CreatedAt.Equal(testNow) {
			t.Errorf("replace must keep CreatedBy/CreatedAt, got %q/%v", replaced.CreatedBy, replaced.CreatedAt)
		}
		if replaced.UpdatedBy != otherUser {
			t.Errorf("replaced UpdatedBy = %q, want %q", replaced.UpdatedBy, otherUser)
		}

		items, err := store.ListSkips(ctx, testHousehold)
		if err != nil {
			t.Fatalf("ListSkips() error = %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("ListSkips() returned %d skips, want 1", len(items))
		}
	})

	t.Run("skips are scoped to their household", func(t *testing.T) {
		if _, _, err := store.PutSkip(ctx, cilantro(otherHousehold, ScopeAlways, "")); err != nil {
			t.Fatalf("PutSkip() for the other household error = %v", err)
		}
		mine, err := store.ListSkips(ctx, testHousehold)
		if err != nil {
			t.Fatalf("ListSkips() error = %v", err)
		}
		for _, s := range mine {
			if s.HouseholdID != testHousehold {
				t.Errorf("ListSkips(%q) returned a skip for %q", testHousehold, s.HouseholdID)
			}
		}
		n, err := store.CountSkips(ctx, testHousehold)
		if err != nil || n != len(mine) {
			t.Errorf("CountSkips() = %d, %v; want %d", n, err, len(mine))
		}
	})

	t.Run("delete removes one skip and is scoped", func(t *testing.T) {
		mine, err := store.ListSkips(ctx, testHousehold)
		if err != nil || len(mine) == 0 {
			t.Fatalf("ListSkips() = %d skips, %v", len(mine), err)
		}
		target := mine[0]
		// Another household cannot resume this one's ingredient.
		if err := store.DeleteSkip(ctx, otherHousehold, target.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteSkip() across households = %v, want ErrNotFound", err)
		}
		if err := store.DeleteSkip(ctx, testHousehold, target.ID); err != nil {
			t.Fatalf("DeleteSkip() error = %v", err)
		}
		if err := store.DeleteSkip(ctx, testHousehold, target.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("deleting twice = %v, want ErrNotFound", err)
		}
		if err := store.DeleteSkip(ctx, testHousehold, "not-an-id"); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteSkip() with a malformed ID = %v, want ErrNotFound", err)
		}
	})
}

func TestMemoryStoreContract(t *testing.T) {
	runStoreContract(t, newMemoryStore())
}
