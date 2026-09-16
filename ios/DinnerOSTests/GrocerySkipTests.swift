import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// An in-memory stand-in for `GrocerySkipping`, for the grocery list model's tests.
private final class FakeSkipper: GrocerySkipping, @unchecked Sendable {
    private(set) var revision = 0
    private(set) var requests: [GrocerySkipRequest] = []
    private(set) var resumed: [String] = []
    var stored: [GrocerySkip] = []
    /// When set, every change throws it.
    var failure: (any Error)?

    func skip(forIngredientKey key: String) -> GrocerySkip? {
        stored.first { $0.ingredientKey == key }
    }

    @discardableResult
    func skip(_ request: GrocerySkipRequest, householdID: String?) async throws -> GrocerySkip {
        if let failure { throw failure }
        requests.append(request)
        let skip = GrocerySkip(
            id: "skip-\(requests.count)", ingredientKey: request.ingredientKey, key: request.name.lowercased(),
            name: request.name, scope: request.scope, week: request.week,
            text: request.scope == .always ? "Never buying this" : "Skipped this week")
        stored.removeAll { $0.ingredientKey == request.ingredientKey }
        stored.append(skip)
        revision += 1
        return skip
    }

    func resume(skipID: String, householdID: String?) async throws {
        if let failure { throw failure }
        resumed.append(skipID)
        stored.removeAll { $0.id == skipID }
        revision += 1
    }
}

/// An in-memory stand-in for the `grocery-skips` endpoints.
private nonisolated final class FakeSkipServer: Sendable {
    struct Row: Sendable {
        var id: String
        var householdID: String
        var ingredientKey: String
        var name: String
        var scope: String
        var week: String?
    }

    struct State: Sendable {
        var rows: [Row] = []
        var nextID = 1
        var log: [String] = []
        /// When set, the next request answers with this status.
        var failWith: Int?
    }

    private let state = Mutex<State>(State())

    var log: [String] { state.withLock { $0.log } }
    var rows: [Row] { state.withLock { $0.rows } }

    func failNext(_ status: Int) {
        state.withLock { $0.failWith = status }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        guard let url = request.url else { return (400, Data()) }
        let method = request.httpMethod ?? "GET"
        let route = Array(url.path().split(separator: "/").map(String.init).dropFirst(2))
        let body = request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]

        return state.withLock { state in
            state.log.append("\(method) /\(route.joined(separator: "/"))")
            if let status = state.failWith {
                state.failWith = nil
                return (status, Fixtures.errorJSON(code: status == 403 ? "forbidden" : "internal"))
            }
            guard route.count >= 3, route[0] == "households", route[2] == "grocery-skips" else {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            // Skips belong to one household, as the API scopes them. Without this the fake
            // would hand another household's skips back and make the store look broken.
            let household = route[1]
            switch (method, route.count) {
            case ("GET", 3):
                let items = state.rows.filter { $0.householdID == household }.map(Self.json).joined(separator: ",")
                return (200, Data(#"{"items":[\#(items)]}"#.utf8))
            case ("POST", 3):
                guard let key = body["ingredientKey"] as? String, let scope = body["scope"] as? String else {
                    return (400, Fixtures.errorJSON(code: "validation_failed"))
                }
                let name = body["name"] as? String ?? key
                let week = body["week"] as? String
                // One skip per ingredient: posting again replaces it and answers 200.
                if let index = state.rows.firstIndex(where: {
                    $0.householdID == household && $0.ingredientKey == key
                }) {
                    state.rows[index].scope = scope
                    state.rows[index].week = week
                    state.rows[index].name = name
                    return (200, Data(Self.json(state.rows[index]).utf8))
                }
                let row = Row(
                    id: "skip-\(state.nextID)", householdID: household, ingredientKey: key, name: name,
                    scope: scope, week: week)
                state.nextID += 1
                state.rows.append(row)
                return (201, Data(Self.json(row).utf8))
            case ("DELETE", 4):
                guard
                    let index = state.rows.firstIndex(where: {
                        $0.householdID == household && $0.id == route[3]
                    })
                else {
                    return (404, Fixtures.errorJSON(code: "not_found"))
                }
                state.rows.remove(at: index)
                return (204, Data())
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    private static func json(_ row: Row) -> String {
        let week = row.week.map { #""\#($0)""# } ?? "null"
        let text = row.scope == "always" ? "Never buying this" : "Skipped this week"
        return #"""
            {"id":"\#(row.id)","ingredientKey":"\#(row.ingredientKey)","key":"\#(row.name.lowercased())",
             "name":"\#(row.name)","scope":"\#(row.scope)","week":\#(week),"text":"\#(text)"}
            """#
    }
}

struct GrocerySkipTests {
    private let week = ISOWeek("2026-W38")

    private func session(_ transport: StubTransport) async throws -> (AuthSession, APIClient) {
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        return (session, client)
    }

    private func makeModel(
        skipper: FakeSkipper, checks: any GroceryCheckStorage = InMemoryGroceryChecks(), canSkip: Bool = true
    ) async throws -> GroceryListModel {
        let server = FakePlanServer()
        let transport = StubTransport { request in server.handle(request) }
        let (session, client) = try await self.session(transport)
        return GroceryListModel(
            householdID: "household-1", week: try #require(week), session: session, api: PlansAPI(client: client),
            checks: checks, skips: skipper, canSkipIngredients: canSkip)
    }

    // MARK: - The list keeps the two kinds of "skipped" apart

    @Test func groceryListDecodesSkippedItemsWithoutPuttingThemOnTheList() async throws {
        let model = try await makeModel(skipper: FakeSkipper())

        await model.load()

        let list = try #require(model.list)
        // `skipped` is about plan entries; `skippedItems` is about ingredients.
        #expect(list.skipped.count == 1)
        #expect(list.skipped.first?.recipeName == "Retired Stew")
        #expect(model.skippedItems.count == 1)
        let cilantro = try #require(model.skippedItems.first)
        #expect(cilantro.name == "Cilantro")
        #expect(cilantro.skipScope == .always)
        #expect(cilantro.skipText == "Never buying this")
        // Nothing asks anyone to buy it, and it isn't counted as left to check off.
        #expect(!list.allItems.contains { $0.ingredientKey == "i-cilantro" })
        #expect(model.remainingCount == 3)
    }

    @Test func aListWithoutSkippedItemsStillDecodes() throws {
        // A server that doesn't know about skips yet.
        let json = Data(
            #"""
            {"week":"2026-W38","status":"draft","pantryApplied":false,"categories":[],"skipped":[]}
            """#.utf8)
        let list = try JSONDecoder().decode(GroceryList.self, from: json)
        #expect(list.skippedItems.isEmpty)
    }

    // MARK: - Skipping from the list

    @Test func skippingOnceAndForeverSendTheRightLifetime() async throws {
        let skipper = FakeSkipper()
        let model = try await makeModel(skipper: skipper)
        await model.load()
        let onion = try #require(model.list?.allItems.first { $0.ingredientKey == "i-onion" })

        await model.skip(onion, scope: .week)
        let once = try #require(skipper.requests.first)
        #expect(once.scope == .week)
        #expect(once.week == "2026-W38")
        #expect(once.ingredientKey == "i-onion")
        #expect(once.name == "Yellow Onion")

        await model.skip(onion, scope: .always)
        let forever = try #require(skipper.requests.last)
        #expect(forever.scope == .always)
        #expect(forever.week == nil)
        // Changing the lifetime replaces the skip rather than stacking a second one.
        #expect(skipper.stored.count == 1)
    }

    @Test func skippingForgetsACheckOffSoAResumedItemIsNotStruckThrough() async throws {
        let checks = InMemoryGroceryChecks()
        let skipper = FakeSkipper()
        let model = try await makeModel(skipper: skipper, checks: checks)
        await model.load()
        let onion = try #require(model.list?.allItems.first { $0.ingredientKey == "i-onion" })
        model.toggle(onion)
        #expect(model.isChecked(onion))

        await model.skip(onion, scope: .always)

        #expect(!model.checked.contains("i-onion"))
        #expect(!checks.checkedItems(householdID: "household-1", week: try #require(week)).contains("i-onion"))
    }

    @Test func skipIsHiddenWithoutPlanEdit() async throws {
        let skipper = FakeSkipper()
        let model = try await makeModel(skipper: skipper, canSkip: false)
        await model.load()
        let onion = try #require(model.list?.allItems.first)

        #expect(!model.canSkip)
        await model.skip(onion, scope: .always)
        #expect(skipper.requests.isEmpty)
    }

    @Test func aRefusedSkipTurnsTheActionOffAndSaysSo() async throws {
        let skipper = FakeSkipper()
        skipper.failure = APIError.server(status: 403, code: "forbidden", message: "Not allowed", requestID: nil)
        let model = try await makeModel(skipper: skipper)
        await model.load()
        let onion = try #require(model.list?.allItems.first)

        await model.skip(onion, scope: .always)

        let failure = try #require(model.skipFailure)
        #expect(failure.isForbidden)
        #expect(failure.name == onion.name)
        #expect(!model.canSkip)
    }

    // MARK: - The store

    @Test func storeReplacesTheSkipForAnIngredientInsteadOfStacking() async throws {
        let server = FakeSkipServer()
        let transport = StubTransport { request in server.handle(request) }
        let (session, client) = try await session(transport)
        let store = GrocerySkipStore(session: session, api: GrocerySkipsAPI(client: client))
        await store.activate(householdID: "household-1")

        let once = try await store.skip(
            GrocerySkipRequest(ingredientKey: "name:cilantro", name: "Cilantro", scope: .week, week: "2026-W38"))
        #expect(once.scope == .week)
        #expect(store.thisWeek.count == 1)
        #expect(store.forever.isEmpty)

        let forever = try await store.skip(
            GrocerySkipRequest(ingredientKey: "name:cilantro", name: "Cilantro", scope: .always, week: nil))
        #expect(forever.id == once.id)
        #expect(store.items.count == 1)
        #expect(store.forever.count == 1)
        #expect(store.thisWeek.isEmpty)
        #expect(store.skip(forIngredientKey: "name:cilantro")?.scope == .always)
        #expect(server.rows.count == 1)
    }

    @Test func storeResumeRemovesTheSkipAndTellsAnOpenList() async throws {
        let server = FakeSkipServer()
        let transport = StubTransport { request in server.handle(request) }
        let (session, client) = try await session(transport)
        let store = GrocerySkipStore(session: session, api: GrocerySkipsAPI(client: client))
        await store.activate(householdID: "household-1")
        let skip = try await store.skip(
            GrocerySkipRequest(ingredientKey: "name:cilantro", name: "Cilantro", scope: .always, week: nil))
        let revisionBefore = store.revision

        try await store.resume(skipID: skip.id)

        #expect(store.items.isEmpty)
        #expect(server.rows.isEmpty)
        #expect(store.revision > revisionBefore)
    }

    @Test func storeTreatsAnAlreadyResumedSkipAsDone() async throws {
        let server = FakeSkipServer()
        let transport = StubTransport { request in server.handle(request) }
        let (session, client) = try await session(transport)
        let store = GrocerySkipStore(session: session, api: GrocerySkipsAPI(client: client))
        await store.activate(householdID: "household-1")
        let skip = try await store.skip(
            GrocerySkipRequest(ingredientKey: "name:cilantro", name: "Cilantro", scope: .always, week: nil))
        try await store.resume(skipID: skip.id)

        // Someone else resumed it first: the end state is the one we wanted.
        try await store.resume(skipID: skip.id)
        #expect(store.items.isEmpty)
    }

    @Test func storeRemembersARefusedChange() async throws {
        let server = FakeSkipServer()
        let transport = StubTransport { request in server.handle(request) }
        let (session, client) = try await session(transport)
        let store = GrocerySkipStore(session: session, api: GrocerySkipsAPI(client: client))
        await store.activate(householdID: "household-1")
        server.failNext(403)

        await #expect(throws: (any Error).self) {
            try await store.skip(
                GrocerySkipRequest(ingredientKey: "name:cilantro", name: "Cilantro", scope: .always, week: nil))
        }
        #expect(store.isForbidden)
    }

    @Test func storeClearsWhenTheHouseholdChanges() async throws {
        let server = FakeSkipServer()
        let transport = StubTransport { request in server.handle(request) }
        let (session, client) = try await session(transport)
        let store = GrocerySkipStore(session: session, api: GrocerySkipsAPI(client: client))
        await store.activate(householdID: "household-1")
        _ = try await store.skip(
            GrocerySkipRequest(ingredientKey: "name:cilantro", name: "Cilantro", scope: .always, week: nil))
        #expect(!store.items.isEmpty)

        await store.activate(householdID: "household-2")
        #expect(store.items.isEmpty)
    }
}
