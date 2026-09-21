import Foundation
import Testing

@testable import DinnerOS

/// Synthetic status payloads shaped like the API's, a synthetic sign-in cookie shaped like
/// HelloFresh's, and a synthetic harvest shaped like the one the web view's script returns.
/// No real account: every token here is a literal in a test, never a credential.
private nonisolated enum MealKitFixtures {
    static let recipeIDA = "6512aa11bb22cc33dd44ee55"
    static let recipeIDB = "6512aa11bb22cc33dd44ee66"
    static let pagePrefix = "https://www.hellofresh.com/recipes/"

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

    static func statusJSON(enabled: Bool = true, job: String? = nil) -> Data {
        Data(
            """
            {"source":"hellofresh","enabled":\(enabled),"latestJob":\(job ?? "null")}
            """.utf8)
    }

    /// A cookie value shaped like the one the sign-in writes: URL-encoded JSON.
    static func cookie(
        accessToken: String = "not-a-real-access-token", tokenType: String? = "Bearer", extra: String = ""
    ) -> String {
        var fields = [#""access_token":"\#(accessToken)""#]
        if let tokenType { fields.append(#""token_type":"\#(tokenType)""#) }
        if !extra.isEmpty { fields.append(extra) }
        let json = "{" + fields.joined(separator: ",") + "}"
        return json.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? json
    }

    /// What the harvest script returns for a household with two delivered recipes.
    static func harvestJSON(
        recipes: String? = nil, pages: Int = 3, weeks: Int = 4
    ) -> String {
        let defaultRecipes = """
            [{"sourceRecipeId":"\(recipeIDA)","name":"Sheet Pan Chicken",
              "url":"\(pagePrefix)sheet-pan-chicken-\(recipeIDA)","weeks":["2026-W33","2026-W38"],"isAddon":false},
             {"sourceRecipeId":"\(recipeIDB)","name":"Garlic Bread",
              "url":"\(pagePrefix)garlic-bread-\(recipeIDB)","weeks":["2026-W38"],"isAddon":true}]
            """
        return """
            {"recipes":\(recipes ?? defaultRecipes),"pages":\(pages),"weeks":\(weeks)}
            """
    }

    static func harvest() -> MealKitHarvest {
        guard case .harvested(let harvest) = MealKitHarvestResult(json: harvestJSON(), service: .helloFresh) else {
            return MealKitHarvest(recipes: [])
        }
        return harvest
    }
}

/// Importing a household's own meal-kit order history: reading it in the web view, the API calls,
/// the store's states, and the wording the screens show.
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

    @Test func statusReadsTheHouseholdsNewestRun() async throws {
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
        let job = try #require(status.latestJob)
        #expect(job.state == .running)
        #expect(job.recipesFound == 48)
        #expect(job.progress == 0.25)
    }

    @Test func startingAnImportSendsTheOrderHistoryAndNothingElse() async throws {
        let transport = StubTransport { _ in
            (202, Data(MealKitFixtures.jobJSON(status: "queued", phase: "recipes").utf8))
        }

        let job = try await MealKitAPI(client: makeClient(transport)).startImport(
            householdID: "household-1", service: .helloFresh, harvest: MealKitFixtures.harvest(),
            accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/meal-kit/hellofresh/imports")
        #expect(request.url?.query() == nil)
        #expect(job.id == "job-1")

        let body = try #require(request.httpBody)
        let fields = try #require(try? JSONSerialization.jsonObject(with: body) as? [String: Any])
        // The order history is the whole of the body. Anything that smells of a credential must
        // not be in it — that is the point of the whole design.
        #expect(fields.keys.sorted() == ["recipes"])
        let recipes = try #require(fields["recipes"] as? [[String: Any]])
        #expect(recipes.count == 2)
        #expect(recipes[0]["sourceRecipeId"] as? String == MealKitFixtures.recipeIDA)
        #expect(recipes[0]["weeks"] as? [String] == ["2026-W33", "2026-W38"])
        #expect(recipes[1]["isAddon"] as? Bool == true)
        let text = try #require(String(data: body, encoding: .utf8)).lowercased()
        for forbidden in ["token", "cookie", "password", "session", "@"] {
            #expect(!text.contains(forbidden), "the body carries something called \(forbidden)")
        }
    }

    @Test func stoppingAndListingUseTheRightRoutes() async throws {
        let transport = StubTransport { request in
            request.httpMethod == "DELETE"
                ? (204, Data())
                : (200, Data(#"{"items":[\#(MealKitFixtures.jobJSON())]}"#.utf8))
        }
        let api = MealKitAPI(client: try makeClient(transport))

        try await api.stopImports(householdID: "household-1", service: .helloFresh, accessToken: "token-1")
        let jobs = try await api.imports(householdID: "household-1", service: .helloFresh, accessToken: "token-1")

        #expect(transport.requests[0].httpMethod == "DELETE")
        #expect(transport.requests[0].url?.path() == "/api/v1/households/household-1/meal-kit/hellofresh/imports")
        #expect(transport.requests[1].httpMethod == "GET")
        #expect(jobs.count == 1)
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

    @Test func storeReportsTheFeatureOffWhenTheServerHasItTurnedOff() async throws {
        let transport = StubTransport { _ in (200, MealKitFixtures.statusJSON(enabled: false)) }
        let store = try await makeStore(transport)

        await store.load()

        #expect(store.isAvailable)
        #expect(store.isEnabled == false)
        #expect(store.job == nil)
    }

    @Test func queueingAnImportStoresTheReturnedRun() async throws {
        let transport = StubTransport { _ in
            (202, Data(MealKitFixtures.jobJSON(status: "queued", phase: "recipes", done: 0).utf8))
        }
        let store = try await makeStore(transport)

        try await store.startImport(harvest: MealKitFixtures.harvest())

        #expect(store.job?.state == .queued)
        #expect(store.isImporting)
        #expect(store.isWorking == false)
    }

    @Test func aRejectedImportSurfacesAndLeavesNothingBehind() async throws {
        let transport = StubTransport { _ in
            (
                400,
                Data(
                    #"{"error":{"code":"validation_failed","message":"none of those looked like hellofresh recipes"}}"#
                        .utf8)
            )
        }
        let store = try await makeStore(transport)

        await #expect(throws: (any Error).self) {
            try await store.startImport(harvest: MealKitFixtures.harvest())
        }
        #expect(store.job == nil)
        #expect(store.isWorking == false)
    }

    @Test func stoppingRereadsTheStatusRatherThanGuessing() async throws {
        let counter = Counter()
        let transport = StubTransport { request in
            if request.httpMethod == "DELETE" {
                counter.increment()
                return (204, Data())
            }
            return counter.value == 0
                ? (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON()))
                : (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON(status: "canceled")))
        }
        let store = try await makeStore(transport)
        await store.load()
        #expect(store.isImporting)

        try await store.stopImports()

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
        #expect(store.phase == .idle)
    }

    // MARK: - The sign-in cookie

    @Test func theSessionIsReadOutOfTheSignInCookie() throws {
        let session = try #require(MealKitWebSession(cookieValue: MealKitFixtures.cookie()))

        #expect(session.accessToken == "not-a-real-access-token")
        #expect(session.tokenType == "Bearer")
    }

    @Test func aCookieThatIsNotASessionIsNoSession() {
        let notSessions = [
            "": "an empty cookie",
            "not-json-at-all": "something that isn't JSON",
            #"{"token_type":"Bearer"}"#.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? "":
                "a cookie with no access token",
        ]
        for (value, what) in notSessions {
            #expect(MealKitWebSession(cookieValue: value) == nil, "\(what) was read as a session")
        }
        #expect(MealKitWebSession(cookieValue: MealKitFixtures.cookie(accessToken: "   ")) == nil)
    }

    @Test func aCookieWithoutATokenTypeStillWorks() throws {
        let session = try #require(MealKitWebSession(cookieValue: MealKitFixtures.cookie(tokenType: nil)))

        #expect(session.tokenType == "Bearer")
    }

    @Test func theSignInPageIsTheServicesOwnDomain() throws {
        let url = try #require(MealKitService.helloFresh.loginURL)

        #expect(url.scheme == "https")
        #expect(url.host() == "www.hellofresh.com")
        #expect(MealKitService.helloFresh.cookieDomain == "hellofresh.com")
        #expect(MealKitService.helloFresh.sessionCookieName == "apiV2Auth")
    }

    // MARK: - Reading the order history

    @Test func theHarvestKeepsTheRecipesAndTheWeeksTheyCameIn() throws {
        guard
            case .harvested(let harvest) = MealKitHarvestResult(
                json: MealKitFixtures.harvestJSON(), service: .helloFresh)
        else {
            Issue.record("the harvest was not read")
            return
        }

        #expect(harvest.recipes.count == 2)
        #expect(harvest.pages == 3)
        #expect(harvest.weeks == 4)
        #expect(harvest.recipes[0].weeks == ["2026-W33", "2026-W38"])
        #expect(harvest.recipes[1].isAddon)
    }

    /// The script ran against someone else's page, so its output is untrusted too.
    @Test func theHarvestDropsAnythingItWouldNotFetch() throws {
        let mixed = """
            [{"sourceRecipeId":"\(MealKitFixtures.recipeIDA)","name":"Good",
              "url":"\(MealKitFixtures.pagePrefix)good-\(MealKitFixtures.recipeIDA)","weeks":[],"isAddon":false},
             {"sourceRecipeId":"\(MealKitFixtures.recipeIDB)","name":"Off-site",
              "url":"https://evil.example.com/recipes/x","weeks":[],"isAddon":false},
             {"sourceRecipeId":"not-an-id","name":"Junk",
              "url":"\(MealKitFixtures.pagePrefix)junk","weeks":[],"isAddon":false}]
            """
        guard
            case .harvested(let harvest) = MealKitHarvestResult(
                json: MealKitFixtures.harvestJSON(recipes: mixed), service: .helloFresh)
        else {
            Issue.record("the harvest was not read")
            return
        }

        #expect(harvest.recipes.count == 1)
        #expect(harvest.recipes[0].sourceRecipeID == MealKitFixtures.recipeIDA)
    }

    @Test func everyWayOfNotGettingAHistoryHasItsOwnReason() {
        let cases: [(String, MealKitHarvestFailure)] = [
            (#"{"error":{"code":"forbidden"}}"#, .forbidden),
            (#"{"error":{"code":"unreadable"}}"#, .unreadable),
            (#"{"error":{"code":"unavailable","status":500}}"#, .unavailable),
            // A code from a newer script this build doesn't know still lands somewhere sane.
            (#"{"error":{"code":"something_new"}}"#, .unavailable),
            // Nothing usable in the list is the same as no history.
            (MealKitFixtures.harvestJSON(recipes: "[]"), .empty),
            ("not json at all", .unreadable),
        ]
        for (json, want) in cases {
            let result = MealKitHarvestResult(json: json, service: .helloFresh)
            #expect(result == .failed(want), "\(json)")
        }
    }

    /// The script is the only thing that touches the meal kit's session, and it must not hand any
    /// of it back: what it returns is the body we post.
    @Test func theHarvestScriptNeverReturnsTheSession() {
        let script = MealKitHarvestScript.body
        #expect(script.contains("credentials: \"omit\""))
        #expect(script.contains("JSON.stringify({ recipes:"))
        // Nothing in it reads or returns the page, and nothing returns the token.
        for forbidden in ["document.cookie", "innerHTML", "innerText", "document.body", "JSON.stringify(token"] {
            #expect(!script.contains(forbidden), "the script mentions \(forbidden)")
        }
    }

    // MARK: - Wording

    @Test func theSummarySaysWhatIsHappeningForEveryState() {
        let cases: [(String, MealKitImportJob.State, Bool)] = [
            ("queued", .queued, false),
            ("running", .running, false),
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

    @Test func progressIsHonestBeforeAnyRecipeIsCounted() {
        let early = MealKitImportJob(id: "job-1", status: "running", phase: "recipes")
        #expect(early.progress == nil)
        #expect(!MealKitFormatting.progressDetail(early, service: .helloFresh).isEmpty)

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

    @Test func theExplanationSaysNothingAboutTheAccountIsKept() {
        let text = MealKitFormatting.credentialExplanation(for: .helloFresh)
        #expect(text.contains("HelloFresh"))
        #expect(text.lowercased().contains("password never reaches"))
        // It has to say that nothing is stored, because that is now literally true.
        #expect(text.lowercased().contains("nothing about"))
    }

    @Test func aHouseholdThatNeverImportedIsOfferedTheImport() {
        let summary = MealKitFormatting.summary(for: nil, service: .helloFresh)
        #expect(summary.needsAttention == false)
        #expect(summary.title == "No imports yet")
    }

    @Test func aJobDecodesItsFailuresAndItsError() async throws {
        let failures = #"""
            [{"sourceRecipeId":"src-1","name":"Sheet Pan Chicken","reason":"the page carried no recipe"},
             {"sourceRecipeId":"src-2","reason":"name is required"}]
            """#
        let lastError = #"""
            {"code":"blocked","message":"HelloFresh refused our requests.","at":"2026-09-20T10:05:00Z"}
            """#
        let transport = StubTransport { _ in
            (
                200,
                MealKitFixtures.statusJSON(
                    job: MealKitFixtures.jobJSON(status: "dead", failures: failures, lastError: lastError))
            )
        }

        let status = try await MealKitAPI(client: makeClient(transport))
            .status(householdID: "household-1", service: .helloFresh, accessToken: "token-1")

        let job = try #require(status.latestJob)
        #expect(job.state == .failed)
        #expect(job.failures.count == 2)
        #expect(job.failures[0].title == "Sheet Pan Chicken")
        // `name` is omitempty on the wire; the source ID is the fallback title.
        #expect(job.failures[1].title == "src-2")
        #expect(job.lastError?.code == "blocked")
    }

    @Test func anUnknownStatusIsTreatedAsStillWorking() {
        let job = MealKitImportJob(id: "job-1", status: "some_future_state")
        #expect(job.state == .running)
        #expect(job.isWorking)
    }
}
