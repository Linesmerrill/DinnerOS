import Foundation
import Testing

@testable import DinnerOS

struct HouseholdsAPITests {
    private func makeAPI(_ transport: StubTransport) throws -> HouseholdsAPI {
        HouseholdsAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private func jsonObject(_ request: URLRequest?) -> [String: Any]? {
        guard let body = request?.httpBody else { return nil }
        return (try? JSONSerialization.jsonObject(with: body)) as? [String: Any]
    }

    @Test func createHouseholdPostsNameAndTimeZone() async throws {
        let transport = StubTransport { _ in (201, HouseholdFixtures.created(id: "household-1", name: "Lovelace")) }

        let response = try await makeAPI(transport).createHousehold(
            name: "Lovelace", timeZone: "America/Denver", defaultServings: nil, accessToken: "token-1")

        #expect(response.household.id == "household-1")
        #expect(response.membership.role == .admin)
        #expect(response.membership.householdID == "household-1")
        #expect(Set(response.membership.permissions) == HouseholdPermission.allKnown)
        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households")
        #expect(request.bearerToken == "token-1")
        // `defaultServings` is omitted, not sent as null.
        #expect(request.jsonBody == ["name": "Lovelace", "timeZone": "America/Denver"])
    }

    @Test func listHouseholdsDecodesRoleAndPermissions() async throws {
        let transport = StubTransport { _ in
            (
                200,
                HouseholdFixtures.list([
                    HouseholdFixtures.listItem(id: "household-1", name: "One", role: "admin"),
                    HouseholdFixtures.listItem(id: "household-2", name: "Two", role: "member"),
                ])
            )
        }

        let items = try await makeAPI(transport).listHouseholds(accessToken: "token-1")

        #expect(items.map(\.id) == ["household-1", "household-2"])
        #expect(items.map(\.role) == [.admin, .member])
        #expect(!items[1].permissions.contains(.membersInvite))
        #expect(transport.requests.first?.httpMethod == "GET")
        #expect(transport.requests.first?.bearerToken == "token-1")
    }

    @Test func householdDetailDecodesMembers() async throws {
        let transport = StubTransport { _ in
            (
                200,
                HouseholdFixtures.detail(
                    id: "household-1", name: "One", role: "admin",
                    members: [
                        HouseholdFixtures.member(role: "admin"),
                        HouseholdFixtures.member(userID: "user-2", name: "", role: "member"),
                    ])
            )
        }

        let detail = try await makeAPI(transport).household(id: "household-1", accessToken: "token-1")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1")
        #expect(detail.members?.map(\.userID) == [Fixtures.user.id, "user-2"])
        #expect(detail.members?.last?.name == String(localized: "Unnamed member"))
        #expect(detail.access.can(.householdUpdate))
    }

    @Test func householdDetailWithoutMembersField() async throws {
        let body = Data(
            #"{"household":\#(HouseholdFixtures.household(id: "h", name: "H")),"role":"viewer","permissions":["household.view"]}"#
                .utf8)
        let transport = StubTransport { _ in (200, body) }

        let detail = try await makeAPI(transport).household(id: "h", accessToken: "t")

        #expect(detail.members == nil)
        // A role this build doesn't know decodes, and grants nothing beyond its permissions.
        #expect(detail.role == HouseholdRole(rawValue: "viewer"))
        #expect(detail.role.displayName == "Viewer")
        #expect(detail.access.invitableRoles.isEmpty)
    }

    @Test func updateHouseholdPatchesOnlyChangedFields() async throws {
        let transport = StubTransport { _ in (200, Data(HouseholdFixtures.household(id: "h", name: "New").utf8)) }

        let household = try await makeAPI(transport).updateHousehold(
            id: "h", changes: HouseholdChanges(defaultServings: 4), accessToken: "t")

        #expect(household.name == "New")
        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PATCH")
        #expect(request.url?.path() == "/api/v1/households/h")
        let body = try #require(jsonObject(request))
        #expect(body.count == 1)
        #expect(body["defaultServings"] as? Int == 4)
    }

    @Test func changeRolePatchesMember() async throws {
        let transport = StubTransport { _ in
            (200, Data(HouseholdFixtures.member(userID: "user-2", name: "Charles", role: "admin").utf8))
        }

        let member = try await makeAPI(transport).changeRole(
            householdID: "h", userID: "user-2", to: .admin, accessToken: "t")

        #expect(member.role == .admin)
        #expect(member.userID == "user-2")
        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PATCH")
        #expect(request.url?.path() == "/api/v1/households/h/members/user-2")
        #expect(request.jsonBody == ["role": "admin"])
    }

    @Test func removeMemberSendsDeleteWithoutBody() async throws {
        let transport = StubTransport { _ in (204, Data()) }

        try await makeAPI(transport).removeMember(householdID: "h", userID: "user-2", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "DELETE")
        #expect(request.url?.path() == "/api/v1/households/h/members/user-2")
        #expect(request.httpBody == nil)
        #expect(request.bearerToken == "t")
    }

    @Test func createInvitationReturnsCodeAndDeliveryStatus() async throws {
        let transport = StubTransport { _ in
            (
                201,
                Data(
                    #"{"invitation":\#(HouseholdFixtures.invitation()),"code":"7K2QX-M9D4P","emailDelivered":false}"#
                        .utf8)
            )
        }

        let response = try await makeAPI(transport).createInvitation(
            householdID: "h", email: "grace@example.com", role: .member, accessToken: "t")

        #expect(response.code == "7K2QX-M9D4P")
        #expect(!response.emailDelivered)
        #expect(response.invitation.email == "grace@example.com")
        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/h/invitations")
        #expect(request.jsonBody == ["email": "grace@example.com", "role": "member"])
    }

    @Test func pendingInvitationsAndRevoke() async throws {
        let transport = StubTransport { request in
            request.httpMethod == "DELETE"
                ? (204, Data())
                : (200, HouseholdFixtures.list([HouseholdFixtures.invitation(role: "admin")]))
        }
        let api = try makeAPI(transport)

        let invitations = try await api.pendingInvitations(householdID: "h", accessToken: "t")
        try await api.revokeInvitation(householdID: "h", invitationID: invitations[0].id, accessToken: "t")

        #expect(invitations.map(\.role) == [.admin])
        #expect(transport.requests.map(\.httpMethod) == ["GET", "DELETE"])
        #expect(
            transport.requests.map { $0.url?.path() } == [
                "/api/v1/households/h/invitations", "/api/v1/households/h/invitations/invitation-1",
            ])
    }

    @Test(arguments: [
        (InvitationSecret.token("link-token"), ["token": "link-token"]),
        (InvitationSecret.code("ABCDE12345"), ["code": "ABCDE12345"]),
    ])
    func acceptSendsExactlyOneSecretInTheBody(secret: InvitationSecret, expectedBody: [String: String]) async throws {
        let transport = StubTransport { _ in
            (200, HouseholdFixtures.accepted(id: "h", name: "Babbage House", role: "member"))
        }

        let response = try await makeAPI(transport).acceptInvitation(secret, accessToken: "t")

        #expect(response.household.name == "Babbage House")
        #expect(response.role == .member)
        let request = try #require(transport.requests.first)
        #expect(request.url?.path() == "/api/v1/invitations/accept")
        #expect(request.url?.query() == nil)
        #expect(request.jsonBody == expectedBody)
    }
}

struct HouseholdErrorMappingTests {
    @Test(arguments: [
        (403, "forbidden", "role"),
        (404, "not_found", "no longer"),
        (409, "last_admin", "admin"),
        (404, "invitation_invalid", "invitation"),
        (409, "conflict", "same time"),
    ])
    func mapsHouseholdErrorCodes(status: Int, code: String, expectedPhrase: String) async throws {
        let transport = StubTransport { _ in (status, Fixtures.errorJSON(code: code, message: "server text")) }
        let api = HouseholdsAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))

        do {
            try await api.removeMember(householdID: "h", userID: "u", accessToken: "t")
            Issue.record("expected an error")
        } catch let error as APIError {
            #expect(error.status == status)
            #expect(error.code == code)
            let description = try #require(error.errorDescription)
            #expect(description.localizedCaseInsensitiveContains(expectedPhrase))
            #expect(!description.contains("server text"))
        }
    }

    @Test func validationMessagesFromTheServerAreShown() {
        let error = APIError(
            status: 400, body: Fixtures.errorJSON(code: "validation_failed", message: "name is required"),
            fallbackRequestID: nil)
        #expect(error.errorDescription == "name is required")
    }
}
