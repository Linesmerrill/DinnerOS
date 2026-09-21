import Foundation
import Testing

@testable import DinnerOS

/// Synthetic status payloads shaped like the API's, and a synthetic sign-in cookie shaped like
/// HelloFresh's. No real account: every token here is a literal in a test, never a credential.
private nonisolated enum MealKitFixtures {
    static let linkJSON = #"""
        {"source":"hellofresh","status":"active","accountLabel":"HelloFresh account",
         "linkedAt":"2026-09-20T10:00:00Z","updatedAt":"2026-09-20T10:00:00Z","lastUsedAt":null}
        """#

    static func jobJSON(
        status: String = "running", phase: String = "recipes", found: Int = 48, done: Int = 12,
        imported: Int = 10, updated: Int = 2, unchanged: Int = 0, reviewItems: Int = 0,
        failures: String = "[]", lastError: String = "null", finishedAt: String = "null"
    ) -> String {
        """
        {"id":"job-1","source":"hellofresh","status":"\(status)","phase":"\(phase)",
         "recipesFound":\(found),"recipesDone":\(done),"imported":\(imported),"updated":\(updated),
         "unchanged":\(unchanged),"reviewItems":\(reviewItems),"failures":\(failures),
         "attempts":1,"maxAttempts":5,"lastError":\(lastError),
         "createdAt":"2026-09-20T10:00:00Z","updatedAt":"2026-09-20T10:05:00Z","finishedAt":\(finishedAt)}
        """
    }

    /// A cookie value shaped like the one the sign-in writes: URL-encoded JSON.
    static func cookie(
        accessToken: String = "not-a-real-access-token", refreshToken: String? = "not-a-real-refresh-token",
        expiresIn: String? = "3600", issuedAt: String? = "1790000000", extra: String = ""
    ) -> String {
        var fields = [#""access_token":"\#(accessToken)""#]
        if let refreshToken { fields.append(#""refresh_token":"\#(refreshToken)""#) }
        if let expiresIn { fields.append(#""expires_in":\#(expiresIn)"#) }
        if let issuedAt { fields.append(#""issued_at":\#(issuedAt)"#) }
        if !extra.isEmpty { fields.append(extra) }
        let json = "{" + fields.joined(separator: ",") + "}"
        return json.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? json
    }

    static func statusJSON(enabled: Bool = true, link: String? = linkJSON, job: String? = nil) -> Data {
        Data(
            """
            {"source":"hellofresh","enabled":\(enabled),"link":\(link ?? "null"),"latestJob":\(job ?? "null")}
            """.utf8)
    }
}

/// Importing a household's own meal-kit order history: the API calls, the store's states,
/// and the wording the screens show.
struct MealKitImportTests {
    private func makeClient(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    private func makeStore(_ transport: StubTransport) async throws -> MealKitImportStore {
        let client = try makeClient(transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let store = MealKitImportStore(session: session, api: MealKitAPI(client: client))
        store.activate(householdID: "household-1")
        return store
    }

    // MARK: - API

    @Test func statusReadsTheHouseholdsLinkAndNewestRun() async throws {
        let transport = StubTransport { _ in
            (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON()))
        }

        let status = try await MealKitAPI(client: makeClient(transport))
            .status(householdID: "household-1", service: .helloFresh, accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/meal-kit/hellofresh")
        #expect(request.bearerToken == "token-1")
        #expect(status.enabled)
        #expect(status.link?.accountLabel == "HelloFresh account")
        #expect(status.link?.needsSignIn == false)
        let job = try #require(status.latestJob)
        #expect(job.state == .running)
        #expect(job.recipesFound == 48)
        #expect(job.progress == 0.25)
    }

    @Test func linkSendsTheSessionInTheBodyAndNeverInTheURL() async throws {
        let transport = StubTransport { _ in
            (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON(status: "queued", phase: "orders")))
        }
        let session = try #require(MealKitWebSession(cookieValue: MealKitFixtures.cookie()))

        _ = try await MealKitAPI(client: makeClient(transport)).link(
            householdID: "household-1", service: .helloFresh, session: session, accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path() == "/api/v1/households/household-1/meal-kit/hellofresh/link")
        // The session must not reach a URL, which request logs keep.
        #expect(request.url?.query() == nil)
        #expect(request.url?.absoluteString.contains("not-a-real-access-token") == false)
        let body = try #require(request.httpBody)
        let fields = try #require(try? JSONSerialization.jsonObject(with: body) as? [String: Any])
        #expect(fields["accessToken"] as? String == "not-a-real-access-token")
        #expect(fields["refreshToken"] as? String == "not-a-real-refresh-token")
        #expect(fields["expiresAt"] != nil)
        #expect(fields["startImport"] as? Bool == true)
        // No password field: there is no password anywhere in this flow.
        #expect(fields["password"] == nil)
        #expect(fields["email"] == nil)
    }

    @Test func unlinkAndStartImportUseTheRightRoutes() async throws {
        let transport = StubTransport { request in
            request.httpMethod == "DELETE" ? (204, Data()) : (202, Data(MealKitFixtures.jobJSON().utf8))
        }
        let api = MealKitAPI(client: try makeClient(transport))

        try await api.unlink(householdID: "household-1", service: .helloFresh, accessToken: "token-1")
        let job = try await api.startImport(householdID: "household-1", service: .helloFresh, accessToken: "token-1")

        #expect(transport.requests[0].httpMethod == "DELETE")
        #expect(transport.requests[0].url?.path() == "/api/v1/households/household-1/meal-kit/hellofresh/link")
        #expect(transport.requests[1].httpMethod == "POST")
        #expect(transport.requests[1].url?.path() == "/api/v1/households/household-1/meal-kit/hellofresh/imports")
        #expect(job.id == "job-1")
    }

    // MARK: - Store

    @Test func storeHidesTheFeatureWhenTheAPIDoesNotHaveTheRoute() async throws {
        let transport = StubTransport { _ in (404, Data(#"{"error":{"code":"not_found"}}"#.utf8)) }
        let store = try await makeStore(transport)

        await store.load()

        #expect(store.isAvailable == false)
        #expect(store.isEnabled == false)
        #expect(store.phase == .loaded)
        #expect(store.job == nil)
    }

    @Test func storeReportsTheFeatureOffWhenTheServerHasNoKey() async throws {
        let transport = StubTransport { _ in (200, MealKitFixtures.statusJSON(enabled: false, link: nil)) }
        let store = try await makeStore(transport)

        await store.load()

        #expect(store.isAvailable)
        #expect(store.isEnabled == false)
        #expect(store.link == nil)
    }

    @Test func linkingStoresTheReturnedStatusAndClearsNothingElse() async throws {
        let transport = StubTransport { _ in
            (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON(status: "queued", phase: "orders", done: 0)))
        }
        let store = try await makeStore(transport)

        let session = try #require(MealKitWebSession(cookieValue: MealKitFixtures.cookie()))
        try await store.link(webSession: session)

        #expect(store.link?.accountLabel == "HelloFresh account")
        #expect(store.job?.state == .queued)
        #expect(store.isImporting)
        #expect(store.isWorking == false)
    }

    @Test func aRejectedSessionSurfacesAndLeavesNothingLinked() async throws {
        let transport = StubTransport { _ in
            (
                400,
                Data(
                    #"{"error":{"code":"validation_failed","message":"the hellofresh sign-in did not produce a session; sign in again"}}"#
                        .utf8)
            )
        }
        let store = try await makeStore(transport)
        let session = try #require(MealKitWebSession(cookieValue: MealKitFixtures.cookie()))

        await #expect(throws: (any Error).self) {
            try await store.link(webSession: session)
        }
        #expect(store.link == nil)
        #expect(store.isWorking == false)
    }

    @Test func unlinkingRereadsTheStatusRatherThanGuessing() async throws {
        let counter = Counter()
        let transport = StubTransport { request in
            if request.httpMethod == "DELETE" {
                counter.increment()
                return (204, Data())
            }
            // After the unlink the server has nothing linked and the run is cancelled.
            return counter.value == 0
                ? (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON()))
                : (200, MealKitFixtures.statusJSON(link: nil, job: MealKitFixtures.jobJSON(status: "canceled")))
        }
        let store = try await makeStore(transport)
        await store.load()
        #expect(store.link != nil)

        try await store.unlink()

        #expect(store.link == nil)
        #expect(store.job?.state == .canceled)
        #expect(store.isImporting == false)
    }

    @Test func aRefreshFailureKeepsWhatIsOnScreen() async throws {
        let counter = Counter()
        let transport = StubTransport { _ in
            defer { counter.increment() }
            return counter.value == 0
                ? (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON()))
                : (500, Data(#"{"error":{"code":"internal"}}"#.utf8))
        }
        let store = try await makeStore(transport)
        await store.load()

        await store.refresh()

        #expect(store.phase == .loaded)
        #expect(store.job != nil)
        #expect(store.refreshError != nil)
    }

    @Test func switchingHouseholdForgetsTheOldOne() async throws {
        let transport = StubTransport { _ in (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON())) }
        let store = try await makeStore(transport)
        await store.load()
        #expect(store.job != nil)

        store.activate(householdID: "household-2")

        #expect(store.job == nil)
        #expect(store.link == nil)
        #expect(store.phase == .idle)
    }

    // MARK: - Decoding

    @Test func aJobDecodesItsFailuresAndItsError() async throws {
        let failures = #"""
            [{"sourceRecipeId":"src-1","name":"Sheet Pan Chicken","reason":"the page carried no recipe"},
             {"sourceRecipeId":"src-2","reason":"name is required"}]
            """#
        let lastError = #"""
            {"code":"auth_expired","message":"Your meal-kit sign-in expired.","at":"2026-09-20T10:05:00Z"}
            """#
        let transport = StubTransport { _ in
            (
                200,
                MealKitFixtures.statusJSON(
                    job: MealKitFixtures.jobJSON(
                        status: "paused_auth", failures: failures, lastError: lastError))
            )
        }

        let status = try await MealKitAPI(client: makeClient(transport))
            .status(householdID: "household-1", service: .helloFresh, accessToken: "token-1")

        let job = try #require(status.latestJob)
        #expect(job.state == .needsSignIn)
        #expect(job.failures.count == 2)
        #expect(job.failures[0].title == "Sheet Pan Chicken")
        // `name` is omitempty on the wire; the source ID is the fallback title.
        #expect(job.failures[1].title == "src-2")
        #expect(job.lastError?.needsSignIn == true)
    }

    @Test func anUnknownStatusIsTreatedAsStillWorking() {
        let job = MealKitImportJob(id: "job-1", status: "some_future_state")
        #expect(job.state == .running)
        #expect(job.isWorking)
    }

    // MARK: - Wording

    @Test func theSummarySaysWhatIsHappeningForEveryState() {
        let cases: [(String, MealKitImportJob.State, Bool)] = [
            ("queued", .queued, false),
            ("running", .running, false),
            ("paused_auth", .needsSignIn, true),
            ("succeeded", .finished, false),
            ("dead", .failed, true),
            ("canceled", .canceled, false),
        ]
        for (status, state, needsAttention) in cases {
            let job = MealKitImportJob(id: "job-1", status: status, recipesFound: 10, recipesDone: 10, imported: 10)
            let summary = MealKitFormatting.summary(for: job, service: .helloFresh)
            #expect(job.state == state)
            #expect(summary.needsAttention == needsAttention, "\(status)")
            #expect(!summary.title.isEmpty && !summary.detail.isEmpty, "\(status)")
        }
    }

    @Test func progressIsHonestBeforeTheOrderHistoryIsRead() {
        let early = MealKitImportJob(id: "job-1", status: "running", phase: "orders")
        #expect(early.progress == nil)
        #expect(MealKitFormatting.progressDetail(early, service: .helloFresh).contains("order history"))

        let midway = MealKitImportJob(id: "job-1", status: "running", recipesFound: 40, recipesDone: 10)
        #expect(midway.progress == 0.25)
        #expect(MealKitFormatting.progressDetail(midway, service: .helloFresh) == "10 of 40 recipes")
    }

    @Test func aFinishedRunCountsWhatLandedAndWhatDidNot() {
        let job = MealKitImportJob(
            id: "job-1", status: "succeeded", recipesFound: 12, recipesDone: 11,
            imported: 8, updated: 2, unchanged: 1,
            failures: [MealKitImportFailure(sourceRecipeID: "src-1", reason: "the page carried no recipe")])

        let summary = MealKitFormatting.summary(for: job, service: .helloFresh)

        #expect(job.recipesAdded == 10)
        #expect(summary.title == "10 recipes added")
        #expect(summary.detail.contains("1 was already in your library"))
        #expect(summary.detail.contains("1 couldn't be imported"))
    }

    @Test func theCredentialExplanationSaysWhoSeesThePassword() {
        let text = MealKitFormatting.credentialExplanation(for: .helloFresh)
        #expect(text.contains("HelloFresh"))
        #expect(text.contains("encrypted"))
        // It has to say the password does not come here, because that is the whole design.
        #expect(text.lowercased().contains("password never reaches"))
    }

    // MARK: - The sign-in cookie

    @Test func aSessionIsReadOutOfTheSignInCookie() throws {
        let issued = Date(timeIntervalSince1970: 1_790_000_000)
        let session = try #require(MealKitWebSession(cookieValue: MealKitFixtures.cookie()))

        #expect(session.accessToken == "not-a-real-access-token")
        #expect(session.refreshToken == "not-a-real-refresh-token")
        #expect(session.expiresAt == issued.addingTimeInterval(3600))
    }

    @Test func aCookieThatIsNotASessionIsNoSession() {
        let notSessions = [
            "": "an empty cookie",
            "not-json-at-all": "something that isn't JSON",
            #"{"refresh_token":"r"}"#.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? "":
                "a cookie with no access token",
        ]
        for (value, what) in notSessions {
            #expect(MealKitWebSession(cookieValue: value) == nil, "\(what) was read as a session")
        }
        // A cookie whose access token is only whitespace is not a session either.
        #expect(MealKitWebSession(cookieValue: MealKitFixtures.cookie(accessToken: "   ")) == nil)
    }

    @Test func aSessionWithoutALifetimeDoesNotInventOne() throws {
        let session = try #require(
            MealKitWebSession(cookieValue: MealKitFixtures.cookie(refreshToken: nil, expiresIn: nil)))

        #expect(session.refreshToken.isEmpty)
        #expect(session.expiresAt == nil)
    }

    @Test func aSessionIssuedInMillisecondsStillExpiresWhenItShould() throws {
        let session = try #require(
            MealKitWebSession(cookieValue: MealKitFixtures.cookie(issuedAt: "1790000000000")))

        #expect(session.expiresAt == Date(timeIntervalSince1970: 1_790_000_000).addingTimeInterval(3600))
    }

    @Test func nothingElseInTheCookieIsTakenFromTheAccount() throws {
        let cookie = MealKitFixtures.cookie(extra: #""user_data":{"email":"cook@example.com","id":"u-1"}"#)
        let session = try #require(MealKitWebSession(cookieValue: cookie))

        // The request body is the whole of what leaves the device.
        let body = try JSONCoding.makeEncoder().encode(MealKitLinkRequest(session: session, startImport: true))
        let text = try #require(String(data: body, encoding: .utf8))
        #expect(!text.contains("cook@example.com"))
        #expect(!text.contains("u-1"))
    }

    @Test func theSignInPageIsTheServicesOwnDomain() throws {
        let url = try #require(MealKitService.helloFresh.loginURL)

        #expect(url.scheme == "https")
        #expect(url.host() == "www.hellofresh.com")
        #expect(MealKitService.helloFresh.cookieDomain == "hellofresh.com")
        #expect(MealKitService.helloFresh.sessionCookieName == "apiV2Auth")
    }

    @Test func aHouseholdThatNeverImportedIsOfferedTheImport() {
        let summary = MealKitFormatting.summary(for: nil, service: .helloFresh)
        #expect(summary.needsAttention == false)
        #expect(summary.title == "No imports yet")
    }
}
