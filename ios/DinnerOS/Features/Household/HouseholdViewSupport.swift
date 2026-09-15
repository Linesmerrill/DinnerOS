import SwiftUI

/// An inline error inside a form, announced to VoiceOver when it appears.
struct FormErrorLabel: View {
    let message: String

    var body: some View {
        Label(message, systemImage: "exclamationmark.triangle.fill")
            .symbolRenderingMode(.multicolor)
            .onAppear {
                AccessibilityNotification.Announcement(message).post()
            }
    }
}

extension Binding where Value == Bool {
    /// `true` while `value` is non-nil. Setting `false` clears it. For alerts and
    /// confirmation dialogs that present an optional item.
    init<Wrapped>(presenting value: Binding<Wrapped?>) {
        self.init(
            get: { value.wrappedValue != nil },
            set: { presented in
                if !presented { value.wrappedValue = nil }
            })
    }
}

/// Sample data for SwiftUI previews. Not DEBUG-only because `#Preview` bodies are
/// type-checked in Release builds too.
enum HouseholdPreviewData {
    static let user = UserSummary(
        id: "user-ada", displayName: "Ada Lovelace", primaryEmail: "ada@example.com", createdAt: .now)

    static let household = Household(
        id: "household-1", name: "The Lovelace Kitchen", defaultServings: 4, timeZone: "America/Denver",
        createdBy: "user-ada", createdAt: .now, updatedAt: .now)

    static let detail = HouseholdDetail(
        household: household,
        members: [
            HouseholdMember(userID: "user-ada", displayName: "Ada Lovelace", role: .admin, joinedAt: .now),
            HouseholdMember(userID: "user-charles", displayName: "Charles Babbage", role: .member, joinedAt: .now),
            HouseholdMember(userID: "user-anon", displayName: "", role: .member, joinedAt: .now),
        ],
        role: .admin,
        permissions: Array(HouseholdPermission.allKnown))

    static let invitations = [
        HouseholdInvitation(
            id: "invitation-1", email: "mary@example.com", role: .member,
            expiresAt: .now.addingTimeInterval(6 * 24 * 3600), createdAt: .now)
    ]

    static func session() -> AuthSession {
        .preview(.signedIn(user))
    }

    static func store(session: AuthSession) -> HouseholdStore {
        .preview(
            session: session,
            phase: .ready,
            current: detail,
            households: [HouseholdListItem(household: household, role: .admin, permissions: detail.permissions)],
            invitations: invitations)
    }
}
