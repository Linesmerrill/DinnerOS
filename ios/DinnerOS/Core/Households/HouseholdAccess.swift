import Foundation

/// Decides which household actions the UI offers.
///
/// This is a convenience only. Hiding a button never secures anything: the API checks
/// every request, returning `403 forbidden` or `409 last_admin` when a hidden rule
/// applies. The rules here mirror `Role.Can` and `Role.Covers` on the server.
nonisolated struct HouseholdAccess: Equatable, Sendable {
    let role: HouseholdRole
    let permissions: Set<HouseholdPermission>

    func can(_ permission: HouseholdPermission) -> Bool {
        permissions.contains(permission)
    }

    /// Whether the caller holds every permission `other` grants, so they may grant it,
    /// or manage someone who has it. Unknown roles are never covered.
    func covers(_ other: HouseholdRole) -> Bool {
        guard let required = other.mirroredPermissions else { return false }
        return required.isSubset(of: permissions)
    }

    /// Roles the caller may invite someone with.
    var invitableRoles: [HouseholdRole] {
        guard can(.membersInvite) else { return [] }
        return HouseholdRole.assignable.filter(covers)
    }

    /// Roles `member` may be changed to. Empty when the caller can't change their role.
    /// Your own role isn't changed from the member list; leaving is a separate action.
    func assignableRoles(for member: HouseholdMember, currentUserID: String) -> [HouseholdRole] {
        guard can(.membersChangeRole), member.userID != currentUserID, covers(member.role) else { return [] }
        return HouseholdRole.assignable.filter { $0 != member.role && covers($0) }
    }

    func canRemove(_ member: HouseholdMember, currentUserID: String) -> Bool {
        can(.membersRemove) && member.userID != currentUserID && covers(member.role)
    }
}
