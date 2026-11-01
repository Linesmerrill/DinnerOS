import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// Whether the stub API answers.
nonisolated final class OnlineFlag: Sendable {
    private let storage = Mutex(false)
    var isOn: Bool { storage.withLock { $0 } }
    func turnOn() { storage.withLock { $0 = true } }
}

nonisolated enum UsualsFixtures {
    static let usualsJSON = Data(
        #"""
        {"week":"2026-W38","prompt":true,"shoppingWeeks":4,"window":6,"minWeeks":3,
         "items":[
           {"ingredientKey":"name:coffee","name":"Coffee","count":1,"pinned":true,"weeks":0,"onList":false},
           {"ingredientKey":"name:eggs","name":"Eggs","count":2,"pinned":false,"weeks":3,"onList":false},
           {"ingredientKey":"66e5a1f2c3b4a5d6e7f80001","name":"Milk","count":1,"pinned":false,"weeks":4,"onList":true}],
         "notUsual":[{"ingredientKey":"name:bread","name":"Bread","count":0,"pinned":false,"notUsual":true}]}
        """#.utf8)

    static let addedJSON = Data(
        #"""
        {"items":[{"id":"a1","week":"2026-W38","ingredientKey":"name:eggs","name":"Eggs","count":2,
                   "fromUsuals":true,"addedBy":"u1","addedAt":"2026-09-15T18:30:00Z"}]}
        """#.utf8)
}

/// Add Item and "Your usuals" (decision 651): the wire types, the calls, and the store's
/// offline queue and prompt.
@MainActor
struct UsualsTests {
    private let week = ISOWeek("2026-W38")

    private func api(_ transport: StubTransport) throws -> (ShoppingAPI, APIClient) {
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        return (ShoppingAPI(client: client), client)
    }

    private func store(_ transport: StubTransport, defaults: UserDefaults) async throws -> UsualsStore {
        let (api, client) = try api(transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        return UsualsStore(session: session, api: api, defaults: defaults)
    }

    private func freshDefaults() throws -> UserDefaults {
        let name = "usuals-tests-\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: name))
        defaults.removePersistentDomain(forName: name)
        return defaults
    }

    // MARK: - Wire types

    @Test func decodesAWeeksUsuals() throws {
        let usuals = try JSONDecoder().decode(WeekUsuals.self, from: UsualsFixtures.usualsJSON)
        #expect(usuals.items.count == 3 && usuals.window == 6 && usuals.minWeeks == 3)
        #expect(usuals.offered.map(\.name) == ["Coffee", "Eggs"])
        #expect(usuals.shouldPrompt)
        #expect(usuals.notUsual.first?.name == "Bread")

        let allOnList = WeekUsuals(week: "2026-W38", items: [usuals.items[2]], prompt: true)
        #expect(!allOnList.shouldPrompt, "nothing to offer, nothing to ask")
    }

    @Test func countsOnlyTheShoppingWeeksThereAre() throws {
        let usuals = try JSONDecoder().decode(WeekUsuals.self, from: UsualsFixtures.usualsJSON)
        #expect(usuals.countedWeeks == 4, "4 shopping weeks so far, of a window of 6")
        #expect(WeekUsuals(week: "2026-W38", items: [], prompt: false, shoppingWeeks: 9).countedWeeks == 6)
        #expect(WeekUsuals(week: "2026-W38", items: [], prompt: false, shoppingWeeks: 0).countedWeeks == 6)
    }

    @Test func aShopLineSaysWhenItWasAdded() throws {
        let json = Data(
            #"""
            {"ingredientKey":"name:bananas","ingredientId":null,"name":"Bananas","category":"produce","amounts":[],
             "quantityText":"1","unquantified":false,"groceryStatus":"toBuy","reason":"no_product",
             "text":"No product chosen yet","searchTerms":{"query":"bananas","qualifiers":[],"avoid":[],"why":""},"recipes":[],"shares":[],"added":true}
            """#.utf8)
        let line = try JSONDecoder().decode(ShoppingExcludedLine.self, from: json)
        #expect(line.isAdded)
        let older = try JSONDecoder().decode(
            ShoppingExcludedLine.self,
            from: Data(String(decoding: json, as: UTF8.self).replacingOccurrences(of: #","added":true"#, with: "").utf8)
        )
        #expect(!older.isAdded)
    }

    @Test func plainWords() throws {
        let usuals = try JSONDecoder().decode(WeekUsuals.self, from: UsualsFixtures.usualsJSON)
        #expect(UsualsText.why(usuals.items[0], window: 6) == "Pinned")
        #expect(UsualsText.why(usuals.items[1], window: 6) == "On your list 3 of the last 6 weeks")
        #expect(UsualsText.nameAndCount("Eggs", count: 2) == "Eggs × 2")
        #expect(UsualsText.nameAndCount("Milk", count: 1) == "Milk")
        #expect(NewGroceryItem(name: "  paper   towels ").cleanName == "paper towels")
    }

    @Test func anAddedExtraIsRemovable() throws {
        let json = Data(
            #"""
            {"ingredientKey":"name:milk","name":"Milk","amounts":[],"quantityText":"2","unquantified":false,
             "status":"toBuy","recipes":[],"extras":[{"id":"a1","origin":"added","text":"You added this"}]}
            """#.utf8)
        let item = try JSONDecoder().decode(GroceryItem.self, from: json)
        #expect(item.addedExtras.map(\.id) == ["a1"])
        #expect(item.pairingExtras.isEmpty)
    }

    // MARK: - Calls

    @Test func callsUseTheHouseholdsShoppingPaths() async throws {
        let transport = StubTransport { request in
            switch (request.httpMethod, request.url?.path() ?? "") {
            case ("GET", _): (200, UsualsFixtures.usualsJSON)
            case ("POST", let path) where path.hasSuffix("/items"): (200, UsualsFixtures.addedJSON)
            case ("PUT", _):
                (
                    200,
                    Data(
                        #"{"usual":{"ingredientKey":"name:eggs","name":"Eggs","count":0,"pinned":false,"notUsual":true}}"#
                            .utf8)
                )
            default: (204, Data())
            }
        }
        let (api, _) = try api(transport)
        let base = "/api/v1/households/household-1/shopping"

        let usuals = try await api.usuals(householdID: "household-1", week: "2026-W38", accessToken: "t")
        #expect(usuals.items.count == 3)
        #expect(transport.requests.last?.url?.path() == base + "/weeks/2026-W38/usuals")

        let added = try await api.addItems(
            householdID: "household-1", week: "2026-W38",
            request: AddGroceryItemsRequest(items: [NewGroceryItem(name: "Eggs", count: 2)], fromUsuals: true),
            accessToken: "t")
        #expect(added.first?.id == "a1" && added.first?.fromUsuals == true)
        let sent = try #require(transport.requests.last?.httpBody)
        let body = try #require(try JSONSerialization.jsonObject(with: sent) as? [String: Any])
        #expect(body["fromUsuals"] as? Bool == true)
        #expect((body["items"] as? [[String: Any]])?.first?["count"] as? Int == 2)

        try await api.removeAddedItem(householdID: "household-1", week: "2026-W38", itemID: "a1", accessToken: "t")
        #expect(transport.requests.last?.httpMethod == "DELETE")
        #expect(transport.requests.last?.url?.path() == base + "/weeks/2026-W38/items/a1")

        try await api.dismissUsuals(householdID: "household-1", week: "2026-W38", accessToken: "t")
        #expect(transport.requests.last?.url?.path() == base + "/weeks/2026-W38/usuals/dismiss")

        let pref = try await api.setUsual(
            householdID: "household-1",
            request: SetUsualRequest(ingredientKey: "name:eggs", name: "Eggs", notUsual: true), accessToken: "t")
        #expect(pref.notUsual && transport.requests.last?.url?.path() == base + "/usuals")
    }

    // MARK: - Store

    @Test func anAddMadeOfflineWaitsAndThenGoes() async throws {
        let online = OnlineFlag()
        let transport = StubTransport { request in
            guard online.isOn else { throw URLError(.notConnectedToInternet) }
            return request.httpMethod == "GET" ? (200, UsualsFixtures.usualsJSON) : (200, UsualsFixtures.addedJSON)
        }
        let defaults = try freshDefaults()
        let store = try await store(transport, defaults: defaults)
        let week = try #require(week)

        let outcome = try await store.add(
            [NewGroceryItem(name: " Eggs ", count: 2)], week: week, householdID: "household-1")
        #expect(outcome == .queued)
        #expect(store.pendingItems(for: week, householdID: "household-1") == [NewGroceryItem(name: "Eggs", count: 2)])
        #expect(store.pendingItems(for: week, householdID: "household-2").isEmpty)

        // The queue outlives the app.
        let reopened = try await self.store(transport, defaults: defaults)
        #expect(reopened.pending.count == 1)

        online.turnOn()
        let before = reopened.revision
        await reopened.flush()
        #expect(reopened.pending.isEmpty && reopened.revision > before)
        #expect(defaults.data(forKey: UsualsStore.pendingKey) == nil)
        #expect(transport.requests(to: "/api/v1/households/household-1/shopping/weeks/2026-W38/items").count >= 2)
    }

    @Test func anItemAddedOfflineCanBeTakenBack() async throws {
        let transport = StubTransport { _ in throw URLError(.notConnectedToInternet) }
        let store = try await store(transport, defaults: try freshDefaults())
        let week = try #require(week)
        try await store.add(
            [NewGroceryItem(name: "Eggs"), NewGroceryItem(name: "Milk")], week: week, householdID: "household-1")

        store.removePending(NewGroceryItem(name: "Eggs"), week: week, householdID: "household-1")
        #expect(store.pendingItems(for: week, householdID: "household-1") == [NewGroceryItem(name: "Milk")])
        store.removePending(NewGroceryItem(name: "Milk"), week: week, householdID: "household-1")
        #expect(store.pending.isEmpty)
    }

    @Test func anAddTheAPIRefusesIsNotQueued() async throws {
        let transport = StubTransport { _ in
            (400, Data(#"{"error":{"code":"validation_failed","message":"Name each item."}}"#.utf8))
        }
        let store = try await store(transport, defaults: try freshDefaults())
        await #expect(throws: APIError.self) {
            try await store.add([NewGroceryItem(name: "Eggs")], week: try #require(week), householdID: "household-1")
        }
        #expect(store.pending.isEmpty)
    }

    @Test func thePromptIsAskedOncePerWeek() async throws {
        let transport = StubTransport { request in
            request.httpMethod == "GET" ? (200, UsualsFixtures.usualsJSON) : (204, Data())
        }
        let store = try await store(transport, defaults: try freshDefaults())
        let week = try #require(week)
        await store.load(week: week, householdID: "household-1")
        #expect(store.shouldPrompt(for: week, householdID: "household-1"))
        #expect(!store.shouldPrompt(for: week, householdID: "household-2"))

        #expect(store.ask(week: week, householdID: "household-1", from: .shop))
        #expect(store.prompt?.origin == .shop)
        #expect(!store.ask(week: week, householdID: "household-1", from: .groceryList), "one prompt at a time")

        store.promptClosed()
        #expect(store.prompt == nil)
        #expect(!store.ask(week: week, householdID: "household-1", from: .groceryList), "closed is asked")

        await store.dismiss(week: week, householdID: "household-1")
        #expect(
            transport.requests.last?.url?.path()
                == "/api/v1/households/household-1/shopping/weeks/2026-W38/usuals/dismiss")
    }
}
