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

    /// The measured shape of a real account on 2026-09-21: 740 recipes across 160 delivered
    /// weeks in 40 pages, stopped on the app's page cap with years of history still behind it.
    static let measuredPages = 40
    static let measuredWeeks = 160
    static let measuredRecipes = 740
    static let earliestWeek = "2023-W30"
    static let latestWeek = "2026-W38"

    static func historyJSON(
        earliest: String = earliestWeek, latest: String = latestWeek, resumeFrom: String? = nil,
        complete: Bool = false, moreToFetch: Bool = true
    ) -> String {
        """
        {"earliestWeek":"\(earliest)","latestWeek":"\(latest)",
         "resumeFromWeek":"\(resumeFrom ?? (complete ? "" : earliest))",
         "complete":\(complete),"moreToFetch":\(moreToFetch),"blockedUntil":null}
        """
    }

    static func statusJSON(enabled: Bool = true, job: String? = nil, history: String? = nil) -> Data {
        let historyField = history.map { ",\"history\":\($0)" } ?? ""
        return Data(
            """
            {"source":"hellofresh","enabled":\(enabled),"latestJob":\(job ?? "null")\(historyField)}
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
        recipes: String? = nil, pages: Int = 3, weeks: Int = 4,
        earliest: String = "2026-W33", latest: String = latestWeek, stopped: String = "end"
    ) -> String {
        let defaultRecipes = """
            [{"sourceRecipeId":"\(recipeIDA)","name":"Sheet Pan Chicken",
              "url":"\(pagePrefix)sheet-pan-chicken-\(recipeIDA)","weeks":["2026-W33","2026-W38"],"isAddon":false},
             {"sourceRecipeId":"\(recipeIDB)","name":"Garlic Bread",
              "url":"\(pagePrefix)garlic-bread-\(recipeIDB)","weeks":["2026-W38"],"isAddon":true}]
            """
        return """
            {"recipes":\(recipes ?? defaultRecipes),"pages":\(pages),"weeks":\(weeks),
             "earliestWeek":"\(earliest)","latestWeek":"\(latest)","stopped":"\(stopped)"}
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
        // The order history and where reading it stopped are the whole of the body. Anything
        // that smells of a credential must not be in it — that is the point of the whole design.
        #expect(fields.keys.sorted() == ["harvest", "recipes"])
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
        #expect(script.contains("recipes: recipes,"))
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
        let detail = MealKitFormatting.progressDetail(midway, service: .helloFresh)
        #expect(detail.hasPrefix("10 of 40 recipes"))
        // It also says the truth a member watching 10 of 740 needs: this continues without them.
        #expect(detail.lowercased().contains("server"))
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

/// Reading an order history too long for one sitting, over several sittings.
///
/// The numbers here are measured against a real HelloFresh account on 2026-09-21, not invented:
/// 740 recipes across 160 delivered weeks in 40 pages, and it **stopped on the page cap** with
/// years of history still behind it. A page therefore covers about four to five weeks.
struct MealKitHarvestResumeTests {
    private func makeClient(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    // MARK: - What one page of history covers

    @Test func aPageOfHistoryCoversAboutFourWeeksSoOneHarvestCannotFinishFourYears() {
        let perPage = Double(MealKitFixtures.measuredWeeks) / Double(MealKitFixtures.measuredPages)
        #expect(perPage > 3.5 && perPage < 5.5)

        // Four years of weekly deliveries at that rate needs far more pages than one harvest
        // walks, which is exactly why the server keeps a cursor.
        let pagesForFourYears = Int((52.0 * 4 / perPage).rounded(.up))
        #expect(pagesForFourYears > MealKitHarvestScript.maxPages)
        #expect(MealKitHarvestScript.maxPages == 40)
        // The measured harvest still fits in one post, so a partial harvest sends what it has.
        #expect(MealKitFixtures.measuredRecipes <= MealKitHarvestScript.maxRecipes)
    }

    // MARK: - The plan

    @Test func aHouseholdThatNeverImportedWalksBackFromToday() {
        #expect(MealKitHarvestPlan.segments(for: nil) == [MealKitHarvestSegment()])
        #expect(MealKitHarvestPlan.isCatchUpOnly(nil) == false)
    }

    @Test func aCappedHistoryCatchesUpThenResumesBelowTheFloor() throws {
        let history = MealKitImportHistory(
            earliestWeek: MealKitFixtures.earliestWeek, latestWeek: MealKitFixtures.latestWeek,
            resumeFromWeek: MealKitFixtures.earliestWeek, moreToFetch: true)

        let plan = MealKitHarvestPlan.segments(for: history)

        #expect(plan.count == 2)
        // First, today back to the newest week already read: that is how a *new* delivery is
        // picked up without re-reading four years.
        #expect(plan[0] == MealKitHarvestSegment(floor: MealKitFixtures.latestWeek))
        // Then the resume proper, one week below the oldest week already read — never the same
        // page again.
        #expect(plan[1] == MealKitHarvestSegment(from: "2023-W29"))
        #expect(MealKitHarvestPlan.isCatchUpOnly(history) == false)
    }

    @Test func aFinishedHistoryOnlyEverLooksForNewDeliveries() {
        let history = MealKitImportHistory(
            earliestWeek: "2022-W05", latestWeek: MealKitFixtures.latestWeek,
            resumeFromWeek: "", complete: true, moreToFetch: false)

        let plan = MealKitHarvestPlan.segments(for: history)

        #expect(plan == [MealKitHarvestSegment(floor: MealKitFixtures.latestWeek)])
        #expect(MealKitHarvestPlan.isCatchUpOnly(history))
    }

    @Test func steppingBackAWeekIsRealCalendarArithmetic() {
        #expect(MealKitHarvestPlan.weekBefore("2026-W38") == "2026-W37")
        // Across a year boundary, into a 52-week year and a 53-week one.
        #expect(MealKitHarvestPlan.weekBefore("2024-W01") == "2023-W52")
        #expect(MealKitHarvestPlan.weekBefore("2021-W01") == "2020-W53")
        for notAWeek in ["", "2026", "2026-W", "2026-W00", "2026-W54", "last march", "2026-W3"] {
            #expect(MealKitHarvestPlan.weekBefore(notAWeek) == nil, "\(notAWeek)")
        }
    }

    // MARK: - What the script is asked to do, and what it says back

    @Test func theScriptIsToldWhereToResumeAndNeverToldToStartOver() {
        let session = MealKitWebSession(accessToken: "not-a-real-access-token")
        let history = MealKitImportHistory(
            earliestWeek: MealKitFixtures.earliestWeek, latestWeek: MealKitFixtures.latestWeek,
            resumeFromWeek: MealKitFixtures.earliestWeek, moreToFetch: true)

        let arguments = MealKitHarvestScript.arguments(
            session: session, service: .helloFresh, history: history)

        let segments = arguments["segments"] as? [[String: String]]
        #expect(segments?.count == 2)
        #expect(segments?[0]["floor"] == MealKitFixtures.latestWeek)
        #expect(segments?[1]["from"] == "2023-W29")
        #expect(arguments["maxPages"] as? Int == 40)
        // The subscription the history endpoint wants is the numeric one; the app does not
        // guess it from a cookie, because the plan UUID answers 403.
        #expect(arguments["subscription"] as? String == "")
        #expect(MealKitService.helloFresh.planCookieName == nil)
        #expect(MealKitHarvestScript.body.contains("legacySubscriptionId"))
        // Whatever it ends up using has to be all digits: the plan UUID answers 403.
        #expect(MealKitHarvestScript.body.contains(#"/^\d+$/.test(String(subscription"#))
    }

    @Test func theHarvestSaysWhereItStopped() throws {
        let capped = MealKitHarvestResult(
            json: MealKitFixtures.harvestJSON(
                pages: MealKitFixtures.measuredPages, weeks: MealKitFixtures.measuredWeeks,
                earliest: MealKitFixtures.earliestWeek, stopped: "cap"),
            service: .helloFresh)
        guard case .harvested(let harvest) = capped else {
            Issue.record("the harvest was not read")
            return
        }

        #expect(harvest.stopped == .cap)
        #expect(harvest.moreToFetch)
        #expect(harvest.earliestWeek == MealKitFixtures.earliestWeek)
        #expect(harvest.latestWeek == MealKitFixtures.latestWeek)
        #expect(harvest.pages == 40)

        // Every other way of stopping, including one a newer script might invent.
        let reasons: [(String, MealKitHarvestStop, Bool)] = [
            ("empty", .empty, false), ("end", .end, false),
            ("caught_up", .caughtUp, false), ("whatever_next", .unknown, false),
        ]
        for (code, want, more) in reasons {
            guard
                case .harvested(let one) = MealKitHarvestResult(
                    json: MealKitFixtures.harvestJSON(stopped: code), service: .helloFresh)
            else {
                Issue.record("\(code) was not read")
                continue
            }
            #expect(one.stopped == want, "\(code)")
            #expect(one.moreToFetch == more, "\(code)")
        }
    }

    @Test func aCatchUpThatFindsNothingIsUpToDateNotAnEmptyAccount() {
        let nothing = MealKitFixtures.harvestJSON(recipes: "[]", stopped: "caught_up")

        #expect(
            MealKitHarvestResult(json: nothing, service: .helloFresh, catchingUp: true)
                == .failed(.nothingNew))
        // The same emptiness on a first-ever harvest still means "you have never ordered".
        #expect(MealKitHarvestResult(json: nothing, service: .helloFresh) == .failed(.empty))
    }

    // MARK: - The wire

    @Test func aCappedHarvestIsPostedWithWhereItStoppedAndNothingElse() async throws {
        let transport = StubTransport { _ in
            (202, Data(MealKitFixtures.jobJSON(status: "queued", done: 0).utf8))
        }
        guard
            case .harvested(let harvest) = MealKitHarvestResult(
                json: MealKitFixtures.harvestJSON(
                    pages: MealKitFixtures.measuredPages, weeks: MealKitFixtures.measuredWeeks,
                    earliest: MealKitFixtures.earliestWeek, stopped: "cap"),
                service: .helloFresh)
        else {
            Issue.record("the harvest was not read")
            return
        }

        _ = try await MealKitAPI(client: makeClient(transport)).startImport(
            householdID: "household-1", service: .helloFresh, harvest: harvest, accessToken: "token-1")

        let body = try #require(transport.requests.first?.httpBody)
        let fields = try #require(try? JSONSerialization.jsonObject(with: body) as? [String: Any])
        let report = try #require(fields["harvest"] as? [String: Any])
        #expect(report["earliestWeek"] as? String == MealKitFixtures.earliestWeek)
        #expect(report["stopped"] as? String == "cap")
        #expect(report["pages"] as? Int == 40)
        #expect(report["weeks"] as? Int == 160)
        // A partial harvest still posts the recipes it did reach: they belong in the library
        // now, not after however many more sign-ins the rest of the history takes.
        #expect((fields["recipes"] as? [[String: Any]])?.isEmpty == false)
        // And still nothing about the account.
        let text = try #require(String(data: body, encoding: .utf8)).lowercased()
        for forbidden in ["token", "cookie", "password", "session", "@"] {
            #expect(!text.contains(forbidden), "the body carries something called \(forbidden)")
        }
    }

    @Test func aStopReasonThisBuildInventedIsNotSentToTheServer() throws {
        guard
            case .harvested(let harvest) = MealKitHarvestResult(
                json: MealKitFixtures.harvestJSON(stopped: "something_new"), service: .helloFresh)
        else {
            Issue.record("the harvest was not read")
            return
        }

        let body = try JSONEncoder().encode(MealKitStartImportRequest(harvest: harvest))
        let fields = try #require(try? JSONSerialization.jsonObject(with: body) as? [String: Any])
        let report = try #require(fields["harvest"] as? [String: Any])
        // The server refuses a stop reason it cannot read, and a harvest must not be lost over
        // a label: it is left out instead.
        #expect(report["stopped"] == nil)
    }

    @Test func theStatusCarriesTheCursorAndAnOlderServerSimplyHasNone() async throws {
        let transport = StubTransport { _ in
            (
                200,
                MealKitFixtures.statusJSON(
                    job: MealKitFixtures.jobJSON(), history: MealKitFixtures.historyJSON())
            )
        }

        let status = try await MealKitAPI(client: makeClient(transport))
            .status(householdID: "household-1", service: .helloFresh, accessToken: "token-1")

        #expect(status.history.resumeFromWeek == MealKitFixtures.earliestWeek)
        #expect(status.history.moreToFetch)
        #expect(status.history.complete == false)
        #expect(MealKitHarvestPlan.segments(for: status.history).count == 2)

        // A server from before the cursor existed sends no history at all, and the harvest then
        // starts at today exactly as it used to.
        let older = StubTransport { _ in (200, MealKitFixtures.statusJSON(job: MealKitFixtures.jobJSON())) }
        let old = try await MealKitAPI(client: makeClient(older))
            .status(householdID: "household-1", service: .helloFresh, accessToken: "token-1")
        #expect(old.history.isEmpty)
        #expect(MealKitHarvestPlan.segments(for: old.history) == [MealKitHarvestSegment()])
    }

    // MARK: - What the member is told

    @Test func aMemberWithHistoryLeftIsToldSoAndWhatHappensNext() throws {
        let history = MealKitImportHistory(
            earliestWeek: "2024-W12", latestWeek: MealKitFixtures.latestWeek,
            resumeFromWeek: "2024-W12", moreToFetch: true)
        let job = MealKitImportJob(
            id: "job-1", status: "succeeded", recipesFound: 740, recipesDone: 740, imported: 740)

        let note = try #require(
            MealKitFormatting.moreHistoryNote(for: job, history: history, service: .helloFresh))

        #expect(note.contains("HelloFresh"))
        // It says how far back we got, in words rather than "2024-W12".
        #expect(note.contains("2024"))
        #expect(!note.contains("W12"))
        // And what will happen next — which needs them, because only their own browser session
        // can read their orders. Nothing here promises the server will do it alone.
        #expect(note.lowercased().contains("import again"))

        // A household whose history is finished is told nothing of the sort.
        let finished = MealKitImportHistory(
            earliestWeek: "2022-W05", latestWeek: MealKitFixtures.latestWeek, complete: true)
        #expect(
            MealKitFormatting.moreHistoryNote(for: nil, history: finished, service: .helloFresh) == nil)
        #expect(MealKitFormatting.moreHistoryNote(for: nil, history: nil, service: .helloFresh) == nil)
    }

    @Test func aQueuedRunSaysHowManyRecipesAreKnown() {
        let job = MealKitImportJob(id: "job-1", status: "queued", recipesFound: 740)

        let summary = MealKitFormatting.summary(for: job, service: .helloFresh)

        #expect(summary.detail.contains("740"))
        #expect(summary.detail.lowercased().contains("close the app"))
    }

    @Test func anIsoWeekIsShownAsAMonthAndYear() {
        #expect(MealKitFormatting.monthAndYear(of: "2024-W12") != nil)
        #expect(MealKitFormatting.monthAndYear(of: "not a week") == nil)
    }
}
