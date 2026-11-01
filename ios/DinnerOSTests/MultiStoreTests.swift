import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// Several stores at once (decision 653): Walmart and Fry's set up together, a default, and a
/// week that shops at the other store without touching the first one's setup.
struct MultiStoreTests {
    /// The household's settings as the API keeps them, for the fake below.
    nonisolated final class Stores: Sendable {
        struct State {
            var defaultProvider: String? = "walmart"
            var stores: [String] = ["walmart", "kroger"]
            var trips: [String: String] = [:]
            var log: [String] = []
            var bodies: [String: Data] = [:]
        }
        let state: Mutex<State>

        init(stores: [String], trips: [String: String]) {
            state = Mutex(State(stores: stores, trips: trips))
        }

        /// The last JSON body sent to `key` ("PUT /settings").
        func body(_ key: String) -> [String: Any] {
            let data = state.withLock { $0.bodies[key] } ?? Data()
            return (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
        }

        func json() -> Data {
            state.withLock { s in
                let stores = s.stores.map { key in
                    let isKroger = key == "kroger"
                    return #"""
                        {"provider":"\#(key)","providerName":"\#(isKroger ? "Fry's" : "Walmart")",
                         "storeId":\#(isKroger ? #""66000123""# : "null"),
                         "storeName":\#(isKroger ? #""Fry's Test Store""# : "null"),
                         "banner":\#(isKroger ? #""frys""# : "null"),"isDefault":\#(key == s.defaultProvider)}
                        """#
                }
                let trips = s.trips.sorted { $0.key < $1.key }.map {
                    #"{"week":"\#($0.key)","provider":"\#($0.value)"}"#
                }
                let provider = s.defaultProvider.map { #""\#($0)""# } ?? "null"
                return Data(
                    #"""
                    {"provider":\#(provider),"storeId":null,"storeName":null,"providerName":null,
                     "stores":[\#(stores.joined(separator: ","))],"trips":[\#(trips.joined(separator: ","))],
                     "updatedBy":null,"updatedAt":null}
                    """#.utf8)
            }
        }
    }

    nonisolated enum JSON {
        static let providers = Data(
            #"""
            {"items":[
              {"key":"walmart","name":"Walmart","affiliateTracked":false,
               "capabilities":{"handoff":"cart_link","pasteProductLink":true,"storeId":true}},
              {"key":"kroger","name":"Fry's","affiliateTracked":false,
               "capabilities":{"handoff":"cart_write","pasteProductLink":true,"storeId":true,"productSearch":true,
                               "storeFinder":true,"cartWrite":true,"customerAccount":true}}
            ]}
            """#.utf8)
    }

    private struct Harness {
        let store: ShoppingStore
        let stores: Stores
        let server: FakeShoppingServer
    }

    /// "Now" is in 2026-W38 in Denver.
    private func makeHarness(
        stores setUp: [String] = ["walmart", "kroger"], trips: [String: String] = [:]
    ) async throws -> Harness {
        let stores = Stores(stores: setUp, trips: trips)
        let server = FakeShoppingServer(.init())
        let transport = StubTransport { request in
            let path = request.url?.path() ?? ""
            let method = request.httpMethod ?? "GET"
            let data = request.httpBody ?? Data()
            let body = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
            let base = "/api/v1/households/household-1/shopping"
            if path.hasSuffix("/shopping/providers") { return (200, JSON.providers) }
            if path.hasSuffix("/kroger/connection") {
                return (
                    200, Data(#"{"provider":"kroger","connected":true,"connectedBy":null,"connectedAt":null}"#.utf8)
                )
            }
            let handled: Bool = stores.state.withLock { s in
                let key = "\(method) \(path.replacingOccurrences(of: base, with: ""))"
                switch (method, path) {
                case ("GET", base + "/settings"):
                    break
                case ("PUT", base + "/settings/default"):
                    s.defaultProvider = body["provider"] as? String
                case ("DELETE", base + "/settings/stores/kroger"):
                    s.stores.removeAll { $0 == "kroger" }
                    s.trips = s.trips.filter { $0.value != "kroger" }
                    if s.defaultProvider == "kroger" { s.defaultProvider = s.stores.first }
                case ("PUT", base + "/settings"):
                    let provider = body["provider"] as? String ?? ""
                    if !s.stores.contains(provider) { s.stores.append(provider) }
                    if body["makeDefault"] as? Bool != false { s.defaultProvider = provider }
                case ("PUT", _) where path.hasPrefix(base + "/weeks/") && path.hasSuffix("/store"):
                    let week = path.split(separator: "/").dropLast().last.map(String.init) ?? ""
                    s.trips[week] = body["provider"] as? String
                default:
                    return false
                }
                s.log.append(key)
                s.bodies[key] = data
                return true
            }
            if handled { return (200, stores.json()) }
            return server.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let instant = try #require(JSONCoding.parseDate("2026-09-16T12:00:00Z"))
        let store = ShoppingStore(
            session: session, api: ShoppingAPI(client: client), checks: InMemoryGroceryChecks(), now: { instant },
            openURL: { _ in true }, productCheck: StubProductCheck())
        store.activate(
            householdID: "household-1", timeZone: TimeZone(identifier: "America/Denver") ?? .gmt, weekStartsOn: .mon)
        store.setPermissions(canEdit: true, canConfirm: true)
        return Harness(store: store, stores: stores, server: server)
    }

    @Test func settingsFromAnOlderAPIHaveOneStore() throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let old = try decoder.decode(
            ShoppingSettings.self,
            from: Data(#"{"provider":"walmart","storeId":"1234","updatedBy":null,"updatedAt":null}"#.utf8))
        #expect(old.stores.map(\.provider) == ["walmart"])
        #expect(old.stores.first?.isDefault == true && old.store(for: "walmart")?.storeID == "1234")
        #expect(old.tripProvider(week: "2026-W38") == "walmart")
        let none = try decoder.decode(
            ShoppingSettings.self,
            from: Data(#"{"provider":null,"storeId":null,"updatedBy":null,"updatedAt":null}"#.utf8))
        #expect(none.stores.isEmpty && none.tripProvider(week: "2026-W38") == nil)
    }

    @Test func aWeekShopsAtTheDefaultUntilItPicksAnother() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()

        #expect(store.isConfigured && store.canSwitchStores)
        #expect(store.provider == "walmart" && store.isDefaultTrip)
        #expect(store.setUpStores.map(\.provider) == ["walmart", "kroger"])
        #expect(store.providerName == "Walmart")
        #expect(harness.server.log.contains("POST /households/household-1/plans/2026-W38/shopping/walmart/match"))

        try await store.shop(at: "kroger")

        #expect(harness.stores.body("PUT /weeks/2026-W38/store")["provider"] as? String == "kroger")
        #expect(store.provider == "kroger" && !store.isDefaultTrip)
        #expect(store.providerName == "Fry's")
        #expect(store.proposal?.week == "2026-W38")
        #expect(
            harness.server.log.last { $0.hasSuffix("/match") }
                == "POST /households/household-1/plans/2026-W38/shopping/kroger/match")

        // Next week isn't picked, so it shops at the default.
        try await store.show(week: try #require(ISOWeek("2026-W39")))
        #expect(store.provider == "walmart")
        #expect(
            harness.server.log.last { $0.hasSuffix("/match") }
                == "POST /households/household-1/plans/2026-W39/shopping/walmart/match")

        // Picking the default again clears the week's pick.
        try await store.show(week: try #require(ISOWeek("2026-W38")))
        #expect(store.provider == "kroger")
        try await store.shop(at: "walmart")
        #expect(harness.stores.body("PUT /weeks/2026-W38/store")["provider"] is NSNull)
        #expect(store.provider == "walmart")
    }

    @Test func addingAStoreKeepsTheDefault() async throws {
        let harness = try await makeHarness(stores: ["walmart"])
        let store = harness.store
        await store.load()
        #expect(!store.canSwitchStores)

        let location = ShoppingStoreLocation(storeID: "66000123", name: "Fry's Test Store", address: "1 Test Rd")
        try await store.saveStore(provider: "kroger", store: location, makeDefault: false)

        #expect(harness.stores.body("PUT /settings")["makeDefault"] as? Bool == false)
        #expect(store.provider == "walmart" && store.canSwitchStores)
        #expect(store.settings?.store(for: "kroger")?.storeName == "Fry's Test Store")
    }

    @Test func theDefaultAndRemovingAStore() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.load()

        try await store.makeDefault("kroger")
        #expect(store.provider == "kroger" && store.setUpStores.first?.provider == "kroger")

        try await store.removeStore("kroger")
        #expect(harness.stores.state.withLock { $0.log.last } == "DELETE /settings/stores/kroger")
        #expect(store.provider == "walmart" && !store.canSwitchStores && store.isConfigured)
        #expect(store.preferences.isEmpty || store.preferences.allSatisfy { $0.provider == "walmart" })
    }

    @Test func aStoreTheAPIStoppedOfferingFallsBackToWalmart() async throws {
        let harness = try await makeHarness(trips: ["2026-W38": "target"])
        await harness.store.load()
        #expect(harness.store.provider == "walmart")
    }
}
