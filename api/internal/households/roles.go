package households

import (
	"fmt"
	"slices"
)

// Role is a member's role in a household. Authorization never compares roles
// directly; it asks Role.Can for a Permission.
type Role string

// Roles implemented today. Adding a role (viewer, shopper, child, guest) means
// adding a constant, a rolePermissions entry, and an entry in roleOrder.
const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// Permission is an action a household member may be allowed to perform.
type Permission string

// Permissions checked by the API. Later modules (planning, pantry, recipes)
// check the permissions reserved for them here.
const (
	PermHouseholdView     Permission = "household.view"
	PermHouseholdUpdate   Permission = "household.update"
	PermMembersView       Permission = "members.view"
	PermMembersInvite     Permission = "members.invite"
	PermMembersRemove     Permission = "members.remove"
	PermMembersChangeRole Permission = "members.changeRole"
	PermPlanEdit          Permission = "plan.edit"
	PermPantryEdit        Permission = "pantry.edit"
	PermRecipesEdit       Permission = "recipes.edit"
	PermRecipesImport     Permission = "recipes.import"
)

// allPermissions lists every permission in a stable order for responses.
var allPermissions = []Permission{
	PermHouseholdView,
	PermHouseholdUpdate,
	PermMembersView,
	PermMembersInvite,
	PermMembersRemove,
	PermMembersChangeRole,
	PermPlanEdit,
	PermPantryEdit,
	PermRecipesEdit,
	PermRecipesImport,
}

// roleOrder lists the valid roles in a stable order.
var roleOrder = []Role{RoleAdmin, RoleMember}

// rolePermissions is the single table that maps roles to permissions.
var rolePermissions = map[Role][]Permission{
	RoleAdmin: allPermissions,
	RoleMember: {
		PermHouseholdView,
		PermMembersView,
		PermPlanEdit,
		PermPantryEdit,
		PermRecipesEdit,
		PermRecipesImport,
	},
}

// AllPermissions returns every permission, in a stable order.
func AllPermissions() []Permission { return slices.Clone(allPermissions) }

// Roles returns every valid role, in a stable order.
func Roles() []Role { return slices.Clone(roleOrder) }

// ParseRole validates s as a role.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", fmt.Errorf("role must be one of %v", roleOrder)
	}
	return r, nil
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	_, ok := rolePermissions[r]
	return ok
}

// Can reports whether the role grants p. Unknown roles grant nothing.
func (r Role) Can(p Permission) bool {
	return slices.Contains(rolePermissions[r], p)
}

// Permissions returns the permissions the role grants, in a stable order.
func (r Role) Permissions() []Permission {
	return slices.Clone(rolePermissions[r])
}

// Covers reports whether r grants every permission other grants. A member may
// only grant (invite with, or change someone to) a role their own role covers,
// and may only change the role of someone whose role theirs covers, so nobody
// can hand out or take away more access than they have.
func (r Role) Covers(other Role) bool {
	if !r.Valid() || !other.Valid() {
		return false
	}
	for _, p := range rolePermissions[other] {
		if !r.Can(p) {
			return false
		}
	}
	return true
}
