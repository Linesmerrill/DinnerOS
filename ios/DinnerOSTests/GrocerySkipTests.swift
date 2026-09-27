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

    var items: [GrocerySkip] { stored }

    func skip(forIngredientKey key: String) -> GrocerySkip? {
        stored.first { $0.recipeID == nil && $0.ingredientKey == key }
    }

    @discardableResult
    func skip(_ request: GrocerySkipRequest, householdID: String?) async throws -> GrocerySkip {
        if let failure { throw failure }
        requests.append(request)
        let skip = GrocerySkip(
            id: "skip-\(requests.count)", ingredientKey: request.ingredientKey, key: request.name.lowercased(),
            name: request.name, scope: request.scope, week: request.week,
            text: request.scope == .always ? "Never buying this" : "Skipped this week", recipeID: request.recipeID)
        stored.removeAll { $0.slot == request.slot }
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
        var recipeID: String?
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
                let recipeID = body["recipeId"] as? String
                // One skip per ingredient and recipe: posting again replaces it and answers 200.
                if let index = state.rows.firstIndex(where: {
                    $0.householdID == household && $0.ingredientKey == key && $0.recipeID == recipeID
                }) {
                    state.rows[index].scope = scope
                    state.rows[index].week = week
                    state.rows[index].name = name
                    return (200, Data(Self.json(state.rows[index]).utf8))
                }
                let row = Row(
                    id: "skip-\(state.nextID)", householdID: household, ingredientKey: key, name: name,
                    scope: scope, week: week, recipeID: recipeID)
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
        let recipeID = row.recipeID.map { #""\#($0)""# } ?? "null"
        let recipeName = row.recipeID.map { _ in #""Thai Coconut Curry Chicken""# } ?? "null"
        let text = row.scope == "always" ? "Never buying this" : "Skipped this week"
        return #"""
            {"id":"\#(row.id)","ingredientKey":"\#(row.ingredientKey)","key":"\#(row.name.lowercased())",
             "name":"\#(row.name)","scope":"\#(row.scope)","week":\#(week),"text":"\#(text)",
             "recipeId":\#(recipeID),"recipeName":\#(recipeName)}
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

    // MARK: - Just this dish

    @Test func aRecipeSkipStandsBesideTheHouseholdWideOne() async throws {
        let server = FakeSkipServer()
        let transport = StubTransport { request in server.handle(request) }
        let (session, client) = try await session(transport)
        let store = GrocerySkipStore(session: session, api: GrocerySkipsAPI(client: client))
        await store.activate(householdID: "household-1")

        let curry = try await store.skip(
            .leaveOut(ingredientKey: "name:cilantro", name: "Cilantro", recipeID: "recipe-curry"))
        let always = try await store.skip(.always(ingredientKey: "name:cilantro", name: "Cilantro"))
        let again = try await store.skip(
            .leaveOut(ingredientKey: "name:cilantro", name: "Cilantro", recipeID: "recipe-curry"))

        #expect(curry.scope == .recipe)
        #expect(curry.recipeID == "recipe-curry")
        #expect(curry.recipeName == "Thai Coconut Curry Chicken")
        #expect(again.id == curry.id)
        #expect(store.items.count == 2)
        #expect(store.inRecipes.map(\.id) == [curry.id])
        #expect(store.forever.map(\.id) == [always.id])
        // The household-wide skip is the one the grocery list's review asks about.
        #expect(store.skip(forIngredientKey: "name:cilantro")?.id == always.id)
        #expect(server.rows.count == 2)
    }

    @Test func aRecipeSkipRequestNamesItsRecipeAndOnlyThen() throws {
        let encoder = JSONEncoder()
        let dish =
            try JSONSerialization.jsonObject(
                with: encoder.encode(
                    GrocerySkipRequest.leaveOut(ingredientKey: "name:cilantro", name: "Cilantro", recipeID: "r1")))
            as? [String: Any]
        #expect(dish?["scope"] as? String == "recipe")
        #expect(dish?["recipeId"] as? String == "r1")
        #expect(dish?["week"] == nil)

        let always =
            try JSONSerialization.jsonObject(
                with: encoder.encode(GrocerySkipRequest.always(ingredientKey: "name:cilantro", name: "Cilantro")))
            as? [String: Any]
        #expect(always?["recipeId"] == nil)
    }

    @Test func leavingAnIngredientOutOfOneMealSendsARecipeSkipAndKeepsItsCheck() async throws {
        let skipper = FakeSkipper()
        let model = try await makeModel(skipper: skipper)
        await model.load()
        // A line two meals share: leaving it out of one keeps it on the list for the other.
        let shared = GroceryItem(
            ingredientKey: "i-onion", name: "Yellow Onion", amounts: [], quantityText: "2", unquantified: false,
            status: .toBuy,
            recipes: [GroceryRecipe(id: "r-tacos", name: "Tacos"), GroceryRecipe(id: "r-soup", name: "Soup")])
        model.toggle(shared)

        await model.skip(shared, scope: .recipe, recipeID: "r-soup")

        let request = try #require(skipper.requests.last)
        #expect(request == .leaveOut(ingredientKey: "i-onion", name: "Yellow Onion", recipeID: "r-soup"))
        #expect(model.isChecked(shared))
    }

    @Test func putBackResumesOnlyTheSkipHoldingTheItemBack() async throws {
        let skipper = FakeSkipper()
        skipper.stored = [
            GrocerySkip(
                id: "s-curry", ingredientKey: "i-cilantro", key: "cilantro", name: "Cilantro", scope: .recipe,
                week: nil, text: "Left out of Curry", recipeID: "r-curry"),
            GrocerySkip(
                id: "s-tacos", ingredientKey: "i-cilantro", key: "cilantro", name: "Cilantro", scope: .recipe,
                week: nil, text: "Left out of Tacos", recipeID: "r-tacos"),
            GrocerySkip(
                id: "s-dill", ingredientKey: "name:dill", key: "dill", name: "Dill", scope: .always, week: nil,
                text: "Never buying this"),
        ]
        let model = try await makeModel(skipper: skipper)
        await model.load()
        let leftOut = GroceryItem(
            ingredientKey: "i-cilantro", name: "Cilantro", amounts: [], quantityText: "¼ oz", unquantified: false,
            status: GroceryItemStatus(rawValue: "skipped"), recipes: [GroceryRecipe(id: "r-curry", name: "Curry")],
            skipScope: .recipe, skipText: "Left out of Curry")

        #expect(model.canPutBack(leftOut))
        await model.putBack(leftOut)

        #expect(skipper.resumed == ["s-curry"])
    }

    @Test func aComponentIsHeldBackByTheSkipOfItsRecipeLine() {
        let crema = GroceryComponent(
            specialtyID: "smoky-red-pepper-crema", specialtyName: "Smoky Red Pepper Crema", ingredientKey: "i-crema")
        let sourCream = GroceryItem(
            ingredientKey: "i-sour-cream", name: "Sour Cream", amounts: [], quantityText: "4 tsp",
            unquantified: false, status: GroceryItemStatus(rawValue: "skipped"),
            recipes: [GroceryRecipe(id: "r-tacos", name: "Tacos")], skipScope: .recipe,
            shares: [GroceryShare(recipeID: "r-tacos", recipeName: "Tacos", quantityText: "4 tsp", component: crema)])
        let skips = [
            GrocerySkip(
                id: "s-crema", ingredientKey: "i-crema", key: "smoky red pepper crema", name: "Smoky Red Pepper Crema",
                scope: .recipe, week: nil, text: "Left out of Tacos", recipeID: "r-tacos")
        ]

        #expect(GrocerySkipMatching.skips(holdingBack: sourCream, in: skips).map(\.id) == ["s-crema"])
        // A batch's component has no recipe line; its name stands in, compared without case.
        let batch = GroceryComponent(specialtyID: "blend", specialtyName: "Southwest Spice Blend")
        #expect(GroceryComponentKey.skipKey(for: batch) == "name:Southwest Spice Blend")
        let blendSkip = GrocerySkip(
            id: "s-blend", ingredientKey: "name:southwest spice blend", key: "southwest spice blend",
            name: "Southwest Spice Blend", scope: .always, week: nil, text: "Never buying this")
        #expect(blendSkip.matches(ingredientKey: GroceryComponentKey.skipKey(for: batch)))
    }

    @Test func groceryListDecodesSharesMealsAndRecipeSkips() throws {
        let json = Data(
            #"""
            {"week":"2026-W38","status":"draft","pantryApplied":true,"skipped":[],
             "meals":[{"recipeId":"r-tacos","recipeName":"Tacos","imageUrl":null,"isAddon":false,"day":"mon"}],
             "categories":[{"category":"produce","items":[
               {"ingredientKey":"i-cilantro","name":"Cilantro","amounts":[],"quantityText":"½ oz","unquantified":false,
                "status":"toBuy","recipes":[{"id":"r-tacos","name":"Tacos"}],
                "shares":[{"recipeId":"r-tacos","recipeName":"Tacos","amounts":[],"quantityText":"½ oz",
                           "unquantified":false,"combined":false,"extra":false,"component":null}]}]}],
             "skippedItems":[
               {"ingredientKey":"i-cilantro","name":"Cilantro","amounts":[],"quantityText":"¼ oz","unquantified":false,
                "status":"skipped","recipes":[{"id":"r-curry","name":"Curry"}],"skipScope":"recipe",
                "skipText":"Left out of Curry","shares":[]}]}
            """#.utf8)
        let list = try JSONDecoder().decode(GroceryList.self, from: json)

        #expect(list.meals.map(\.recipeName) == ["Tacos"])
        #expect(list.meals.first?.day == "mon")
        let listed = try #require(list.allItems.first)
        let skipped = try #require(list.skippedItems.first)
        #expect(listed.shares.first?.quantityText == "½ oz")
        #expect(skipped.skipScope == .recipe)
        // The same ingredient is on the list for one meal and left out of another: two rows.
        #expect(listed.id != skipped.id)
    }

    @Test func aSkipStoredBeforeRecipeSkipsStillDecodes() throws {
        let json = Data(
            #"""
            {"id":"s1","ingredientKey":"name:cilantro","key":"cilantro","name":"Cilantro","scope":"always",
             "week":null,"text":"Never buying this"}
            """#.utf8)
        let skip = try JSONDecoder().decode(GrocerySkip.self, from: json)
        #expect(skip.recipeID == nil)
        #expect(skip.slot == GrocerySkipSlot(ingredientKey: "name:cilantro", recipeID: nil))
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
