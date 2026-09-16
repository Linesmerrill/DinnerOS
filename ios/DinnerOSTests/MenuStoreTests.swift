import Foundation
import Testing

@testable import DinnerOS

struct MenuStoreTests {
    private struct Harness {
        let store: MenuStore
        let plans: PlanStore
        let menuServer: FakeMenuServer
        let planServer: FakePlanServer
        let session: AuthSession
        let client: APIClient
    }

    private static let denver = TimeZone(identifier: "America/Denver") ?? .gmt

    /// "Now" is Wednesday, September 16, 2026 at noon UTC: 2026-W38 in Denver.
    private func makeHarness(
        menuServer: FakeMenuServer = FakeMenuServer(), planServer: FakePlanServer = FakePlanServer()
    ) async throws -> Harness {
        let transport = StubTransport { request in
            request.url?.path().contains("/plans") == true ? planServer.handle(request) : menuServer.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let instant = try #require(JSONCoding.parseDate("2026-09-16T12:00:00Z"))
        let store = MenuStore(session: session, api: MenuAPI(client: client), now: { instant })
        let plans = PlanStore(
            session: session, api: PlansAPI(client: client), checks: InMemoryGroceryChecks(), now: { instant })
        plans.planDidChange = { [store] plan in store.applyPlan(plan) }
        return Harness(
            store: store, plans: plans, menuServer: menuServer, planServer: planServer, session: session,
            client: client)
    }

    private func activated(_ menuServer: FakeMenuServer = FakeMenuServer()) async throws -> Harness {
        let harness = try await makeHarness(menuServer: menuServer)
        await harness.store.activate(householdID: "household-1", timeZone: Self.denver)
        return harness
    }

    private func week(_ string: String) throws -> ISOWeek {
        try #require(ISOWeek(string))
    }

    private func plan(week: String = "2026-W38", entries: [String], updatedAt: String = "2026-09-14T19:05:00Z")
        throws -> Plan
    {
        let json = PlanFixtures.plan(week: week, entries: entries)
            .replacingOccurrences(of: "2026-09-14T19:05:00Z", with: updatedAt)
        return try JSONCoding.makeDecoder().decode(Plan.self, from: Data(json.utf8))
    }

    // MARK: Loading

    @Test func activateLoadsTheMenuWeeksAndFilters() async throws {
        let harness = try await activated()
        let store = harness.store

        #expect(store.phase == .loaded)
        #expect(store.selectedWeek.description == "2026-W38")
        #expect(store.selectedTiming == .current)
        #expect(store.menu?.sections.map(\.id) == ["favorites", "quick"])
        #expect(store.weekSummaries["2026-W38"]?.plannedCount == 2)
        #expect(store.earliestWeek?.description == "2026-W20")
        #expect(store.filterOptions?.proteins.count == 2)
        #expect(harness.menuServer.weekQueries.first == ["around": "2026-W38", "before": "8", "after": "4"])
        let log = harness.menuServer.log
        #expect(log.contains("GET /households/household-1/menu?week=2026-W38"))

        // Activating the same household again doesn't refetch.
        await store.activate(householdID: "household-1", timeZone: Self.denver)
        #expect(harness.menuServer.log.count == log.count)
    }

    @Test func switchingWeeksLoadsThatWeeksMenu() async throws {
        let harness = try await activated()
        let store = harness.store

        await store.select(week: try week("2026-W37"))
        #expect(store.selectedWeek.description == "2026-W37")
        #expect(store.menu?.week == "2026-W37")
        #expect(store.selectedTiming == .past)
        #expect(store.menu?.sections.map(\.id) == ["history_planned"])
        #expect(store.allMeals.week?.description == "2026-W37")

        await store.select(week: try week("2026-W38"))
        #expect(store.menu?.week == "2026-W38")
        #expect(harness.menuServer.log.last?.hasPrefix("GET /households/household-1/menu?week=2026-W38") == true)
    }

    @Test func menuFailureThenRetry() async throws {
        let server = FakeMenuServer()
        server.update { $0.failMenu = true }
        let harness = try await activated(server)
        let store = harness.store

        guard case .failed = store.phase else {
            Issue.record("expected a failure, got \(store.phase)")
            return
        }
        // The strip still loads.
        #expect(!store.weekSummaries.isEmpty)

        server.update { $0.failMenu = false }
        await store.retry()
        #expect(store.phase == .loaded)

        server.update { $0.failMenu = true }
        await store.reload()
        #expect(store.phase == .loaded)
        #expect(store.refreshError != nil)
        #expect(store.menu != nil)
    }

    // MARK: Week strip

    @Test func stripLoadsEarlierWeeksDownToTheEarliestWeek() async throws {
        let harness = try await activated()
        let store = harness.store

        #expect(store.stripItems.first?.week.description == "2026-W30")
        #expect(store.stripItems.last?.week.description == "2026-W42")
        #expect(store.stripItems.first { $0.week.description == "2026-W37" }?.timing == .past)
        #expect(store.canLoadEarlierWeeks)

        await store.loadEarlierWeeks()
        #expect(harness.menuServer.weekQueries.last == ["around": "2026-W29", "before": "8", "after": "0"])
        #expect(store.oldestWeek.description == "2026-W21")
        #expect(store.weekSummaries["2026-W21"] != nil)
        #expect(store.canLoadEarlierWeeks)

        await store.loadEarlierWeeks()
        #expect(store.oldestWeek.description == "2026-W20")
        #expect(!store.canLoadEarlierWeeks)

        let requests = harness.menuServer.weekQueries.count
        await store.loadEarlierWeeks()
        #expect(harness.menuServer.weekQueries.count == requests)
    }

    @Test func stripFallsBackToLocalWeeksWhenTheWeekListFails() async throws {
        let server = FakeMenuServer()
        server.update { $0.failWeeks = true }
        let harness = try await activated(server)
        let store = harness.store

        #expect(store.weeksError != nil)
        #expect(store.stripItems.count == 13)
        #expect(store.stripItems.allSatisfy { $0.summary == nil })
        #expect(store.canLoadEarlierWeeks)

        await store.loadEarlierWeeks()
        #expect(store.oldestWeek.description == "2026-W21")
    }

    /// A week of seven dinners and a pairing's add-on is seven meals in the strip, the same as
    /// in the bottom bar and Your Meals, however the week's plan reaches the app.
    @Test func theStripCountsAddOnsApartFromMeals() async throws {
        let server = FakeMenuServer()
        server.update {
            $0.plannedCount = 7
            $0.addOnCount = 1
        }
        let harness = try await activated(server)
        let store = harness.store
        let thisWeek = try week("2026-W38")
        let mains = (1...7).map { PlanFixtures.entry(id: "e\($0)", recipeID: "recipe-\($0)", name: "Meal \($0)") }

        // What the server counted, before any plan is in hand.
        #expect(MenuFormat.weekPillDetail(store.summary(for: thisWeek), timing: .current) == "7 meals")

        // An older server doesn't mark the add-on entry, and the menu has no card for it, so
        // the eight entries mustn't be read as eight dinners.
        let unmarked = PlanFixtures.entry(id: "e8", recipeID: "addon-1", name: "Garlic Bread")
        store.applyPlan(try plan(entries: mains + [unmarked]))
        #expect(store.summary(for: thisWeek)?.plannedCount == 7)
        #expect(store.summary(for: thisWeek)?.addOnCount == 1)
        #expect(MenuFormat.weekPillDetail(store.summary(for: thisWeek), timing: .current) == "7 meals")

        // A current server marks the entry itself, and the week counts out the same way.
        let marked = PlanFixtures.entry(id: "e8", recipeID: "addon-1", name: "Garlic Bread", isAddon: true)
        store.applyPlan(try plan(entries: mains + [marked]))
        #expect(store.summary(for: thisWeek)?.plannedCount == 7)
        #expect(store.summary(for: thisWeek)?.addOnCount == 1)

        // Planning another dinner counts right away, with the add-on still apart from it.
        let ninth = PlanFixtures.entry(id: "e9", recipeID: "recipe-9", name: "Meal 9")
        store.applyPlan(try plan(entries: mains + [marked, ninth]))
        #expect(MenuFormat.weekPillDetail(store.summary(for: thisWeek), timing: .current) == "8 meals")
        #expect(store.summary(for: thisWeek)?.addOnCount == 1)
    }

    @Test func aPastWeekStillReadsOrdered() async throws {
        let server = FakeMenuServer()
        server.update {
            $0.pastCooked = 0
            $0.pastOrdered = 2
        }
        let harness = try await activated(server)
        let lastWeek = try week("2026-W37")

        #expect(MenuFormat.weekPillDetail(harness.store.summary(for: lastWeek), timing: .past) == "Ordered")

        // Nothing was planned that week, and the plan in hand doesn't erase the delivery.
        harness.store.applyPlan(try plan(week: "2026-W37", entries: []))
        #expect(MenuFormat.weekPillDetail(harness.store.summary(for: lastWeek), timing: .past) == "Ordered")
        #expect(harness.store.summary(for: lastWeek)?.orderedCount == 2)
    }

    @Test func pickingAWeekBeforeTheStripExtendsIt() async throws {
        let harness = try await activated()
        let store = harness.store

        await store.select(week: try week("2026-W25"))

        #expect(store.stripItems.first?.week.description == "2026-W25")
        #expect(harness.menuServer.weekQueries.last?["around"] == "2026-W25")
    }

    // MARK: All Meals

    private func list(_ harness: Harness, pageSize: Int = 2) throws -> MenuRecipeList {
        MenuRecipeList(
            session: harness.session, api: MenuAPI(client: harness.client), householdID: "household-1",
            week: try week("2026-W38"), pageSize: pageSize)
    }

    @Test func allMealsPagesByCursor() async throws {
        let harness = try await makeHarness()
        let list = try list(harness)

        await list.load()
        #expect(list.items.map(\.id) == ["recipe-1", "recipe-2"])
        #expect(list.nextCursor == "2")

        await list.loadMore()
        await list.loadMore()
        #expect(list.items.count == 5)
        #expect(!list.hasMore)

        await list.loadMore()
        let queries = harness.menuServer.recipeQueries
        #expect(queries.count == 3)
        #expect(queries[0]["cursor"] == nil)
        #expect(queries[0]["week"] == "2026-W38")
        #expect(queries[1]["cursor"] == "2")
        #expect(queries[2]["cursor"] == "4")
    }

    @Test func changingFiltersResetsTheCursor() async throws {
        let harness = try await makeHarness()
        let list = try list(harness)
        await list.load()
        await list.loadMore()
        #expect(list.items.count == 4)

        var query = list.query
        query.search = "taco"
        query.protein = "chicken"
        await list.setQuery(query)

        let last = try #require(harness.menuServer.recipeQueries.last)
        #expect(last["cursor"] == nil)
        #expect(last["q"] == "taco")
        #expect(last["protein"] == "chicken")
        #expect(list.items.map(\.id) == ["recipe-1"])
        #expect(!list.hasMore)

        // The same request with trailing spaces isn't sent again.
        let requests = harness.menuServer.recipeQueries.count
        query.search = "taco  "
        await list.setQuery(query)
        #expect(harness.menuServer.recipeQueries.count == requests)
        #expect(list.query.search == "taco  ")
    }

    @Test func switchingWeeksReloadsAShownList() async throws {
        let harness = try await makeHarness()
        let list = try list(harness)

        // An idle list only remembers the week.
        await list.setWeek(try week("2026-W39"))
        #expect(harness.menuServer.recipeQueries.isEmpty)

        await list.load()
        await list.setWeek(try week("2026-W40"))
        #expect(harness.menuServer.recipeQueries.last?["week"] == "2026-W40")
        #expect(harness.menuServer.recipeQueries.last?["cursor"] == nil)
    }

    // MARK: Plan changes

    @Test func addingToTheWeekMarksCardsInPlan() async throws {
        let harness = try await activated()
        let store = harness.store
        let plans = harness.plans
        await store.allMeals.load()
        let more = store.makeList(query: MenuRecipeQuery(sort: .popular))
        await more.load()
        await plans.activate(householdID: "household-1", timeZone: Self.denver)

        let entry = try await plans.addEntry(NewPlanEntry(recipeID: "recipe-1", day: .tue, servings: 2))

        let favorite = try #require(store.menu?.sections.first?.items.first)
        #expect(favorite.inPlan)
        #expect(favorite.planEntryIDs == [entry.id])
        #expect(store.menu?.plan?.entries.map(\.id) == [entry.id])
        #expect(store.allMeals.items.first { $0.id == "recipe-1" }?.inPlan == true)
        #expect(store.allMeals.items.first { $0.id == "recipe-2" }?.inPlan == false)
        #expect(more.items.first { $0.id == "recipe-1" }?.inPlan == true)
        #expect(store.summary(for: try week("2026-W38"))?.plannedCount == 1)
        #expect(store.card(forRecipeID: "recipe-1")?.inPlan == true)

        try await plans.deleteEntry(id: entry.id)
        #expect(store.menu?.sections.first?.items.first?.inPlan == false)
        #expect(store.allMeals.items.first { $0.id == "recipe-1" }?.inPlan == false)
    }

    @Test func anotherWeeksPlanOnlyChangesItsCount() async throws {
        let harness = try await activated()
        let store = harness.store

        store.applyPlan(try plan(week: "2026-W39", entries: [PlanFixtures.entry(id: "e1", recipeID: "recipe-1")]))

        #expect(store.menu?.sections.first?.items.first?.inPlan == false)
        #expect(store.summary(for: try week("2026-W39"))?.plannedCount == 1)
    }

    @Test func aMenuLoadedAfterAPlanChangeKeepsTheNewerPlan() async throws {
        let harness = try await activated()
        let store = harness.store
        store.applyPlan(
            try plan(
                week: "2026-W39", entries: [PlanFixtures.entry(id: "e1", recipeID: "recipe-2")],
                updatedAt: "2026-09-16T11:00:00Z"))

        await store.select(week: try week("2026-W39"))

        #expect(store.menu?.plan?.entries.map(\.id) == ["e1"])
        #expect(store.menu?.sections.first?.items.first { $0.id == "recipe-2" }?.inPlan == true)
    }

    @Test func plansForAnotherHouseholdAreIgnored() async throws {
        let harness = try await activated()
        let store = harness.store

        store.applyPlan(
            try plan(entries: [PlanFixtures.entry(id: "e1", recipeID: "recipe-1")])
                .withHousehold("household-2"))

        #expect(store.menu?.sections.first?.items.first?.inPlan == false)
    }

    // MARK: Reset

    @Test func resetForgetsEverything() async throws {
        let harness = try await activated()
        let store = harness.store
        await store.allMeals.load()

        store.reset()

        #expect(store.householdID == nil)
        #expect(store.menu == nil)
        #expect(store.phase == .idle)
        #expect(store.weekSummaries.isEmpty)
        #expect(store.earliestWeek == nil)
        #expect(store.filterOptions == nil)
        #expect(store.allMeals.items.isEmpty)
        #expect(store.allMeals.phase == .idle)
        #expect(store.allMeals.householdID == nil)
    }

    @Test func switchingHouseholdsStartsOver() async throws {
        let harness = try await activated()
        let store = harness.store
        await store.select(week: try week("2026-W40"))
        await store.allMeals.load()
        var query = store.allMeals.query
        query.search = "soup"
        await store.allMeals.setQuery(query)

        await store.activate(householdID: "household-2", timeZone: Self.denver)

        #expect(store.householdID == "household-2")
        #expect(store.selectedWeek.description == "2026-W38")
        #expect(store.allMeals.query == MenuRecipeQuery())
        #expect(store.allMeals.phase == .idle)
        #expect(harness.menuServer.log.contains("GET /households/household-2/menu?week=2026-W38"))
    }
}

extension Plan {
    fileprivate func withHousehold(_ householdID: String) -> Plan {
        Plan(
            householdID: householdID, week: week, startDate: startDate, endDate: endDate, status: status,
            entries: entries, createdAt: createdAt, updatedAt: updatedAt)
    }
}
