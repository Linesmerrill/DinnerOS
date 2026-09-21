import Foundation

/// A member's role in a household (`Role` in `api/openapi.yaml`).
///
/// A role from a newer server decodes as-is; the app offers no actions for roles it
/// doesn't know, and the server still decides what every role may do.
nonisolated struct HouseholdRole: RawRepresentable, Codable, Hashable, Sendable, Identifiable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let admin = HouseholdRole(rawValue: "admin")
    static let member = HouseholdRole(rawValue: "member")

    /// Roles the app can offer when inviting or changing a role, most privileged first.
    static let assignable: [HouseholdRole] = [.admin, .member]

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .admin: String(localized: "Admin")
        case .member: String(localized: "Member")
        default: rawValue.capitalized
        }
    }

    var summary: String {
        switch self {
        case .admin: String(localized: "Can rename the household, invite people, and manage members.")
        case .member: String(localized: "Can plan meals, edit recipes, and update the pantry.")
        default: String(localized: "Permissions are set by the server.")
        }
    }

    /// A copy of the API's role table (`rolePermissions` in `api/internal/households/roles.go`).
    ///
    /// Used only to decide which roles the UI offers to grant. It never grants access:
    /// the server checks every request against its own table.
    var mirroredPermissions: Set<HouseholdPermission>? {
        switch self {
        case .admin:
            HouseholdPermission.allKnown
        case .member:
            [.householdView, .membersView, .planEdit, .pantryEdit, .recipesEdit, .recipesImport, .shoppingEdit]
        default:
            nil
        }
    }
}

/// An action a household member may be allowed to perform (`Permission`).
nonisolated struct HouseholdPermission: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let householdView = HouseholdPermission(rawValue: "household.view")
    static let householdUpdate = HouseholdPermission(rawValue: "household.update")
    static let membersView = HouseholdPermission(rawValue: "members.view")
    static let membersInvite = HouseholdPermission(rawValue: "members.invite")
    static let membersRemove = HouseholdPermission(rawValue: "members.remove")
    static let membersChangeRole = HouseholdPermission(rawValue: "members.changeRole")
    static let planEdit = HouseholdPermission(rawValue: "plan.edit")
    static let pantryEdit = HouseholdPermission(rawValue: "pantry.edit")
    static let recipesEdit = HouseholdPermission(rawValue: "recipes.edit")
    static let recipesImport = HouseholdPermission(rawValue: "recipes.import")
    /// Store settings, saved provider products, and handing a list off to a store.
    static let shoppingEdit = HouseholdPermission(rawValue: "shopping.edit")

    static let allKnown: Set<HouseholdPermission> = [
        .householdView, .householdUpdate, .membersView, .membersInvite, .membersRemove, .membersChangeRole,
        .planEdit, .pantryEdit, .recipesEdit, .recipesImport, .shoppingEdit,
    ]
}

/// A household (`Household`).
nonisolated struct Household: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    let name: String
    let defaultServings: Int
    /// An IANA time zone name such as `America/Denver`.
    let timeZone: String
    /// The weekday the household means to place its grocery order (`mon`…`sun`), or `nil`
    /// when nobody has picked one, which turns the weekly order reminder off.
    let orderDay: String?
    let createdBy: String
    let createdAt: Date
    let updatedAt: Date
    /// What the household spent on meal kits, to compare grocery cost against; `nil` when not
    /// set, which turns the comparison off.
    var mealKit: MealKitBaseline? = nil
    /// The day the household's weeks start on. Week keys stay ISO weeks; this decides which
    /// seven dates a key covers (`ISOWeek.startDate(weekStartsOn:)`).
    var weekStartsOn: PlanDay = PlanDay.isoWeekStart
    /// The local hour thaw reminders go out (0–23). Always set; a household that never
    /// chose one reads as `Household.defaultThawReminderHour`.
    var thawReminderHour: Int = Household.defaultThawReminderHour

    private enum CodingKeys: String, CodingKey {
        case id, name, defaultServings, timeZone, orderDay, createdBy, createdAt, updatedAt, mealKit, weekStartsOn,
            thawReminderHour
    }
}

nonisolated extension Household {
    /// `weekStartsOn` is additive: a server that doesn't send it, or sends a day this build
    /// doesn't know, lays weeks out Monday to Sunday, the way it stored them.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            name: try container.decode(String.self, forKey: .name),
            defaultServings: try container.decode(Int.self, forKey: .defaultServings),
            timeZone: try container.decode(String.self, forKey: .timeZone),
            orderDay: try container.decodeIfPresent(String.self, forKey: .orderDay),
            createdBy: try container.decode(String.self, forKey: .createdBy),
            createdAt: try container.decode(Date.self, forKey: .createdAt),
            updatedAt: try container.decode(Date.self, forKey: .updatedAt),
            mealKit: try container.decodeIfPresent(MealKitBaseline.self, forKey: .mealKit),
            weekStartsOn: container.decodeLenient(PlanDay.self, forKey: .weekStartsOn) ?? PlanDay.isoWeekStart,
            thawReminderHour: (try? container.decodeIfPresent(Int.self, forKey: .thawReminderHour))
                ?? Household.defaultThawReminderHour)
    }

    /// The hour thaw reminders go out for a household that hasn't chosen one, matching
    /// `households.DefaultThawReminderHour` on the server.
    static let defaultThawReminderHour = 6

    /// The hours the picker offers, and the text for each: moving something to the fridge
    /// is a morning errand, so the list runs across the whole day but reads as a clock.
    static let thawReminderHours = Array(0...23)

    /// `5` as "5:00 AM", in the reader's own locale.
    static func thawHourText(_ hour: Int) -> String {
        var components = DateComponents()
        components.hour = hour
        components.minute = 0
        let calendar = Calendar(identifier: .gregorian)
        guard let date = calendar.date(from: components) else { return "\(hour):00" }
        let formatter = DateFormatter()
        formatter.timeStyle = .short
        formatter.dateStyle = .none
        return formatter.string(from: date)
    }
}

/// A meal kit baseline to save (`PATCH .../households/{id}` `mealKit`).
nonisolated struct MealKitInput: Encodable, Equatable, Sendable {
    static let weeklyCentsRange = 1...MoneyText.maxCents
    static let mealsRange = 1...21

    var weeklyCents: Int
    var meals: Int
}

/// The caller's membership, returned when a household is created (`Membership`).
nonisolated struct HouseholdMembership: Decodable, Equatable, Sendable {
    let id: String
    let householdID: String
    let userID: String
    let role: HouseholdRole
    let permissions: [HouseholdPermission]
    let createdAt: Date
    let updatedAt: Date

    private enum CodingKeys: String, CodingKey {
        case id
        case householdID = "householdId"
        case userID = "userId"
        case role, permissions, createdAt, updatedAt
    }
}

/// One entry of a household's member list (`Member`).
nonisolated struct HouseholdMember: Decodable, Equatable, Sendable, Identifiable {
    let userID: String
    /// May be empty when no provider supplied a name.
    let displayName: String
    let role: HouseholdRole
    let joinedAt: Date

    var id: String { userID }

    /// The display name, or a placeholder when none was provided.
    var name: String {
        displayName.isEmpty ? String(localized: "Unnamed member") : displayName
    }

    private enum CodingKeys: String, CodingKey {
        case userID = "userId"
        case displayName, role, joinedAt
    }
}

/// Response to `POST /api/v1/households`.
nonisolated struct CreateHouseholdResponse: Decodable, Equatable, Sendable {
    let household: Household
    let membership: HouseholdMembership
}

/// One entry of `GET /api/v1/households`.
nonisolated struct HouseholdListItem: Decodable, Equatable, Sendable, Identifiable {
    let household: Household
    let role: HouseholdRole
    let permissions: [HouseholdPermission]

    var id: String { household.id }
}

nonisolated struct HouseholdListResponse: Decodable, Equatable, Sendable {
    let items: [HouseholdListItem]
}

/// Response to `GET /api/v1/households/{householdId}`.
nonisolated struct HouseholdDetail: Decodable, Equatable, Sendable {
    let household: Household
    /// `nil` when the caller's role lacks `members.view`.
    let members: [HouseholdMember]?
    let role: HouseholdRole
    let permissions: [HouseholdPermission]

    var access: HouseholdAccess {
        HouseholdAccess(role: role, permissions: Set(permissions))
    }
}

/// A pending invitation (`Invitation`). Never includes the token or code.
nonisolated struct HouseholdInvitation: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    let email: String
    let role: HouseholdRole
    let expiresAt: Date
    let createdAt: Date
}

nonisolated struct InvitationListResponse: Decodable, Equatable, Sendable {
    let items: [HouseholdInvitation]
}

/// Response to `POST /api/v1/households/{householdId}/invitations`.
nonisolated struct CreateInvitationResponse: Decodable, Equatable, Sendable, Identifiable {
    let invitation: HouseholdInvitation
    /// Shown once so the inviter can share it (`XXXXX-XXXXX`). Never log it.
    let code: String
    let emailDelivered: Bool

    var id: String { invitation.id }
}

/// Response to `POST /api/v1/invitations/accept`.
nonisolated struct AcceptInvitationResponse: Decodable, Equatable, Sendable {
    let household: Household
    let role: HouseholdRole
    let permissions: [HouseholdPermission]
}

/// A partial household update. `nil` fields are omitted from the request and left unchanged.
nonisolated struct HouseholdChanges: Encodable, Equatable, Sendable {
    var name: String?
    var timeZone: String?
    var defaultServings: Int?
    /// A weekday code to remind on, or `""` to turn the order reminder off. `nil` leaves
    /// the household's order day alone.
    var orderDay: String?
    /// The meal kit comparison: `.keep` omits it, `.clear` turns it off.
    var mealKit: FieldChange<MealKitInput> = .keep
    /// The day weeks start on. The server moves meals whose dates now fall in a neighbouring
    /// week into that week; `nil` leaves it alone.
    var weekStartsOn: PlanDay? = nil
    /// The local hour thaw reminders go out (0–23); `nil` leaves it alone.
    var thawReminderHour: Int? = nil

    var isEmpty: Bool {
        name == nil && timeZone == nil && defaultServings == nil && orderDay == nil && mealKit == .keep
            && weekStartsOn == nil && thawReminderHour == nil
    }

    private enum CodingKeys: String, CodingKey {
        case name, timeZone, defaultServings, orderDay, mealKit, weekStartsOn, thawReminderHour
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(name, forKey: .name)
        try container.encodeIfPresent(timeZone, forKey: .timeZone)
        try container.encodeIfPresent(defaultServings, forKey: .defaultServings)
        try container.encodeIfPresent(orderDay, forKey: .orderDay)
        try container.encodeChange(mealKit, forKey: .mealKit)
        try container.encodeIfPresent(weekStartsOn, forKey: .weekStartsOn)
        try container.encodeIfPresent(thawReminderHour, forKey: .thawReminderHour)
    }
}

/// The secret presented to accept an invitation.
nonisolated enum InvitationSecret: Equatable, Sendable {
    /// From an invitation link.
    case token(String)
    /// Typed by the user, normalized with `InviteCode.normalize`.
    case code(String)
}

/// Response to `POST /api/v1/invitations/preview`: what joining means, shown before the
/// user confirms.
nonisolated struct InvitationPreview: Decodable, Equatable, Sendable {
    let householdName: String
    /// Empty or `nil` when the inviter has no display name.
    let inviterName: String?
    let role: HouseholdRole
    let expiresAt: Date

    /// For example, "Join Lines?".
    var joinTitle: String {
        String(localized: "Join \(householdName)?")
    }

    /// For example, "Merrill Lines invited you to join as an admin. Joining shares your
    /// name with its members."
    var joinMessage: String {
        let inviter = inviterName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let role = roleWithArticle
        let invited =
            inviter.isEmpty
            ? String(localized: "You're invited to join as \(role).")
            : String(localized: "\(inviter) invited you to join as \(role).")
        return invited + " " + String(localized: "Joining shares your name with its members.")
    }

    private var roleWithArticle: String {
        switch role {
        case .admin: String(localized: "an admin")
        case .member: String(localized: "a member")
        default: role.rawValue
        }
    }
}
