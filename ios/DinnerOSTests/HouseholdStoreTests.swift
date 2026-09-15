import Foundation
import Testing

@testable import DinnerOS

struct HouseholdStoreTests {
    private struct Harness {
        let store: HouseholdStore
        let session: AuthSession
        let selection: InMemoryHouseholdSelection
        let server: FakeHouseholdServer
        let transport: StubTransport
    }

    private func makeHarness(
        server: FakeHouseholdServer = FakeHouseholdServer(),
        signedIn: Bool = true,
        selection: InMemoryHouseholdSelection = InMemoryHouseholdSelection()
    ) async throws -> Harness {
        let transport = StubTransport { request in
            if request.url?.path() == "/api/v1/auth/google" {
                return (200, Fixtures.sessionJSON(access: "access-1", refresh: "refresh-1"))
            }
            return server.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = signedIn ? StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user) : nil
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let store = HouseholdStore(session: session, api: HouseholdsAPI(client: client), selection: selection)
        return Harness(store: store, session: session, selection: selection, server: server, transport: transport)
    }

    private let userID = Fixtures.user.id

    // MARK: Loading

    @Test func loadWhileSignedOutDoesNothing() async throws {
        let harness = try await makeHarness(signedIn: false)

        await harness.store.load()

        #expect(harness.store.phase == .idle)
        #expect(harness.transport.requests.isEmpty)
    }

    @Test func noHouseholdThenCreateThenLoaded() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        await store.load()
        #expect(store.phase == .needsHousehold)
        #expect(store.current == nil)
        #expect(harness.selection.selectedHouseholdID(for: userID) == nil)

        try await store.createHousehold(name: "The Lovelace Kitchen", timeZone: "America/Denver")

        #expect(store.phase == .ready)
        #expect(store.current?.household.name == "The Lovelace Kitchen")
        #expect(store.current?.role == .admin)
        #expect(store.access?.can(.membersInvite) == true)
        #expect(store.invitations.map(\.email) == ["grace@example.com"])
        #expect(store.households.map(\.id) == ["household-created"])
        #expect(harness.selection.selectedHouseholdID(for: userID) == "household-created")
        let create = try #require(
            harness.transport.requests(to: "/api/v1/households").first { $0.httpMethod == "POST" })
        #expect(create.jsonBody == ["name": "The Lovelace Kitchen", "timeZone": "America/Denver"])
        #expect(create.bearerToken == "access-1")
    }

    @Test func restoresStoredSelection() async throws {
        let server = FakeHouseholdServer(
            .init(memberships: [
                .init(householdID: "household-1", name: "First", role: "admin"),
                .init(householdID: "household-2", name: "Second", role: "member"),
            ]))
        let selection = InMemoryHouseholdSelection(selections: [Fixtures.user.id: "household-2"])
        let harness = try await makeHarness(server: server, selection: selection)

        await harness.store.load()

        #expect(harness.store.phase == .ready)
        #expect(harness.store.current?.household.name == "Second")
        // A member lacks members.invite, so invitations aren't requested.
        #expect(harness.transport.requests(to: "/api/v1/households/household-2/invitations").isEmpty)
        #expect(harness.store.invitations.isEmpty)
    }

    @Test func staleSelectionFallsBackToFirstHousehold() async throws {
        let server = FakeHouseholdServer(
            .init(memberships: [.init(householdID: "household-1", name: "First", role: "admin")]))
        let selection = InMemoryHouseholdSelection(selections: [Fixtures.user.id: "household-gone"])
        let harness = try await makeHarness(server: server, selection: selection)

        await harness.store.load()

        #expect(harness.store.current?.household.id == "household-1")
        #expect(selection.selectedHouseholdID(for: Fixtures.user.id) == "household-1")
    }

    @Test func selectingAnotherHouseholdPersistsIt() async throws {
        let server = FakeHouseholdServer(
            .init(memberships: [
                .init(householdID: "household-1", name: "First", role: "admin"),
                .init(householdID: "household-2", name: "Second", role: "member"),
            ]))
        let harness = try await makeHarness(server: server)
        await harness.store.load()

        await harness.store.selectHousehold(id: "household-2")

        #expect(harness.store.current?.household.name == "Second")
        #expect(harness.selection.selectedHouseholdID(for: userID) == "household-2")
    }

    @Test func offlineFirstLoadFails() async throws {
        let transport = StubTransport { _ in throw URLError(.notConnectedToInternet) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let store = HouseholdStore(
            session: session, api: HouseholdsAPI(client: client), selection: InMemoryHouseholdSelection())

        await store.load()

        guard case .failed(let message) = store.phase else {
            Issue.record("expected failed, got \(store.phase)")
            return
        }
        #expect(message.localizedCaseInsensitiveContains("offline"))
    }

    // MARK: Joining

    @Test func acceptByCodeNormalizesAndSelectsHousehold() async throws {
        let harness = try await makeHarness()
        await harness.store.load()

        let household = try await harness.store.acceptInvitation(code: " abcde-12345\n")

        #expect(household.name == "Babbage House")
        #expect(harness.store.phase == .ready)
        #expect(harness.store.current?.role == .member)
        #expect(harness.store.access?.invitableRoles == [])
        #expect(harness.selection.selectedHouseholdID(for: userID) == "household-joined")
        let accept = try #require(harness.transport.requests(to: "/api/v1/invitations/accept").first)
        #expect(accept.jsonBody == ["code": "ABCDE12345"])
    }

    @Test func invalidCodeThrowsAndKeepsOnboarding() async throws {
        let harness = try await makeHarness()
        await harness.store.load()

        await #expect(throws: APIError.self) {
            try await harness.store.acceptInvitation(code: "ZZZZZ-ZZZZZ")
        }
        #expect(harness.store.phase == .needsHousehold)
        #expect(harness.selection.selectedHouseholdID(for: userID) == nil)
    }

    // MARK: Leaving

    @Test func leaveClearsSelection() async throws {
        let server = FakeHouseholdServer(
            .init(memberships: [.init(householdID: "household-1", name: "First", role: "member")]))
        let harness = try await makeHarness(server: server)
        await harness.store.load()
        #expect(harness.selection.selectedHouseholdID(for: userID) == "household-1")

        try await harness.store.leaveHousehold()

        let leave = try #require(harness.transport.requests.first { $0.httpMethod == "DELETE" })
        #expect(leave.url?.path() == "/api/v1/households/household-1/members/\(userID)")
        #expect(harness.selection.selectedHouseholdID(for: userID) == nil)
        #expect(harness.store.phase == .needsHousehold)
        #expect(harness.store.current == nil)
    }

    @Test func leaveAsLastAdminSurfacesConflictAndKeepsHousehold() async throws {
        let server = FakeHouseholdServer(
            .init(
                memberships: [.init(householdID: "household-1", name: "First", role: "admin")],
                rejectLeaveAsLastAdmin: true))
        let harness = try await makeHarness(server: server)
        await harness.store.load()
        #expect(harness.store.isOnlyAdmin == true)

        do {
            try await harness.store.leaveHousehold()
            Issue.record("expected last_admin")
        } catch let error as APIError {
            #expect(error.status == 409)
            #expect(error.code == "last_admin")
            #expect(HouseholdStore.message(for: error).localizedCaseInsensitiveContains("admin"))
        }
        #expect(harness.store.phase == .ready)
        #expect(harness.store.current?.household.id == "household-1")
        #expect(harness.selection.selectedHouseholdID(for: userID) == "household-1")
    }

    // MARK: Invitation links

    @Test func linkWhileSignedOutIsAcceptedAfterSignInAndConfirmation() async throws {
        let harness = try await makeHarness(signedIn: false)
        let store = harness.store
        let url = try #require(URL(string: "dinneros://invite?token=link-token-abc"))

        #expect(store.handleOpenURL(url))
        #expect(store.hasPendingInvite)
        #expect(store.inviteStatus == nil)
        #expect(harness.transport.requests.isEmpty)

        try await harness.session.signIn { api in
            try await api.signInWithGoogle(idToken: "google.jwt", rawNonce: "raw")
        }
        await store.load()
        #expect(store.inviteStatus == .awaitingConfirmation)
        #expect(harness.transport.requests(to: "/api/v1/invitations/accept").isEmpty)

        let task = try #require(store.confirmPendingInvite())
        #expect(store.inviteStatus == .accepting)
        await task.value

        #expect(store.inviteStatus == .joined(householdName: "Babbage House"))
        #expect(!store.hasPendingInvite)
        #expect(store.phase == .ready)
        let accept = try #require(harness.transport.requests(to: "/api/v1/invitations/accept").first)
        #expect(accept.jsonBody == ["token": "link-token-abc"])
        #expect(accept.url?.query() == nil)

        store.dismissInviteStatus()
        #expect(store.inviteStatus == nil)
    }

    @Test func linkWhileSignedInPromptsImmediately() async throws {
        let harness = try await makeHarness()
        await harness.store.load()

        harness.store.handleOpenURL(try #require(URL(string: "dinneros://invite?token=unknown")))
        #expect(harness.store.inviteStatus == .awaitingConfirmation)

        await harness.store.confirmPendingInvite()?.value

        guard case .failed(let message) = harness.store.inviteStatus else {
            Issue.record("expected failed, got \(String(describing: harness.store.inviteStatus))")
            return
        }
        #expect(message.localizedCaseInsensitiveContains("invitation"))
        #expect(harness.store.phase == .needsHousehold)
    }

    @Test func declinedLinkIsDiscarded() async throws {
        let harness = try await makeHarness()
        await harness.store.load()
        harness.store.handleOpenURL(try #require(URL(string: "dinneros://invite?token=link-token-abc")))

        harness.store.declinePendingInvite()

        #expect(harness.store.inviteStatus == nil)
        #expect(!harness.store.hasPendingInvite)
        #expect(harness.store.confirmPendingInvite() == nil)
    }

    @Test func pendingLinkSurvivesSignOutReset() async throws {
        let harness = try await makeHarness(signedIn: false)
        harness.store.handleOpenURL(try #require(URL(string: "dinneros://invite?token=link-token-abc")))

        harness.store.reset()

        #expect(harness.store.hasPendingInvite)
    }

    @Test func linkWithoutTokenReportsFailureAndOtherURLsAreIgnored() async throws {
        let harness = try await makeHarness()

        #expect(harness.store.handleOpenURL(try #require(URL(string: "dinneros://invite"))))
        guard case .failed = harness.store.inviteStatus else {
            Issue.record("expected failed, got \(String(describing: harness.store.inviteStatus))")
            return
        }
        #expect(!harness.store.hasPendingInvite)

        #expect(!harness.store.handleOpenURL(try #require(URL(string: "https://example.com/invite?token=x"))))
    }
}

struct HouseholdAccessTests {
    private let me = "user-me"

    private func member(_ id: String, _ role: HouseholdRole) -> HouseholdMember {
        HouseholdMember(userID: id, displayName: id, role: role, joinedAt: .now)
    }

    private var admin: HouseholdAccess {
        HouseholdAccess(role: .admin, permissions: HouseholdPermission.allKnown)
    }

    private var plainMember: HouseholdAccess {
        HouseholdAccess(role: .member, permissions: HouseholdRole.member.mirroredPermissions ?? [])
    }

    @Test func adminCanGrantEveryKnownRole() {
        #expect(admin.invitableRoles == [.admin, .member])
        #expect(admin.assignableRoles(for: member("u2", .member), currentUserID: me) == [.admin])
        #expect(admin.assignableRoles(for: member("u3", .admin), currentUserID: me) == [.member])
        #expect(admin.canRemove(member("u2", .member), currentUserID: me))
        #expect(admin.canRemove(member("u3", .admin), currentUserID: me))
    }

    @Test func nobodyManagesThemselvesFromTheMemberList() {
        #expect(admin.assignableRoles(for: member(me, .admin), currentUserID: me).isEmpty)
        #expect(!admin.canRemove(member(me, .admin), currentUserID: me))
    }

    @Test func memberGetsNoManagementActions() {
        #expect(plainMember.invitableRoles.isEmpty)
        #expect(plainMember.assignableRoles(for: member("u2", .member), currentUserID: me).isEmpty)
        #expect(!plainMember.canRemove(member("u2", .member), currentUserID: me))
        #expect(!plainMember.can(.householdUpdate))
        #expect(plainMember.can(.membersView))
    }

    @Test func grantingIsLimitedToCoveredRoles() {
        // A role that can invite but lacks admin's other permissions may only invite members.
        var permissions = HouseholdRole.member.mirroredPermissions ?? []
        permissions.formUnion([.membersInvite, .membersRemove, .membersChangeRole])
        let inviter = HouseholdAccess(role: HouseholdRole(rawValue: "organizer"), permissions: permissions)

        #expect(inviter.invitableRoles == [.member])
        #expect(inviter.assignableRoles(for: member("u2", .member), currentUserID: me).isEmpty)
        #expect(!inviter.canRemove(member("u3", .admin), currentUserID: me))
        #expect(inviter.canRemove(member("u2", .member), currentUserID: me))
    }

    @Test func unknownRolesAreNeverCovered() {
        let stranger = member("u9", HouseholdRole(rawValue: "guest"))
        #expect(!admin.covers(stranger.role))
        #expect(!admin.canRemove(stranger, currentUserID: me))
        #expect(admin.assignableRoles(for: stranger, currentUserID: me).isEmpty)
    }
}

struct HouseholdSelectionStorageTests {
    @Test func userDefaultsSelectionIsPerUserAndClearable() throws {
        let suiteName = "HouseholdSelectionStorageTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suiteName))
        defer { defaults.removePersistentDomain(forName: suiteName) }
        let storage = UserDefaultsHouseholdSelection(defaults: defaults)

        storage.setSelectedHouseholdID("household-1", for: "user-a")
        #expect(storage.selectedHouseholdID(for: "user-a") == "household-1")
        #expect(storage.selectedHouseholdID(for: "user-b") == nil)

        storage.setSelectedHouseholdID(nil, for: "user-a")
        #expect(storage.selectedHouseholdID(for: "user-a") == nil)
        #expect(defaults.object(forKey: UserDefaultsHouseholdSelection.key(for: "user-a")) == nil)
    }
}
