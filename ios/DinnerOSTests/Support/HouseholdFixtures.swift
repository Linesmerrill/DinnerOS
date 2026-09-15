import Foundation
import Synchronization

@testable import DinnerOS

/// JSON shaped like the API's household and invitation responses.
nonisolated enum HouseholdFixtures {
    static let adminPermissions = [
        "household.view", "household.update", "members.view", "members.invite", "members.remove",
        "members.changeRole", "plan.edit", "pantry.edit", "recipes.edit", "recipes.import", "shopping.edit",
    ]
    static let memberPermissions = [
        "household.view", "members.view", "plan.edit", "pantry.edit", "recipes.edit", "recipes.import",
        "shopping.edit",
    ]

    static func permissions(for role: String) -> String {
        let list = role == "admin" ? adminPermissions : memberPermissions
        return "[" + list.map { "\"\($0)\"" }.joined(separator: ",") + "]"
    }

    static func household(id: String, name: String) -> String {
        #"""
        {"id":"\#(id)","name":"\#(name)","defaultServings":2,"timeZone":"America/Denver",
         "createdBy":"\#(Fixtures.user.id)","createdAt":"2026-09-14T18:30:00.123456789Z",
         "updatedAt":"2026-09-14T18:30:00Z"}
        """#
    }

    static func member(userID: String = Fixtures.user.id, name: String = "Ada Lovelace", role: String) -> String {
        #"{"userId":"\#(userID)","displayName":"\#(name)","role":"\#(role)","joinedAt":"2026-09-14T18:30:00Z"}"#
    }

    static func listItem(id: String, name: String, role: String) -> String {
        #"{"household":\#(household(id: id, name: name)),"role":"\#(role)","permissions":\#(permissions(for: role))}"#
    }

    static func list(_ items: [String]) -> Data {
        Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8)
    }

    static func detail(id: String, name: String, role: String, members: [String]? = nil) -> Data {
        let membersField = (members ?? [member(role: role)]).joined(separator: ",")
        return Data(
            #"""
            {"household":\#(household(id: id, name: name)),"members":[\#(membersField)],
             "role":"\#(role)","permissions":\#(permissions(for: role))}
            """#.utf8)
    }

    static func created(id: String, name: String) -> Data {
        Data(
            #"""
            {"household":\#(household(id: id, name: name)),
             "membership":{"id":"membership-1","householdId":"\#(id)","userId":"\#(Fixtures.user.id)",
                           "role":"admin","permissions":\#(permissions(for: "admin")),
                           "createdAt":"2026-09-14T18:30:00Z","updatedAt":"2026-09-14T18:30:00Z"}}
            """#.utf8)
    }

    static func accepted(id: String, name: String, role: String) -> Data {
        Data(
            #"{"household":\#(household(id: id, name: name)),"role":"\#(role)","permissions":\#(permissions(for: role))}"#
                .utf8)
    }

    static func preview(
        householdName: String = "Babbage House", inviterName: String = "Charles Babbage", role: String = "member"
    ) -> Data {
        Data(
            #"""
            {"householdName":"\#(householdName)","inviterName":"\#(inviterName)","role":"\#(role)",
             "expiresAt":"2026-09-21T18:30:00Z"}
            """#.utf8)
    }

    static func invitation(id: String = "invitation-1", email: String = "grace@example.com", role: String = "member")
        -> String
    {
        #"""
        {"id":"\#(id)","email":"\#(email)","role":"\#(role)","expiresAt":"2026-09-21T18:30:00Z",
         "createdAt":"2026-09-14T18:30:00Z"}
        """#
    }
}

/// An in-memory stand-in for the household endpoints, so store tests exercise real
/// state transitions through `APIClient` and `AuthSession`.
nonisolated final class FakeHouseholdServer: Sendable {
    struct Membership: Sendable {
        var householdID: String
        var name: String
        var role: String
    }

    struct State: Sendable {
        var memberships: [Membership] = []
        var validCode = "ABCDE12345"
        var validToken = "link-token-abc"
        /// Answer leaving with `409 last_admin`.
        var rejectLeaveAsLastAdmin = false
    }

    private let state: Mutex<State>

    init(_ initial: State = State()) {
        state = Mutex(initial)
    }

    var memberships: [Membership] {
        state.withLock { $0.memberships }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        let method = request.httpMethod ?? "GET"
        let parts = (request.url?.path() ?? "").split(separator: "/").map(String.init).dropFirst(2)
        let route = Array(parts)

        return state.withLock { state in
            switch (method, route.count) {
            case ("GET", 1) where route == ["households"]:
                return (
                    200,
                    HouseholdFixtures.list(
                        state.memberships.map {
                            HouseholdFixtures.listItem(id: $0.householdID, name: $0.name, role: $0.role)
                        })
                )
            case ("POST", 1) where route == ["households"]:
                let membership = Membership(
                    householdID: "household-created", name: request.jsonBody?["name"] ?? "", role: "admin")
                state.memberships.append(membership)
                return (201, HouseholdFixtures.created(id: membership.householdID, name: membership.name))
            case ("GET", 2) where route[0] == "households":
                guard let membership = state.memberships.first(where: { $0.householdID == route[1] }) else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                return (
                    200,
                    HouseholdFixtures.detail(id: membership.householdID, name: membership.name, role: membership.role)
                )
            case ("GET", 3) where route[0] == "households" && route[2] == "invitations":
                return (200, HouseholdFixtures.list([HouseholdFixtures.invitation()]))
            case ("DELETE", 4) where route[0] == "households" && route[2] == "members":
                if state.rejectLeaveAsLastAdmin {
                    return (409, Fixtures.errorJSON(code: "last_admin", message: "a household must keep an admin"))
                }
                state.memberships.removeAll { $0.householdID == route[1] }
                return (204, Data())
            case ("POST", 2) where route == ["invitations", "preview"]:
                let body = request.jsonBody ?? [:]
                guard body["code"] == state.validCode || body["token"] == state.validToken else {
                    return (404, Fixtures.errorJSON(code: "invitation_invalid"))
                }
                return (200, HouseholdFixtures.preview())
            case ("POST", 2) where route == ["invitations", "accept"]:
                let body = request.jsonBody ?? [:]
                guard body["code"] == state.validCode || body["token"] == state.validToken else {
                    return (404, Fixtures.errorJSON(code: "invitation_invalid"))
                }
                let membership = Membership(householdID: "household-joined", name: "Babbage House", role: "member")
                state.memberships.append(membership)
                return (
                    200,
                    HouseholdFixtures.accepted(id: membership.householdID, name: membership.name, role: "member")
                )
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }
}
