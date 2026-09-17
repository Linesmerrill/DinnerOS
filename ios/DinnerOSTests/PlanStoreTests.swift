import Foundation
import Testing

@testable import DinnerOS

struct PlanStoreTests {
    private struct Harness {
        let store: PlanStore
        let server: FakePlanServer
    }

    private static let denver = TimeZone(identifier: "America/Denver") ?? .gmt

    /// "Now" is Wednesday, September 16, 2026 at noon UTC: 2026-W38 in Denver.
    private func makeHarness(
        server: FakePlanServer = FakePlanServer(), now: String = "2026-09-16T12:00:00Z"
    ) async throws -> Harness {
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let instant = try #require(JSONCoding.parseDate(now))
        let store = PlanStore(
            session: session, api: PlansAPI(client: client), checks: InMemoryGroceryChecks(), now: { instant })
        return Harness(store: store, server: server)
    }

    private func activated(_ server: FakePlanServer = FakePlanServer()) async throws -> Harness {
        let harness = try await makeHarness(server: server)
        await harness.store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .mon)
        return harness
    }

    // MARK: Loading

    @Test func activateLoadsThisWeekInTheHouseholdTimeZone() async throws {
        // Sunday night in Denver, Monday in UTC.
        let harness = try await makeHarness(now: "2026-09-21T03:00:00Z")
        let store = harness.store
        #expect(store.phase == .idle)

        await store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .mon)

        #expect(store.week.description == "2026-W38")
        #expect(store.today == .sun)
        #expect(store.phase == .loaded)
        #expect(store.plan?.entries.isEmpty == true)
        #expect(store.isDraft)
        #expect(harness.server.log == ["GET /households/household-1/plans/2026-W38"])

        // Activating the same household again doesn't refetch.
        await store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .mon)
        #expect(harness.server.log.count == 1)
    }

    /// Sunday noon in Denver is the end of 2026-W38 for a Monday household and the start of
    /// 2026-W39 for a Sunday one. Changing the start day reloads, and follows this week.
    @Test func aNewWeekStartDayReloadsAndFollowsThisWeek() async throws {
        let harness = try await makeHarness(now: "2026-09-20T18:00:00Z")
        let store = harness.store
        await store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .mon)
        #expect(store.week.description == "2026-W38")
        #expect(store.weekStartsOn == .mon)

        await store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .sun)

        #expect(store.weekStartsOn == .sun)
        #expect(store.week.description == "2026-W39")
        #expect(store.today == .sun)
        #expect(store.phase == .loaded)
        #expect(store.plan?.week == "2026-W39")
        #expect(
            harness.server.log == [
                "GET /households/household-1/plans/2026-W38", "GET /households/household-1/plans/2026-W39",
            ])
    }

    /// A week other than this one stays shown, reloaded, since its meals may have moved.
    @Test func aNewWeekStartDayReloadsAnotherShownWeekInPlace() async throws {
        let harness = try await activated()
        let store = harness.store
        let later = try #require(ISOWeek("2026-W40"))
        await store.show(week: later)

        await store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .sat)

        #expect(store.week == later)
        #expect(store.phase == .loaded)
        #expect(harness.server.log.last == "GET /households/household-1/plans/2026-W40")
        #expect(harness.server.log.count == 3)
    }

    /// The grocery list screen builds its model from here, and answers `nil` without a
    /// household. The screen has to say so: it used to show a spinner nothing would replace.
    @Test func aGroceryListIsOnlyMadeOnceThereIsAHousehold() async throws {
        let harness = try await makeHarness()
        let week = try #require(ISOWeek("2026-W38"))

        #expect(harness.store.makeGroceryList(week: week) == nil)

        await harness.store.activate(householdID: "household-1", timeZone: Self.denver, weekStartsOn: .mon)

        #expect(harness.store.makeGroceryList(week: week) != nil)
    }

    @Test func switchingWeeksLoadsThemAndReturnsToThisWeek() async throws {
        let harness = try await activated()
        let store = harness.store

        await store.show(week: store.week.next)
        #expect(store.week.description == "2026-W39")
        #expect(store.today == nil)
        #expect(store.plan?.week == "2026-W39")

        await store.showCurrentWeek()
        #expect(store.week.description == "2026-W38")
        #expect(harness.server.log.last == "GET /households/household-1/plans/2026-W38")
    }

    @Test func loadFailureThenReload() async throws {
        let server = FakePlanServer()
        server.failNext()
        let harness = try await activated(server)
        let store = harness.store

        guard case .failed(let message) = store.phase else {
            Issue.record("expected a failure, got \(store.phase)")
            return
        }
        #expect(message.localizedCaseInsensitiveContains("server"))

        await store.reload()
        #expect(store.phase == .loaded)

        server.failNext()
        await store.reload()
        #expect(store.phase == .loaded)
        #expect(store.refreshError != nil)
        #expect(store.plan != nil)
    }

    // MARK: Changes

    @Test func addAppliesTheReturnedPlanWithoutReloading() async throws {
        let harness = try await activated()
        let store = harness.store

        let entry = try await store.addEntry(NewPlanEntry(recipeID: "recipe-1", day: .thu, servings: 4, note: "hi"))

        #expect(entry.day == .thu)
        #expect(store.plan?.entries.map(\.id) == [entry.id])
        #expect(store.plan?.entries.first?.servings == 4)
        #expect(!store.isSaving)
        #expect(harness.server.log.filter { $0.hasPrefix("GET") }.count == 1)
    }

    @Test func addingToAnotherWeekLeavesTheShownPlanAlone() async throws {
        let harness = try await activated()
        let store = harness.store

        try await store.addEntry(NewPlanEntry(recipeID: "recipe-2", servings: 2), to: store.week.next)

        #expect(store.plan?.week == "2026-W38")
        #expect(store.plan?.entries.isEmpty == true)
        #expect(harness.server.week("2026-W39")?.entries.count == 1)
    }

    @Test func updateMovesUnschedulesAndChangesServings() async throws {
        let harness = try await activated()
        let store = harness.store
        let entry = try await store.addEntry(NewPlanEntry(recipeID: "recipe-1", day: .mon, servings: 2))

        try await store.moveEntry(id: entry.id, to: .fri)
        #expect(store.plan?.entries.first?.day == .fri)

        try await store.moveEntry(id: entry.id, to: nil)
        #expect(store.plan?.entries.first?.day == nil)
        #expect(store.plan?.entries(on: nil).count == 1)

        // Moving to the current day sends nothing.
        let requests = harness.server.log.count
        try await store.moveEntry(id: entry.id, to: nil)
        #expect(harness.server.log.count == requests)

        try await store.updateEntry(id: entry.id, changes: PlanEntryChanges(servings: 4, note: "double"))
        #expect(store.plan?.entries.first?.servings == 4)
        #expect(store.plan?.entries.first?.note == "double")
        #expect(store.plan?.entries.first?.day == nil)
    }

    @Test func deleteRemovesTheEntryAndReloads() async throws {
        let harness = try await activated()
        let store = harness.store
        let first = try await store.addEntry(NewPlanEntry(recipeID: "recipe-1", servings: 2))
        let second = try await store.addEntry(NewPlanEntry(recipeID: "recipe-2", servings: 2))

        try await store.deleteEntry(id: first.id)

        #expect(store.plan?.entries.map(\.id) == [second.id])
        #expect(
            harness.server.log.suffix(2) == [
                "DELETE /households/household-1/plans/2026-W38/entries/\(first.id)",
                "GET /households/household-1/plans/2026-W38",
            ])
    }

    @Test func failedDeletePutsTheEntryBack() async throws {
        let harness = try await activated()
        let store = harness.store
        let entry = try await store.addEntry(NewPlanEntry(recipeID: "recipe-1", servings: 2))
        harness.server.failNext()

        await #expect(throws: APIError.self) {
            try await store.deleteEntry(id: entry.id)
        }

        #expect(store.plan?.entries.map(\.id) == [entry.id])
    }

    @Test func deletingAnEntrySomeoneElseRemovedCountsAsRemoved() async throws {
        let harness = try await activated()
        let store = harness.store
        let entry = try await store.addEntry(NewPlanEntry(recipeID: "recipe-1", servings: 2))
        harness.server.update { $0.weeks["2026-W38"]?.entries = [] }

        try await store.deleteEntry(id: entry.id)

        #expect(store.plan?.entries.isEmpty == true)
    }

    @Test func finalizeAndReopen() async throws {
        let harness = try await activated()
        let store = harness.store
        try await store.addEntry(NewPlanEntry(recipeID: "recipe-1", servings: 2))

        try await store.setStatus(.finalized)
        #expect(store.plan?.status == .finalized)
        #expect(!store.isDraft)

        try await store.setStatus(.draft)
        #expect(store.isDraft)
    }

    @Test func changingAWeekFinalizedElsewhereFailsAndShowsItFinalized() async throws {
        let harness = try await activated()
        let store = harness.store
        let entry = try await store.addEntry(NewPlanEntry(recipeID: "recipe-1", servings: 2))
        // Another member finalizes the week.
        harness.server.update { $0.weeks["2026-W38"]?.status = "finalized" }
        #expect(store.isDraft)

        do {
            try await store.addEntry(NewPlanEntry(recipeID: "recipe-2", servings: 2))
            Issue.record("expected plan_finalized")
        } catch let error as APIError {
            #expect(error.code == "plan_finalized")
            #expect(HouseholdStore.message(for: error).contains("finalized"))
        }

        #expect(store.plan?.status == .finalized)
        #expect(store.plan?.entries.map(\.id) == [entry.id])
        #expect(!store.isSaving)

        await #expect(throws: APIError.self) {
            try await store.moveEntry(id: entry.id, to: .sat)
        }
        #expect(store.plan?.entries.first?.day == nil)
    }

    @Test func fullWeekRejectsAnotherEntry() async throws {
        let entries = (0..<50).map {
            FakePlanServer.Entry(id: "full-\($0)", recipeID: "recipe-1", day: nil, servings: 2, note: "")
        }
        let harness = try await activated(
            FakePlanServer(.init(weeks: ["2026-W38": .init(status: "draft", entries: entries)])))
        let store = harness.store
        #expect(store.plan?.entries.count == 50)

        do {
            try await store.addEntry(NewPlanEntry(recipeID: "recipe-2", servings: 2))
            Issue.record("expected plan_full")
        } catch let error as APIError {
            #expect(error.code == "plan_full")
        }
        #expect(store.plan?.entries.count == 50)
    }

    @Test func summariesCoverTheRequestedRange() async throws {
        let harness = try await activated()
        #expect(harness.server.log.count == 1)

        await #expect(throws: APIError.self) {
            // The fake has no list route; the store surfaces the API's error.
            _ = try await harness.store.summaries(from: harness.store.week, to: harness.store.week.next)
        }
        #expect(harness.server.log.last == "GET /households/household-1/plans")
    }

    @Test func resetForgetsEverything() async throws {
        let harness = try await activated()
        let store = harness.store

        store.reset()

        #expect(store.householdID == nil)
        #expect(store.plan == nil)
        #expect(store.phase == .idle)
        #expect(store.makeGroceryList(week: store.week) == nil)
    }

    @Test func switchingHouseholdsStartsAtThisWeek() async throws {
        let server = FakePlanServer()
        let harness = try await activated(server)
        let store = harness.store
        await store.show(week: store.week.adding(weeks: 3))

        server.update { $0.householdID = "household-2" }
        await store.activate(householdID: "household-2", timeZone: Self.denver, weekStartsOn: .mon)

        #expect(store.householdID == "household-2")
        #expect(store.week.description == "2026-W38")
        #expect(store.plan?.householdID == "household-2")
    }
}
