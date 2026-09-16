import Foundation
import Testing

@testable import DinnerOS

struct TokenStoreTests {
    @Test func inMemoryStoreRoundTrips() throws {
        let store = InMemoryTokenStore()
        #expect(try store.load() == nil)

        let session = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        try store.save(session)
        #expect(try store.load() == session)

        try store.clear()
        #expect(try store.load() == nil)
    }

    @Test func storedSessionSurvivesJSONEncoding() throws {
        let session = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let data = try JSONCoding.makeEncoder().encode(session)
        #expect(try JSONCoding.makeDecoder().decode(StoredSession.self, from: data) == session)
    }

    @Test func accessTokenExpiryUsesLeeway() {
        let now = Date()
        #expect(Fixtures.tokens(accessExpiresIn: 10).isAccessTokenExpired(at: now))
        #expect(!Fixtures.tokens(accessExpiresIn: 120).isAccessTokenExpired(at: now))
    }
}

struct AuthSessionTests {
    private let meRequestPath = "/api/v1/me"
    private let refreshPath = "/api/v1/auth/refresh"
    private let logoutPath = "/api/v1/auth/logout"

    private func makeSession(
        stored: StoredSession?,
        handler: @escaping StubTransport.Handler
    ) throws -> (AuthSession, InMemoryTokenStore, StubTransport) {
        let transport = StubTransport(handler)
        let store = InMemoryTokenStore(session: stored)
        let api = AuthAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
        return (AuthSession(api: api, store: store), store, transport)
    }

    // MARK: Restore

    @Test func missingConfigurationReportsError() async {
        let session = AuthSession(api: nil, store: InMemoryTokenStore())
        await session.restore()
        guard case .configurationError = session.state else {
            Issue.record("expected configurationError, got \(session.state)")
            return
        }
    }

    @Test func restoreWithoutStoredSessionIsSignedOut() async throws {
        let (session, _, transport) = try makeSession(stored: nil) { _ in (500, Data()) }
        await session.restore()
        #expect(session.state == .signedOut)
        #expect(transport.requests.isEmpty)
    }

    @Test func restoreWithValidTokenSignsInWithoutNetwork() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let (session, store, transport) = try makeSession(stored: stored) { _ in (500, Data()) }

        await session.restore()

        #expect(session.state == .signedIn(Fixtures.user))
        #expect(transport.requests.isEmpty)
        #expect(store.session == stored)
    }

    @Test func restoreWithExpiredTokenRefreshes() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(accessExpiresIn: -60), user: Fixtures.user)
        let (session, store, transport) = try makeSession(stored: stored) { _ in
            (200, Fixtures.tokenPairJSON(access: "access-2", refresh: "refresh-2"))
        }

        await session.restore()

        #expect(session.state == .signedIn(Fixtures.user))
        #expect(transport.requests(to: refreshPath).count == 1)
        #expect(transport.requests.first?.jsonBody == ["refreshToken": "refresh-1"])
        #expect(store.session?.tokens.accessToken == "access-2")
        #expect(store.session?.tokens.refreshToken == "refresh-2")
        #expect(store.session?.user == Fixtures.user)
    }

    @Test func restoreWithRejectedRefreshSignsOutAndClearsStore() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(accessExpiresIn: -60), user: Fixtures.user)
        let (session, store, _) = try makeSession(stored: stored) { _ in
            (401, Fixtures.errorJSON(code: "unauthenticated"))
        }

        await session.restore()

        #expect(session.state == .signedOut)
        #expect(store.session == nil)
    }

    @Test func restoreWhileOfflineKeepsCachedSession() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(accessExpiresIn: -60), user: Fixtures.user)
        let (session, store, _) = try makeSession(stored: stored) { _ in throw URLError(.notConnectedToInternet) }

        await session.restore()

        // The refresh token may still be valid; losing it offline would force a new sign-in.
        #expect(session.state == .signedIn(Fixtures.user))
        #expect(store.session == stored)
    }

    @Test func restoreWithExpiredRefreshTokenSignsOutWithoutNetwork() async throws {
        let stored = StoredSession(
            tokens: Fixtures.tokens(accessExpiresIn: -120, refreshExpiresIn: -60), user: Fixtures.user)
        let (session, store, transport) = try makeSession(stored: stored) { _ in (500, Data()) }

        await session.restore()

        #expect(session.state == .signedOut)
        #expect(store.session == nil)
        #expect(transport.requests.isEmpty)
    }

    // MARK: Sign in

    @Test func signInStoresSession() async throws {
        let (session, store, _) = try makeSession(stored: nil) { _ in
            (200, Fixtures.sessionJSON(access: "access-new", refresh: "refresh-new"))
        }
        await session.restore()

        try await session.signIn { api in
            try await api.signInWithGoogle(idToken: "google.jwt", rawNonce: "raw")
        }

        #expect(session.currentUser?.id == Fixtures.user.id)
        #expect(store.session?.tokens.accessToken == "access-new")
    }

    // MARK: Delete account

    @Test func deleteAccountDeletesOnServerThenSignsOut() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(access: "access-1"), user: Fixtures.user)
        let (session, store, transport) = try makeSession(stored: stored) { _ in (204, Data()) }
        await session.restore()
        let hookRan = Counter()
        session.willSignOut = { hookRan.increment() }

        try await session.deleteAccount()

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "DELETE")
        #expect(request.url?.path() == meRequestPath)
        #expect(request.bearerToken == "access-1")
        #expect(hookRan.value == 1)
        #expect(session.state == .signedOut)
        #expect(store.session == nil)
        // The server deleted every session; there's nothing to log out.
        #expect(transport.requests(to: logoutPath).isEmpty)
    }

    @Test func failedDeleteAccountKeepsTheSession() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let (session, store, _) = try makeSession(stored: stored) { _ in
            (500, Fixtures.errorJSON(code: "internal"))
        }
        await session.restore()

        await #expect(throws: APIError.self) { try await session.deleteAccount() }

        #expect(session.state == .signedIn(Fixtures.user))
        #expect(store.session == stored)
    }

    // MARK: Authorized requests

    @Test func concurrentUnauthorizedRequestsShareOneRefresh() async throws {
        let refreshCalls = Counter()
        let stored = StoredSession(tokens: Fixtures.tokens(access: "stale-access"), user: Fixtures.user)
        let (session, store, transport) = try makeSession(stored: stored) { request in
            switch request.url?.path() {
            case "/api/v1/auth/refresh":
                refreshCalls.increment()
                // Keep the refresh in flight while the other requests hit 401.
                try await Task.sleep(for: .milliseconds(200))
                return (200, Fixtures.tokenPairJSON(access: "fresh-access", refresh: "refresh-2"))
            case "/api/v1/me":
                if request.bearerToken == "fresh-access" {
                    return (200, Fixtures.meJSON)
                }
                return (401, Fixtures.errorJSON(code: "token_expired"))
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
        await session.restore()

        let requestCount = 10
        let userIDs = try await withThrowingTaskGroup(of: String.self) { group in
            for _ in 0..<requestCount {
                group.addTask { try await session.loadCurrentUser().user.id }
            }
            var ids: [String] = []
            for try await id in group {
                ids.append(id)
            }
            return ids
        }

        #expect(userIDs.count == requestCount)
        #expect(refreshCalls.value == 1)
        #expect(transport.requests(to: refreshPath).count == 1)
        #expect(transport.requests(to: refreshPath).first?.jsonBody == ["refreshToken": "refresh-1"])
        #expect(store.session?.tokens.refreshToken == "refresh-2")
        #expect(session.currentUser != nil)
    }

    @Test func concurrentRequestsWithExpiredTokenShareOneRefresh() async throws {
        let refreshCalls = Counter()
        let (session, _, transport) = try makeSession(stored: nil) { request in
            switch request.url?.path() {
            case "/api/v1/auth/dev":
                return (200, Fixtures.sessionJSON(access: "a", refresh: "r"))
            case "/api/v1/auth/refresh":
                refreshCalls.increment()
                try await Task.sleep(for: .milliseconds(100))
                return (200, Fixtures.tokenPairJSON(access: "fresh-access", refresh: "r2"))
            default:
                return request.bearerToken == "fresh-access"
                    ? (200, Fixtures.meJSON) : (401, Fixtures.errorJSON(code: "token_expired"))
            }
        }
        await session.restore()
        // A sign-in response whose access token is already expired.
        try await session.signIn { _ in
            try JSONCoding.makeDecoder().decode(
                SessionResponse.self,
                from: Data(
                    """
                    {"accessToken":"a","accessTokenExpiresAt":"2020-01-01T00:00:00Z","refreshToken":"r",
                     "refreshTokenExpiresAt":"\(Fixtures.iso(Date().addingTimeInterval(3600)))",
                     "user":{"id":"u","displayName":"","createdAt":"2020-01-01T00:00:00Z"},"isNewUser":false}
                    """.utf8))
        }

        try await withThrowingTaskGroup(of: Void.self) { group in
            for _ in 0..<5 {
                group.addTask { _ = try await session.loadCurrentUser() }
            }
            try await group.waitForAll()
        }

        #expect(refreshCalls.value == 1)
        #expect(transport.requests(to: meRequestPath).allSatisfy { $0.bearerToken == "fresh-access" })
    }

    @Test func rejectedRefreshDuringRequestSignsOut() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let (session, store, _) = try makeSession(stored: stored) { request in
            request.url?.path() == "/api/v1/auth/refresh"
                ? (401, Fixtures.errorJSON(code: "unauthenticated"))
                : (401, Fixtures.errorJSON(code: "token_expired"))
        }
        await session.restore()

        await #expect(throws: APIError.self) {
            try await session.loadCurrentUser()
        }
        #expect(session.state == .signedOut)
        #expect(store.session == nil)
    }

    @Test func secondUnauthorizedAfterRefreshSignsOut() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let (session, store, transport) = try makeSession(stored: stored) { request in
            request.url?.path() == "/api/v1/auth/refresh"
                ? (200, Fixtures.tokenPairJSON(access: "access-2", refresh: "refresh-2"))
                : (401, Fixtures.errorJSON(code: "unauthenticated"))
        }
        await session.restore()

        await #expect(throws: APIError.self) {
            try await session.loadCurrentUser()
        }
        #expect(transport.requests(to: meRequestPath).count == 2)
        #expect(session.state == .signedOut)
        #expect(store.session == nil)
    }

    @Test func serverErrorDuringRefreshKeepsSession() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(accessExpiresIn: -60), user: Fixtures.user)
        let (session, store, _) = try makeSession(stored: stored) { _ in
            (503, Fixtures.errorJSON(code: "unavailable"))
        }
        await session.restore()

        await #expect(throws: APIError.self) {
            try await session.loadCurrentUser()
        }
        #expect(session.currentUser == Fixtures.user)
        #expect(store.session == stored)
    }

    // MARK: Sign out

    @Test func signOutClearsStoreEvenWhenLogoutFails() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let (session, store, transport) = try makeSession(stored: stored) { _ in
            throw URLError(.notConnectedToInternet)
        }
        await session.restore()

        await session.signOut()

        #expect(session.state == .signedOut)
        #expect(store.session == nil)
        let logout = transport.requests(to: logoutPath)
        #expect(logout.count == 1)
        #expect(logout.first?.jsonBody == ["refreshToken": "refresh-1"])
        #expect(logout.first?.bearerToken == nil)
    }

    @Test func signOutClearsStoreWhenServerRejectsLogout() async throws {
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let (session, store, _) = try makeSession(stored: stored) { _ in
            (500, Fixtures.errorJSON(code: "internal"))
        }
        await session.restore()

        await session.signOut()

        #expect(session.state == .signedOut)
        #expect(store.session == nil)
    }
}
