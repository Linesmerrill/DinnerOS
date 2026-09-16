package households

import (
	"context"
	"errors"
	"testing"
)

func TestAccountDeparturesMarkLastMember(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	solo, _ := newHousehold(t, svc, nil)
	shared, _ := newHousehold(t, svc, map[string]Role{userBob: RoleMember})

	got, err := svc.AccountDepartures(ctx, userAda)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{solo.ID: true, shared.ID: false}
	if len(got) != 2 {
		t.Fatalf("AccountDepartures() = %+v", got)
	}
	for _, d := range got {
		if want[d.HouseholdID] != d.LastMember {
			t.Errorf("departure %+v, want LastMember %v", d, want[d.HouseholdID])
		}
	}
	if got, _ := svc.AccountDepartures(ctx, userEve); len(got) != 0 {
		t.Errorf("AccountDepartures(non-member) = %+v", got)
	}
}

func TestDeleteEmptiedHousehold(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	h, _ := newHousehold(t, svc, nil)

	if err := svc.DeleteEmptiedHousehold(ctx, h.ID, userAda); err != nil {
		t.Fatalf("DeleteEmptiedHousehold() error = %v", err)
	}
	if _, err := store.GetHousehold(ctx, h.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("household still exists: %v", err)
	}
	if list, _ := store.ListMembershipsByUser(ctx, userAda); len(list) != 0 {
		t.Errorf("memberships left: %+v", list)
	}
	// Retrying is harmless.
	if err := svc.DeleteEmptiedHousehold(ctx, h.ID, userAda); err != nil {
		t.Errorf("retry error = %v", err)
	}
}

func TestDeleteEmptiedHouseholdRefusesWhenSomeoneJoined(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	h, _ := newHousehold(t, svc, map[string]Role{userBob: RoleMember})

	if err := svc.DeleteEmptiedHousehold(ctx, h.ID, userAda); !errors.Is(err, ErrConflict) {
		t.Fatalf("DeleteEmptiedHousehold() error = %v, want ErrConflict", err)
	}
	if _, err := store.GetHousehold(ctx, h.ID); err != nil {
		t.Errorf("household deleted despite another member: %v", err)
	}
}

func TestLeaveForAccountDeletionPromotesLongestStandingMember(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	h, _ := newHousehold(t, svc, nil)
	// Bob joins before Cat, so Bob has been a member longer.
	for _, u := range []string{userBob, userCat} {
		if _, _, err := svc.AddMember(ctx, h.ID, u, RoleMember); err != nil {
			t.Fatal(err)
		}
	}

	promoted, err := svc.LeaveForAccountDeletion(ctx, h.ID, userAda)
	if err != nil || promoted != userBob {
		t.Fatalf("LeaveForAccountDeletion() = %q, %v; want Bob promoted", promoted, err)
	}
	if m := membership(t, svc, h.ID, userBob); m.Role != RoleAdmin {
		t.Errorf("Bob role = %s, want admin", m.Role)
	}
	if m := membership(t, svc, h.ID, userCat); m.Role != RoleMember {
		t.Errorf("Cat role = %s, want member", m.Role)
	}
	if _, err := store.GetMembership(ctx, h.ID, userAda); !errors.Is(err, ErrNotFound) {
		t.Errorf("Ada still a member: %v", err)
	}
	if n := store.adminCounts[h.ID]; n != 1 {
		t.Errorf("admin count = %d, want 1", n)
	}
	// The household still can't lose its last admin.
	if err := svc.RemoveMember(ctx, membership(t, svc, h.ID, userBob), userBob); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("Bob leaving error = %v, want ErrLastAdmin", err)
	}
}

func TestLeaveForAccountDeletionWithAnotherAdminPromotesNobody(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	h, _ := newHousehold(t, svc, map[string]Role{userBob: RoleAdmin, userCat: RoleMember})

	promoted, err := svc.LeaveForAccountDeletion(ctx, h.ID, userAda)
	if err != nil || promoted != "" {
		t.Fatalf("LeaveForAccountDeletion() = %q, %v", promoted, err)
	}
	if m := membership(t, svc, h.ID, userCat); m.Role != RoleMember {
		t.Errorf("Cat role = %s, want member", m.Role)
	}
	if n := store.adminCounts[h.ID]; n != 1 {
		t.Errorf("admin count = %d, want 1", n)
	}
}

func TestLeaveForAccountDeletionAsMember(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	h, _ := newHousehold(t, svc, map[string]Role{userBob: RoleMember})

	promoted, err := svc.LeaveForAccountDeletion(ctx, h.ID, userBob)
	if err != nil || promoted != "" {
		t.Fatalf("LeaveForAccountDeletion() = %q, %v", promoted, err)
	}
	if _, err := svc.GetMembership(ctx, h.ID, userBob); !errors.Is(err, ErrNotFound) {
		t.Errorf("Bob still a member: %v", err)
	}
	// Already gone is not an error.
	if _, err := svc.LeaveForAccountDeletion(ctx, h.ID, userBob); err != nil {
		t.Errorf("retry error = %v", err)
	}
}
