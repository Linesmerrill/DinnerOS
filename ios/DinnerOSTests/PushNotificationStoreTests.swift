import Foundation
import Testing

@testable import DinnerOS

/// A push platform that answers from fixed values and records registrations.
private final class FakePushPlatform: PushPlatform {
    var status: PushAuthorization
    var grants: Bool
    private(set) var prompts = 0
    private(set) var registrations = 0

    init(status: PushAuthorization, grants: Bool = true) {
        self.status = status
        self.grants = grants
    }

    func authorizationStatus() async -> PushAuthorization { status }

    func requestAuthorization() async throws -> Bool {
        prompts += 1
        status = grants ? .authorized : .denied
        return grants
    }

    func registerForRemoteNotifications() {
        registrations += 1
    }
}

struct PushNotificationStoreTests {
    private static let tokenA = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    private static let tokenB = "b1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    private static let path = "/api/v1/me/device-tokens"

    private struct Harness {
        let session: AuthSession
        let store: PushNotificationStore
        let platform: FakePushPlatform
        let storage: InMemoryPushRegistration
        let transport: StubTransport
    }

    private func makeHarness(
        status: PushAuthorization = .notDetermined, grants: Bool = true,
        stored registration: StoredPushRegistration? = nil,
        handler: StubTransport.Handler? = nil
    ) async throws -> Harness {
        let transport = StubTransport(
            handler ?? { request in
                if request.url?.path() == "/api/v1/me/device-tokens", request.httpMethod == "PUT" {
                    return (
                        200,
                        Data(
                            #"{"token":"t","environment":"sandbox","platform":"ios","createdAt":"2026-09-16T12:00:00Z","updatedAt":"2026-09-16T12:00:00Z"}"#
                                .utf8)
                    )
                }
                return (204, Data())
            })
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let platform = FakePushPlatform(status: status, grants: grants)
        let storage = InMemoryPushRegistration(registration: registration)
        let store = PushNotificationStore(
            session: session, api: DeviceTokensAPI(client: client), platform: platform, storage: storage,
            environment: .sandbox)
        session.willSignOut = { [store] in await store.unregisterForSignOut() }
        return Harness(session: session, store: store, platform: platform, storage: storage, transport: transport)
    }

    @Test func refreshNeverPromptsAndRegistersOnlyWhenAllowed() async throws {
        let undecided = try await makeHarness(status: .notDetermined)
        await undecided.store.refresh()
        #expect(undecided.platform.prompts == 0)
        #expect(undecided.platform.registrations == 0)
        #expect(undecided.store.authorization == .notDetermined)

        let allowed = try await makeHarness(status: .authorized)
        await allowed.store.refresh()
        #expect(allowed.platform.registrations == 1)
        #expect(allowed.store.authorization == .authorized)
    }

    @Test func requestAsksOnceThenRegisters() async throws {
        let harness = try await makeHarness(status: .notDetermined)

        #expect(await harness.store.requestAuthorizationIfNeeded())
        #expect(harness.platform.prompts == 1)
        #expect(harness.platform.registrations == 1)

        #expect(await harness.store.requestAuthorizationIfNeeded())
        #expect(harness.platform.prompts == 1)
    }

    @Test func aRefusalIsNotAskedAgainAndRegistersNothing() async throws {
        let harness = try await makeHarness(status: .notDetermined, grants: false)

        #expect(await harness.store.requestAuthorizationIfNeeded() == false)
        #expect(await harness.store.requestAuthorizationIfNeeded() == false)

        #expect(harness.platform.prompts == 1)
        #expect(harness.platform.registrations == 0)
        #expect(harness.store.authorization == .denied)
    }

    @Test func deviceTokenIsSentAsHexWithTheBuildEnvironmentOncePerLaunch() async throws {
        let harness = try await makeHarness(status: .authorized)
        let token = Data([0xA1, 0xB2, 0x03, 0xFF])

        await harness.store.didRegister(deviceToken: token)
        await harness.store.didRegister(deviceToken: token)

        let puts = harness.transport.requests(to: Self.path)
        #expect(puts.count == 1)
        let request = try #require(puts.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.bearerToken == "access-1")
        #expect(request.jsonBody == ["token": "a1b203ff", "environment": "sandbox", "platform": "ios"])
        #expect(harness.storage.registration == StoredPushRegistration(token: "a1b203ff", userID: Fixtures.user.id))
    }

    @Test func aRotatedTokenReplacesThePreviousOne() async throws {
        let harness = try await makeHarness(
            status: .authorized, stored: StoredPushRegistration(token: Self.tokenA, userID: Fixtures.user.id))

        await harness.store.register(token: Self.tokenB)

        let requests = harness.transport.requests(to: Self.path)
        #expect(requests.map(\.httpMethod) == ["PUT", "DELETE"])
        #expect(requests.first?.jsonBody?["token"] == Self.tokenB)
        #expect(requests.last?.jsonBody == ["token": Self.tokenA])
        #expect(harness.storage.registration?.token == Self.tokenB)
    }

    @Test func aFailedRegistrationIsRetriedOnTheNextCallback() async throws {
        let failures = Counter()
        let harness = try await makeHarness(status: .authorized) { request in
            if failures.value == 0 {
                failures.increment()
                return (500, Fixtures.errorJSON(code: "internal"))
            }
            return (200, Data(#"{"token":"t","environment":"sandbox","platform":"ios"}"#.utf8))
        }

        await harness.store.register(token: Self.tokenA)
        #expect(harness.storage.registration == nil)

        await harness.store.register(token: Self.tokenA)
        #expect(harness.transport.requests(to: Self.path).count == 2)
        #expect(harness.storage.registration?.token == Self.tokenA)
    }

    @Test func signOutDeletesThisDevicesTokenWhileStillAuthorized() async throws {
        let harness = try await makeHarness(status: .authorized)
        await harness.store.register(token: Self.tokenA)

        await harness.session.signOut()

        let deletes = harness.transport.requests(to: Self.path).filter { $0.httpMethod == "DELETE" }
        #expect(deletes.count == 1)
        #expect(deletes.first?.jsonBody == ["token": Self.tokenA])
        #expect(deletes.first?.bearerToken == "access-1")
        #expect(harness.storage.registration == nil)
        #expect(harness.session.state == .signedOut)
        // The delete went out before the logout that revokes the tokens.
        let paths = harness.transport.requests.compactMap { $0.url?.path() }
        #expect(paths.firstIndex(of: Self.path) ?? .max < paths.firstIndex(of: "/api/v1/auth/logout") ?? -1)
    }

    @Test func signOutDoesNotWaitOnASlowServer() async throws {
        let harness = try await makeHarness(status: .authorized) { request in
            if request.httpMethod == "DELETE" {
                try await Task.sleep(for: .seconds(30))
            }
            return (200, Data(#"{"token":"t","environment":"sandbox","platform":"ios"}"#.utf8))
        }
        await harness.store.register(token: Self.tokenA)

        let started = ContinuousClock.now
        await harness.session.signOut()

        // The server would take 30s. Well under that proves sign-out didn't wait for it;
        // the bound is loose because parallel tests on a CI runner stall the clock.
        #expect(ContinuousClock.now - started < .seconds(20))
        #expect(harness.session.state == .signedOut)
    }

    @Test func signOutWithoutARegistrationSendsNoDelete() async throws {
        let harness = try await makeHarness(status: .denied)

        await harness.session.signOut()

        #expect(harness.transport.requests(to: Self.path).isEmpty)
    }

    @Test func tappedPushWaitsUntilOpened() async throws {
        let harness = try await makeHarness()
        let route = try #require(
            PushRoute(userInfo: [
                "aps": ["alert": ["title": "Time to order"]],
                "notificationId": "n-1", "householdId": "household-2", "type": "shopping.order_due",
                "subject": ["kind": "shopping_week", "id": "2026-W38"],
            ]))

        harness.store.open(route)
        #expect(harness.store.pendingRoute == route)

        harness.store.finishOpening(route)
        #expect(harness.store.pendingRoute == nil)
    }

    @Test func routeParsesTheAPIPayload() throws {
        let order = try #require(
            PushRoute(userInfo: [
                "notificationId": "n-1", "householdId": "household-2", "type": "shopping.order_due",
                "subject": ["kind": "shopping_week", "id": "2026-W38"],
            ]))
        #expect(order.type == .shoppingOrderDue)
        #expect(order.subject?.shoppingWeek == "2026-W38")
        #expect(order.subject?.pantryItemID == nil)

        let low = try #require(
            PushRoute(userInfo: [
                "notificationId": "n-2", "householdId": "household-1", "type": "pantry.low",
                "subject": ["kind": "pantry_item", "id": "item-butter"],
            ]))
        #expect(low.subject?.pantryItemID == "item-butter")

        // Another app's payload, or one missing its household, opens nothing.
        #expect(PushRoute(userInfo: ["aps": ["alert": "hi"]]) == nil)
        #expect(PushRoute(userInfo: ["notificationId": "n-3"]) == nil)
        #expect(PushRoute(userInfo: ["notificationId": "n-3", "householdId": "h"])?.subject == nil)
    }

    @Test func tokenHexIsLowercaseAndPadded() {
        #expect(Data([0x00, 0x0A, 0xFF]).pushTokenHex == "000aff")
    }

    @Test func buildEnvironmentMatchesTheEntitlement() {
        // Unit tests run the Debug configuration, whose aps-environment is development.
        #expect(PushEnvironment.current == .sandbox)
    }
}
