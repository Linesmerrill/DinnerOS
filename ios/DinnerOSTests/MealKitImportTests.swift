import Foundation
import Testing

@testable import DinnerOS

/// Synthetic status payloads shaped like the API's. No real account, and the "password" here
/// is a literal in a test, never a credential.
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

    @Test func linkSendsTheCredentialInTheBodyAndNeverInTheURL() async throws {
        let transport = StubTransport { _ in
            (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON(status: "queued", phase: "orders")))
        }

        _ = try await MealKitAPI(client: makeClient(transport)).link(
            householdID: "household-1", service: .helloFresh, email: "cook@example.com",
            password: "not-a-real-password", accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path() == "/api/v1/households/household-1/meal-kit/hellofresh/link")
        // The credential must not reach a URL, which request logs keep.
        #expect(request.url?.query() == nil)
        #expect(request.url?.absoluteString.contains("not-a-real-password") == false)
        let body = try #require(request.httpBody)
        let fields = try #require(try? JSONSerialization.jsonObject(with: body) as? [String: Any])
        #expect(fields["email"] as? String == "cook@example.com")
        #expect(fields["password"] as? String == "not-a-real-password")
        #expect(fields["startImport"] as? Bool == true)
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

        try await store.link(email: "cook@example.com", password: "not-a-real-password")

        #expect(store.link?.accountLabel == "HelloFresh account")
        #expect(store.job?.state == .queued)
        #expect(store.isImporting)
        #expect(store.isWorking == false)
    }

    @Test func aFailedSignInSurfacesAndLeavesNothingLinked() async throws {
        let transport = StubTransport { _ in
            (
                400,
                Data(
                    #"{"error":{"code":"validation_failed","message":"that email and password did not sign in to the meal-kit account"}}"#
                        .utf8)
            )
        }
        let store = try await makeStore(transport)

        await #expect(throws: (any Error).self) {
            try await store.link(email: "cook@example.com", password: "wrong")
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

    @Test func theCredentialExplanationSaysWhatIsAndIsNotKept() {
        let text = MealKitFormatting.credentialExplanation(for: .helloFresh)
        #expect(text.contains("HelloFresh"))
        #expect(text.contains("encrypted"))
        #expect(text.lowercased().contains("password is never stored"))
    }

    @Test func aHouseholdThatNeverImportedIsOfferedTheImport() {
        let summary = MealKitFormatting.summary(for: nil, service: .helloFresh)
        #expect(summary.needsAttention == false)
        #expect(summary.title == "No imports yet")
    }
}
