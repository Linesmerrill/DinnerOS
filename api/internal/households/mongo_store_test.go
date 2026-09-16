package households

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestMongoStore(t *testing.T) *MongoStore {
	t.Helper()
	client := mongotest.Client(t)
	if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
		t.Fatalf("EnsureIndexes() error = %v", err)
	}
	// Applying indexes twice is a no-op.
	if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
		t.Fatalf("second EnsureIndexes() error = %v", err)
	}
	return NewMongoStore(client.Database())
}

func TestIntegrationMongoStoreHouseholds(t *testing.T) {
	store := newTestMongoStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	h, err := store.CreateHousehold(ctx, Household{
		Name: "Home", DefaultServings: 2, TimeZone: "America/Denver", CreatedBy: userAda, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateHousehold() error = %v", err)
	}
	got, err := store.GetHousehold(ctx, h.ID)
	if err != nil || got != h {
		t.Fatalf("GetHousehold() = %+v, %v; want %+v", got, err, h)
	}

	later := now.Add(time.Hour)
	updated, err := store.UpdateHousehold(ctx, h.ID, HouseholdPatch{Name: ptr("Casa"), DefaultServings: ptr(5)}, later)
	if err != nil || updated.Name != "Casa" || updated.DefaultServings != 5 || updated.TimeZone != "America/Denver" || !updated.UpdatedAt.Equal(later) {
		t.Errorf("UpdateHousehold() = %+v, %v", updated, err)
	}
	if _, err := store.UpdateHousehold(ctx, missingID, HouseholdPatch{Name: ptr("x")}, later); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateHousehold(missing) error = %v", err)
	}

	other, _ := store.CreateHousehold(ctx, Household{Name: "Cabin", DefaultServings: 2, TimeZone: "UTC", CreatedBy: userBob, CreatedAt: now, UpdatedAt: now})
	list, err := store.ListHouseholds(ctx, []string{h.ID, other.ID, missingID, "bad"})
	if err != nil || len(list) != 2 {
		t.Errorf("ListHouseholds() = %+v, %v", list, err)
	}
	// Paging over every household, in ID order.
	page, err := store.ListHouseholdIDs(ctx, "", 1)
	if err != nil || len(page) != 1 || page[0] != h.ID {
		t.Errorf("ListHouseholdIDs(first) = %v, %v", page, err)
	}
	if page, err := store.ListHouseholdIDs(ctx, h.ID, 10); err != nil || len(page) != 1 || page[0] != other.ID {
		t.Errorf("ListHouseholdIDs(after) = %v, %v", page, err)
	}
	if page, err := store.ListHouseholdIDs(ctx, other.ID, 10); err != nil || len(page) != 0 {
		t.Errorf("ListHouseholdIDs(last) = %v, %v", page, err)
	}

	// Admin count starts at 1 and never drops below it.
	if err := store.DecrementAdminCount(ctx, h.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("DecrementAdminCount(1) error = %v, want ErrLastAdmin", err)
	}
	if err := store.IncrementAdminCount(ctx, h.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DecrementAdminCount(ctx, h.ID); err != nil {
		t.Errorf("DecrementAdminCount(2) error = %v", err)
	}
	if err := store.DecrementAdminCount(ctx, missingID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DecrementAdminCount(missing) error = %v, want ErrNotFound", err)
	}
	if err := store.IncrementAdminCount(ctx, missingID); !errors.Is(err, ErrNotFound) {
		t.Errorf("IncrementAdminCount(missing) error = %v, want ErrNotFound", err)
	}

	if err := store.DeleteHousehold(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetHousehold(ctx, other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetHousehold(deleted) error = %v", err)
	}
	if _, err := store.GetHousehold(ctx, "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetHousehold(invalid) error = %v", err)
	}
}

func TestIntegrationMongoStoreMemberships(t *testing.T) {
	store := newTestMongoStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	const home, cabin = "111111111111111111111111", "222222222222222222222222"

	ada, err := store.CreateMembership(ctx, Membership{HouseholdID: home, UserID: userAda, Role: RoleAdmin, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}
	if _, err := store.CreateMembership(ctx, Membership{HouseholdID: home, UserID: userAda, Role: RoleMember}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate CreateMembership() error = %v, want ErrDuplicate", err)
	}
	if _, err := store.CreateMembership(ctx, Membership{HouseholdID: home, UserID: userBob, Role: RoleMember, CreatedAt: now.Add(time.Second), UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMembership(ctx, Membership{HouseholdID: cabin, UserID: userAda, Role: RoleMember, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetMembership(ctx, home, userAda)
	if err != nil || got != ada {
		t.Errorf("GetMembership() = %+v, %v; want %+v", got, err, ada)
	}
	if _, err := store.GetMembership(ctx, home, userEve); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMembership(non-member) error = %v", err)
	}
	if _, err := store.GetMembership(ctx, "bad", userAda); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMembership(invalid) error = %v", err)
	}

	byHousehold, err := store.ListMembershipsByHousehold(ctx, home)
	if err != nil || len(byHousehold) != 2 || byHousehold[0].UserID != userAda || byHousehold[1].UserID != userBob {
		t.Errorf("ListMembershipsByHousehold() = %+v, %v", byHousehold, err)
	}
	byUser, err := store.ListMembershipsByUser(ctx, userAda)
	if err != nil || len(byUser) != 2 {
		t.Errorf("ListMembershipsByUser() = %+v, %v", byUser, err)
	}

	later := now.Add(time.Minute)
	if _, err := store.UpdateMembershipRole(ctx, home, userBob, RoleAdmin, RoleMember, later); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateMembershipRole(wrong from) error = %v, want ErrNotFound", err)
	}
	promoted, err := store.UpdateMembershipRole(ctx, home, userBob, RoleMember, RoleAdmin, later)
	if err != nil || promoted.Role != RoleAdmin || !promoted.UpdatedAt.Equal(later) {
		t.Errorf("UpdateMembershipRole() = %+v, %v", promoted, err)
	}

	if err := store.DeleteMembership(ctx, home, userBob, RoleMember); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteMembership(wrong role) error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteMembership(ctx, home, userBob, RoleAdmin); err != nil {
		t.Errorf("DeleteMembership() error = %v", err)
	}
	if _, err := store.GetMembership(ctx, home, userBob); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMembership(deleted) error = %v", err)
	}
}

func TestIntegrationConcurrentDemotionsKeepOneAdmin(t *testing.T) {
	store := newTestMongoStore(t)
	svc := NewService(ServiceOptions{Store: store, Users: fakeUsers{}})
	ctx := context.Background()

	for range 5 {
		h, ada, err := svc.Create(ctx, userAda, CreateInput{Name: "Home", TimeZone: "UTC"})
		if err != nil {
			t.Fatal(err)
		}
		bob, _, err := svc.AddMember(ctx, h.ID, userBob, RoleAdmin)
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); errs[0] = svc.RemoveMember(ctx, ada, userAda) }()
		go func() { defer wg.Done(); _, errs[1] = svc.ChangeRole(ctx, bob, userBob, RoleMember) }()
		wg.Wait()

		members, err := store.ListMembershipsByHousehold(ctx, h.ID)
		if err != nil {
			t.Fatal(err)
		}
		admins := 0
		for _, m := range members {
			if m.Role == RoleAdmin {
				admins++
			}
		}
		if admins != 1 {
			t.Fatalf("admins after concurrent step-downs = %d (errors %v), want 1", admins, errs)
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, ErrLastAdmin) {
				t.Errorf("unexpected error %v", err)
			}
		}
	}
}
