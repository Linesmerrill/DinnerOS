package households

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

var testNow = time.Date(2026, 9, 14, 18, 30, 0, 0, time.UTC)

// Test user IDs (24 hex chars, like real ObjectIDs).
const (
	userAda   = "aaaaaaaaaaaaaaaaaaaaaaaa"
	userBob   = "bbbbbbbbbbbbbbbbbbbbbbbb"
	userCat   = "cccccccccccccccccccccccc"
	userEve   = "eeeeeeeeeeeeeeeeeeeeeeee"
	missingID = "ffffffffffffffffffffffff"
)

type fakeUsers map[string]string // id → display name

func (f fakeUsers) GetUser(_ context.Context, id string) (users.User, error) {
	name, ok := f[id]
	if !ok {
		return users.User{}, users.ErrNotFound
	}
	return users.User{ID: id, DisplayName: name}, nil
}

func newTestService(t *testing.T) (*Service, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	svc := NewService(ServiceOptions{
		Store: store,
		Users: fakeUsers{userAda: "Ada", userBob: "Bob", userCat: "Cat"},
		Now:   func() time.Time { return testNow },
	})
	return svc, store
}

func ptr[T any](v T) *T { return &v }

// newHousehold creates a household owned by userAda with extra members added.
func newHousehold(t *testing.T, svc *Service, members map[string]Role) (Household, Membership) {
	t.Helper()
	ctx := context.Background()
	h, admin, err := svc.Create(ctx, userAda, CreateInput{Name: "Home", TimeZone: "America/Denver"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for userID, role := range members {
		if _, _, err := svc.AddMember(ctx, h.ID, userID, role); err != nil {
			t.Fatalf("AddMember(%s) error = %v", userID, err)
		}
	}
	return h, admin
}

func membership(t *testing.T, svc *Service, householdID, userID string) Membership {
	t.Helper()
	m, err := svc.GetMembership(context.Background(), householdID, userID)
	if err != nil {
		t.Fatalf("GetMembership(%s) error = %v", userID, err)
	}
	return m
}

func TestCreateHousehold(t *testing.T) {
	svc, store := newTestService(t)
	h, m, err := svc.Create(context.Background(), userAda, CreateInput{Name: "  The Lines  ", TimeZone: " America/Denver "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if h.ID == "" || h.Name != "The Lines" || h.TimeZone != "America/Denver" || h.DefaultServings != DefaultServings ||
		h.CreatedBy != userAda || !h.CreatedAt.Equal(testNow) || !h.UpdatedAt.Equal(testNow) {
		t.Errorf("household = %+v", h)
	}
	if m.HouseholdID != h.ID || m.UserID != userAda || m.Role != RoleAdmin {
		t.Errorf("membership = %+v", m)
	}
	if n := store.adminCount(h.ID); n != 1 {
		t.Errorf("admin count = %d, want 1", n)
	}

	h2, _, err := svc.Create(context.Background(), userAda, CreateInput{Name: "Cabin", TimeZone: "UTC", DefaultServings: ptr(4)})
	if err != nil || h2.DefaultServings != 4 {
		t.Errorf("Create(servings 4) = %+v, %v", h2, err)
	}
}

func TestCreateHouseholdValidation(t *testing.T) {
	tests := []struct {
		name string
		in   CreateInput
		want string
	}{
		{"missing name", CreateInput{Name: "  ", TimeZone: "UTC"}, "name is required"},
		{"long name", CreateInput{Name: strings.Repeat("é", 101), TimeZone: "UTC"}, "at most 100"},
		{"missing time zone", CreateInput{Name: "Home"}, "timeZone is required"},
		{"unknown time zone", CreateInput{Name: "Home", TimeZone: "Mars/Olympus"}, "IANA"},
		{"local time zone", CreateInput{Name: "Home", TimeZone: "Local"}, "IANA"},
		{"offset instead of zone", CreateInput{Name: "Home", TimeZone: "-07:00"}, "IANA"},
		{"zero servings", CreateInput{Name: "Home", TimeZone: "UTC", DefaultServings: ptr(0)}, "between 1 and 12"},
		{"too many servings", CreateInput{Name: "Home", TimeZone: "UTC", DefaultServings: ptr(13)}, "between 1 and 12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newTestService(t)
			_, _, err := svc.Create(context.Background(), userAda, tt.in)
			var ve *ValidationError
			if !errors.As(err, &ve) || !strings.Contains(ve.Message, tt.want) {
				t.Fatalf("Create() error = %v, want validation error containing %q", err, tt.want)
			}
			if len(store.households) != 0 {
				t.Error("household was stored despite invalid input")
			}
		})
	}
}

func TestCreateHouseholdCleansUpWhenMembershipFails(t *testing.T) {
	svc, store := newTestService(t)
	store.failCreateMembership = errors.New("boom")
	if _, _, err := svc.Create(context.Background(), userAda, CreateInput{Name: "Home", TimeZone: "UTC"}); err == nil {
		t.Fatal("Create() error = nil")
	}
	if len(store.households) != 0 {
		t.Errorf("orphaned household left behind: %+v", store.households)
	}
}

type createdRecorder []string

func (r *createdRecorder) HouseholdCreated(_ context.Context, householdID string) {
	*r = append(*r, householdID)
}

func TestCreateHouseholdNotifiesListener(t *testing.T) {
	store := newMemoryStore()
	var heard createdRecorder
	svc := NewService(ServiceOptions{Store: store, OnCreated: &heard, Now: func() time.Time { return testNow }})
	h, _, err := svc.Create(context.Background(), userAda, CreateInput{Name: "Home", TimeZone: "UTC"})
	if err != nil || len(heard) != 1 || heard[0] != h.ID {
		t.Fatalf("Create() = %+v, %v; listener heard %v", h, err, heard)
	}
	store.failCreateMembership = errors.New("boom")
	if _, _, err := svc.Create(context.Background(), userAda, CreateInput{Name: "Cabin", TimeZone: "UTC"}); err == nil || len(heard) != 1 {
		t.Errorf("failed Create() error = %v; listener heard %v", err, heard)
	}
}

func TestListForUser(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	home, _ := newHousehold(t, svc, map[string]Role{userBob: RoleMember})
	cabin, _, err := svc.Create(ctx, userBob, CreateInput{Name: "Cabin", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}

	bob, err := svc.ListForUser(ctx, userBob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bob) != 2 || bob[0].Household.ID != home.ID || bob[0].Membership.Role != RoleMember ||
		bob[1].Household.ID != cabin.ID || bob[1].Membership.Role != RoleAdmin {
		t.Errorf("ListForUser(bob) = %+v", bob)
	}
	ada, _ := svc.ListForUser(ctx, userAda)
	if len(ada) != 1 || ada[0].Household.ID != home.ID {
		t.Errorf("ListForUser(ada) = %+v", ada)
	}
	eve, err := svc.ListForUser(ctx, userEve)
	if err != nil || eve == nil || len(eve) != 0 {
		t.Errorf("ListForUser(eve) = %#v, %v; want empty non-nil", eve, err)
	}
}

func TestAuthorizeIsolation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	h, _ := newHousehold(t, svc, map[string]Role{userBob: RoleMember})

	tests := []struct {
		name        string
		householdID string
		userID      string
		perm        Permission
		wantErr     error
	}{
		{"admin", h.ID, userAda, PermMembersInvite, nil},
		{"member allowed", h.ID, userBob, PermPlanEdit, nil},
		{"member forbidden", h.ID, userBob, PermMembersInvite, ErrForbidden},
		{"non-member", h.ID, userEve, PermHouseholdView, ErrNotFound},
		{"unknown household", missingID, userAda, PermHouseholdView, ErrNotFound},
		{"malformed id", "not-an-id", userAda, PermHouseholdView, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Authorize(ctx, tt.householdID, tt.userID, tt.perm)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Errorf("Authorize() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDetails(t *testing.T) {
	svc, _ := newTestService(t)
	h, admin := newHousehold(t, svc, map[string]Role{userBob: RoleMember})

	got, members, err := svc.Details(context.Background(), admin)
	if err != nil || got.ID != h.ID {
		t.Fatalf("Details() = %+v, %v", got, err)
	}
	if len(members) != 2 ||
		members[0] != (Member{UserID: userAda, DisplayName: "Ada", Role: RoleAdmin, JoinedAt: testNow}) ||
		members[1] != (Member{UserID: userBob, DisplayName: "Bob", Role: RoleMember, JoinedAt: testNow}) {
		t.Errorf("members = %+v", members)
	}
}

func TestUpdateHouseholdOrderDay(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	h, admin := newHousehold(t, svc, nil)

	if h.OrderDay != "" {
		t.Errorf("new household OrderDay = %q, want none", h.OrderDay)
	}

	updated, err := svc.Update(ctx, admin, UpdateInput{OrderDay: ptr("thu")})
	if err != nil || updated.OrderDay != "thu" {
		t.Fatalf("Update() = %+v, %v; want orderDay thu", updated, err)
	}
	// Setting the order day alone leaves the rest of the household alone.
	if updated.Name != h.Name || updated.TimeZone != h.TimeZone || updated.DefaultServings != h.DefaultServings {
		t.Errorf("Update() changed more than the order day: %+v", updated)
	}

	// Turning reminders off is an empty order day, not a missing field.
	cleared, err := svc.Update(ctx, admin, UpdateInput{OrderDay: ptr("")})
	if err != nil || cleared.OrderDay != "" {
		t.Fatalf("clearing orderDay = %+v, %v", cleared, err)
	}

	var ve *ValidationError
	for _, bad := range []string{"Thursday", "thur", "0"} {
		if _, err := svc.Update(ctx, admin, UpdateInput{OrderDay: ptr(bad)}); !errors.As(err, &ve) {
			t.Errorf("Update(orderDay %q) error = %v, want ValidationError", bad, err)
		}
	}
}

func TestUpdateHousehold(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	h, admin := newHousehold(t, svc, map[string]Role{userBob: RoleMember})

	updated, err := svc.Update(ctx, admin, UpdateInput{Name: ptr(" Casa "), DefaultServings: ptr(4)})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "Casa" || updated.DefaultServings != 4 || updated.TimeZone != h.TimeZone {
		t.Errorf("Update() = %+v", updated)
	}

	if _, err := svc.Update(ctx, membership(t, svc, h.ID, userBob), UpdateInput{Name: ptr("Mine")}); !errors.Is(err, ErrForbidden) {
		t.Errorf("member Update() error = %v, want ErrForbidden", err)
	}

	invalidInputs := map[string]UpdateInput{
		"empty patch":    {},
		"blank name":     {Name: ptr(" ")},
		"bad time zone":  {TimeZone: ptr("Nowhere/Land")},
		"servings range": {DefaultServings: ptr(99)},
	}
	for name, in := range invalidInputs {
		t.Run(name, func(t *testing.T) {
			var ve *ValidationError
			if _, err := svc.Update(ctx, admin, in); !errors.As(err, &ve) {
				t.Errorf("Update() error = %v, want ValidationError", err)
			}
		})
	}
}

func TestLastAdminGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("sole admin cannot demote themselves", func(t *testing.T) {
		svc, _ := newTestService(t)
		h, admin := newHousehold(t, svc, map[string]Role{userBob: RoleMember})
		if _, err := svc.ChangeRole(ctx, admin, userAda, RoleMember); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("ChangeRole() error = %v, want ErrLastAdmin", err)
		}
		if m := membership(t, svc, h.ID, userAda); m.Role != RoleAdmin {
			t.Errorf("role = %s, want admin", m.Role)
		}
	})

	t.Run("sole admin cannot leave while others remain", func(t *testing.T) {
		svc, _ := newTestService(t)
		h, admin := newHousehold(t, svc, map[string]Role{userBob: RoleMember})
		if err := svc.RemoveMember(ctx, admin, userAda); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("RemoveMember(self) error = %v, want ErrLastAdmin", err)
		}
		membership(t, svc, h.ID, userAda)
	})

	t.Run("lone member cannot leave", func(t *testing.T) {
		svc, _ := newTestService(t)
		_, admin := newHousehold(t, svc, nil)
		if err := svc.RemoveMember(ctx, admin, userAda); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("RemoveMember(self) error = %v, want ErrLastAdmin", err)
		}
	})

	t.Run("admin can be removed or demoted when another admin remains", func(t *testing.T) {
		svc, store := newTestService(t)
		h, ada := newHousehold(t, svc, map[string]Role{userBob: RoleAdmin, userCat: RoleAdmin})
		if n := store.adminCount(h.ID); n != 3 {
			t.Fatalf("admin count = %d, want 3", n)
		}
		if _, err := svc.ChangeRole(ctx, ada, userBob, RoleMember); err != nil {
			t.Fatalf("demote bob: %v", err)
		}
		if err := svc.RemoveMember(ctx, ada, userCat); err != nil {
			t.Fatalf("remove cat: %v", err)
		}
		if n := store.adminCount(h.ID); n != 1 {
			t.Errorf("admin count = %d, want 1", n)
		}
		if err := svc.RemoveMember(ctx, ada, userAda); !errors.Is(err, ErrLastAdmin) {
			t.Errorf("last admin leave error = %v, want ErrLastAdmin", err)
		}
		// Promoting again lets Ada step down.
		if _, err := svc.ChangeRole(ctx, ada, userBob, RoleAdmin); err != nil {
			t.Fatalf("promote bob: %v", err)
		}
		if err := svc.RemoveMember(ctx, ada, userAda); err != nil {
			t.Errorf("leave after promotion: %v", err)
		}
	})

	t.Run("concurrent demotions keep one admin", func(t *testing.T) {
		svc, store := newTestService(t)
		h, _ := newHousehold(t, svc, map[string]Role{userBob: RoleAdmin})
		ada, bob := membership(t, svc, h.ID, userAda), membership(t, svc, h.ID, userBob)

		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, pair := range [][2]any{{ada, userBob}, {bob, userAda}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = svc.ChangeRole(ctx, pair[0].(Membership), pair[1].(string), RoleMember)
			}()
		}
		wg.Wait()

		succeeded := 0
		for _, err := range errs {
			switch {
			case err == nil:
				succeeded++
			case !errors.Is(err, ErrLastAdmin):
				t.Errorf("unexpected error %v", err)
			}
		}
		if succeeded != 1 {
			t.Fatalf("successful demotions = %d (errors %v), want exactly 1", succeeded, errs)
		}
		admins := 0
		for _, m := range store.memberships {
			if m.Role == RoleAdmin {
				admins++
			}
		}
		if admins != 1 || store.adminCount(h.ID) != 1 {
			t.Errorf("admins = %d, count = %d; want 1 and 1", admins, store.adminCount(h.ID))
		}
	})
}

func TestMemberPermissions(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	h, _ := newHousehold(t, svc, map[string]Role{userBob: RoleMember, userCat: RoleMember})
	bob := membership(t, svc, h.ID, userBob)

	if _, err := svc.ChangeRole(ctx, bob, userCat, RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Errorf("member ChangeRole() error = %v, want ErrForbidden", err)
	}
	if _, err := svc.ChangeRole(ctx, bob, userBob, RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Errorf("member self-promotion error = %v, want ErrForbidden", err)
	}
	if err := svc.RemoveMember(ctx, bob, userCat); !errors.Is(err, ErrForbidden) {
		t.Errorf("member RemoveMember(other) error = %v, want ErrForbidden", err)
	}
	// Leaving needs no permission.
	if err := svc.RemoveMember(ctx, bob, userBob); err != nil {
		t.Errorf("member leave error = %v", err)
	}
	if _, err := svc.GetMembership(ctx, h.ID, userBob); !errors.Is(err, ErrNotFound) {
		t.Errorf("membership after leave: %v, want ErrNotFound", err)
	}
}

func TestChangeRoleAndRemoveErrors(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	h, admin := newHousehold(t, svc, map[string]Role{userBob: RoleMember})

	var ve *ValidationError
	if _, err := svc.ChangeRole(ctx, admin, userBob, "owner"); !errors.As(err, &ve) {
		t.Errorf("invalid role error = %v, want ValidationError", err)
	}
	if _, err := svc.ChangeRole(ctx, admin, userEve, RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("non-member target error = %v, want ErrNotFound", err)
	}
	if err := svc.RemoveMember(ctx, admin, userEve); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove non-member error = %v, want ErrNotFound", err)
	}
	same, err := svc.ChangeRole(ctx, admin, userBob, RoleMember)
	if err != nil || same.Role != RoleMember || same.DisplayName != "Bob" {
		t.Errorf("no-op ChangeRole() = %+v, %v", same, err)
	}
	promoted, err := svc.ChangeRole(ctx, admin, userBob, RoleAdmin)
	if err != nil || promoted.Role != RoleAdmin {
		t.Errorf("promote = %+v, %v", promoted, err)
	}
	if m := membership(t, svc, h.ID, userBob); m.Role != RoleAdmin {
		t.Errorf("stored role = %s", m.Role)
	}
}

func TestAddMember(t *testing.T) {
	ctx := context.Background()
	svc, store := newTestService(t)
	h, _ := newHousehold(t, svc, nil)

	m, created, err := svc.AddMember(ctx, h.ID, userBob, RoleMember)
	if err != nil || !created || m.Role != RoleMember {
		t.Fatalf("AddMember() = %+v, %v, %v", m, created, err)
	}
	again, created, err := svc.AddMember(ctx, h.ID, userBob, RoleAdmin)
	if err != nil || created || again.ID != m.ID || again.Role != RoleMember {
		t.Errorf("repeat AddMember() = %+v, %v, %v; want existing member unchanged", again, created, err)
	}
	if n := store.adminCount(h.ID); n != 1 {
		t.Errorf("admin count = %d, want 1 (repeat add must not count)", n)
	}
	if _, _, err := svc.AddMember(ctx, missingID, userBob, RoleMember); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddMember(unknown household) error = %v", err)
	}
	if _, _, err := svc.AddMember(ctx, h.ID, userCat, RoleAdmin); err != nil || store.adminCount(h.ID) != 2 {
		t.Errorf("AddMember(admin) err = %v, admin count = %d", err, store.adminCount(h.ID))
	}
}
