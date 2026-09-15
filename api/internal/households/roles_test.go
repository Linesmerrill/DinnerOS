package households

import (
	"slices"
	"testing"
)

func TestRolePermissionMatrix(t *testing.T) {
	// Every (role, permission) pair is listed explicitly so any change to the
	// table is a deliberate, reviewed test change.
	want := map[Permission]map[Role]bool{
		PermHouseholdView:     {RoleAdmin: true, RoleMember: true},
		PermHouseholdUpdate:   {RoleAdmin: true, RoleMember: false},
		PermMembersView:       {RoleAdmin: true, RoleMember: true},
		PermMembersInvite:     {RoleAdmin: true, RoleMember: false},
		PermMembersRemove:     {RoleAdmin: true, RoleMember: false},
		PermMembersChangeRole: {RoleAdmin: true, RoleMember: false},
		PermPlanEdit:          {RoleAdmin: true, RoleMember: true},
		PermPantryEdit:        {RoleAdmin: true, RoleMember: true},
		PermRecipesEdit:       {RoleAdmin: true, RoleMember: true},
		PermRecipesImport:     {RoleAdmin: true, RoleMember: true},
	}
	if len(want) != len(AllPermissions()) {
		t.Fatalf("matrix covers %d permissions, AllPermissions() has %d", len(want), len(AllPermissions()))
	}
	for _, p := range AllPermissions() {
		for _, r := range Roles() {
			expected, ok := want[p][r]
			if !ok {
				t.Errorf("matrix is missing (%s, %s)", r, p)
				continue
			}
			if got := r.Can(p); got != expected {
				t.Errorf("%s.Can(%s) = %v, want %v", r, p, got, expected)
			}
			if got := slices.Contains(r.Permissions(), p); got != expected {
				t.Errorf("%s.Permissions() contains %s = %v, want %v", r, p, got, expected)
			}
		}
	}
}

func TestUnknownRoleGrantsNothing(t *testing.T) {
	for _, r := range []Role{"", "owner", "ADMIN", "viewer"} {
		if r.Valid() {
			t.Errorf("Role(%q).Valid() = true", r)
		}
		for _, p := range AllPermissions() {
			if r.Can(p) {
				t.Errorf("Role(%q).Can(%s) = true", r, p)
			}
		}
		if _, err := ParseRole(string(r)); err == nil {
			t.Errorf("ParseRole(%q) error = nil", r)
		}
	}
	if RoleAdmin.Can("household.delete") {
		t.Error("admin can an unknown permission")
	}
}

func TestParseRole(t *testing.T) {
	for _, r := range Roles() {
		got, err := ParseRole(string(r))
		if err != nil || got != r {
			t.Errorf("ParseRole(%q) = %q, %v", r, got, err)
		}
	}
}

func TestRoleCovers(t *testing.T) {
	tests := []struct {
		r, other Role
		want     bool
	}{
		{RoleAdmin, RoleAdmin, true},
		{RoleAdmin, RoleMember, true},
		{RoleMember, RoleMember, true},
		{RoleMember, RoleAdmin, false},
		{RoleAdmin, "bogus", false},
		{"bogus", RoleMember, false},
	}
	for _, tt := range tests {
		if got := tt.r.Covers(tt.other); got != tt.want {
			t.Errorf("%q.Covers(%q) = %v, want %v", tt.r, tt.other, got, tt.want)
		}
	}
}
