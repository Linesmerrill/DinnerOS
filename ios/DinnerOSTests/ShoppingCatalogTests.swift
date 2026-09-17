import Foundation
import Testing

@testable import DinnerOS

/// The store catalog behind "Don't see your store?": decoding, search, and the request/undo
/// flow through `ShoppingStore`.
struct ShoppingCatalogTests {
    // MARK: Fixtures

    /// A catalog with one entry of every status, plus a `kind` and a `status` this build
    /// doesn't know, and one entry carrying only the required fields.
    private static let catalogJSON = Data(
        #"""
        {"items":[
          {"key":"walmart","name":"Walmart","kind":"grocer","status":"available",
           "aliases":["wal mart"],"note":null,"requestedByHousehold":false,"requests":0},
          {"key":"kroger","name":"Kroger","kind":"grocer","status":"researched",
           "aliases":["krogers","king soopers"],"note":"Needs a partner agreement.",
           "requestedByHousehold":false,"requests":3},
          {"key":"frys","name":"Fry's Food Stores","kind":"grocer","status":"unsupported",
           "aliases":["frys"],"requestedByHousehold":true,"requests":1},
          {"key":"gopuff","name":"Gopuff","kind":"bodega","status":"piloting"}
        ]}
        """#.utf8)

    nonisolated private static func requestJSON(id: String, key: String?, name: String, note: String? = nil) -> Data {
        Data(
            #"""
            {"request":{"id":"\#(id)","key":\#(ShoppingFixtures.string(key)),"name":"\#(name)",
             "status":"unsupported","note":\#(ShoppingFixtures.string(note)),
             "requestedBy":"\#(Fixtures.user.id)","requestedAt":"2026-09-15T18:30:00Z"}}
            """#.utf8)
    }

    private func decodeCatalog() throws -> [ShoppingCatalogItem] {
        try JSONCoding.makeDecoder().decode(ShoppingCatalogList.self, from: Self.catalogJSON).items
    }

    // MARK: Decoding

    @Test func catalogDecodesEveryStatusAndKeepsUnknownKindAndStatus() throws {
        let items = try decodeCatalog()

        #expect(items.count == 4)
        let walmart = try #require(items.first)
        #expect(walmart.kind == .grocer)
        #expect(walmart.status == .available)
        #expect(walmart.note == nil)
        let kroger = items[1]
        #expect(kroger.status == .researched)
        #expect(kroger.aliases == ["krogers", "king soopers"])
        #expect(kroger.requests == 3)
        // An unknown kind and status decode as-is rather than failing the whole list.
        let unknown = items[3]
        #expect(unknown.kind == ShoppingStoreKind(rawValue: "bodega"))
        #expect(unknown.status == ShoppingCatalogStatus(rawValue: "piloting"))
        // …and the absent optional fields fall back instead of throwing.
        #expect(unknown.aliases.isEmpty)
        #expect(unknown.requests == 0)
        #expect(!unknown.requestedByHousehold)
    }

    @Test func anUnknownKindStillDrawsAnIconAndAnUnknownStatusShowsNoChip() throws {
        let unknown = try decodeCatalog()[3]

        #expect(unknown.kind.systemImage == ShoppingStoreKind.other.systemImage)
        #expect(unknown.kind.name == ShoppingStoreKind.other.name)
        // Only `available` and `researched` make a claim worth a chip.
        #expect(unknown.status != .available && unknown.status != .researched)
    }

    @Test func kindIconsDifferPerKnownKind() {
        #expect(ShoppingStoreKind.grocer.systemImage == "storefront")
        #expect(ShoppingStoreKind.delivery.systemImage == "bicycle")
        #expect(ShoppingStoreKind.warehouse.systemImage == "shippingbox")
    }

    // MARK: Search

    @Test func searchMatchesNamesAndAliasesCaseAndDiacriticInsensitively() throws {
        let items = try decodeCatalog()
        func matches(_ query: String) -> [String] {
            items.filter { $0.matches(query) }.map(\.key)
        }

        #expect(matches("kro") == ["kroger"])
        // "king soopers" is only an alias of Kroger.
        #expect(matches("king") == ["kroger"])
        #expect(matches("KROGERS") == ["kroger"])
        #expect(matches("fry") == ["frys"])
        // An empty or blank query matches everything, which is what the sheet shows.
        #expect(matches("").count == items.count)
        #expect(matches("   ").count == items.count)
        #expect(matches("safeway").isEmpty)
    }

    @Test func socialProofOnlyCountsAboveOne() throws {
        let items = try decodeCatalog()

        #expect(items[1].socialProof == "3 households asked")
        // One household is usually the one reading the row, so it says nothing.
        #expect(items[2].socialProof == nil)
        #expect(items[0].socialProof == nil)
    }

    // MARK: Request bodies

    @Test func aCatalogRequestSendsTheKeyAndFreeTextSendsTheName() throws {
        let encoder = JSONCoding.makeEncoder()

        let byKey = try JSONSerialization.jsonObject(with: try encoder.encode(CreateShoppingStoreRequest.key("kroger")))
        let byName = try JSONSerialization.jsonObject(
            with: try encoder.encode(CreateShoppingStoreRequest.name("  Some Local Market  ", note: " Walkable ")))

        let keyBody = try #require(byKey as? [String: Any])
        #expect(keyBody["key"] as? String == "kroger")
        #expect(keyBody["name"] == nil)
        #expect(keyBody["note"] == nil)
        let nameBody = try #require(byName as? [String: Any])
        #expect(nameBody["key"] == nil)
        #expect(nameBody["name"] as? String == "Some Local Market")
        #expect(nameBody["note"] as? String == "Walkable")
    }

    @Test func aBlankNoteIsOmittedAndALongOneIsCutToTheLimit() throws {
        let long = String(repeating: "a", count: ShoppingRequestLimits.maxNoteLength + 50)

        #expect(CreateShoppingStoreRequest.key("kroger", note: "   ").note == nil)
        #expect(CreateShoppingStoreRequest.key("kroger", note: long).note?.count == ShoppingRequestLimits.maxNoteLength)
    }

    // MARK: Store flow

    private struct Harness {
        let store: ShoppingStore
        let transport: StubTransport
    }

    /// Routes only the catalog and request paths; everything else is `404`, which the store
    /// treats as "not built yet".
    private func makeHarness(
        catalog: Data? = ShoppingCatalogTests.catalogJSON,
        requests: Data = Data(#"{"items":[]}"#.utf8),
        onCreate: @escaping @Sendable (URLRequest) -> (Int, Data) = { _ in
            (201, ShoppingCatalogTests.requestJSON(id: "request-1", key: "kroger", name: "Kroger"))
        },
        onDelete: @escaping @Sendable () -> (Int, Data) = { (204, Data()) }
    ) async throws -> Harness {
        let transport = StubTransport { request in
            let path = request.url?.path() ?? ""
            let method = request.httpMethod ?? "GET"
            switch (method, path) {
            case ("GET", "/api/v1/shopping/catalog"):
                guard let catalog else { return (404, Fixtures.errorJSON(code: "not_found")) }
                return (200, catalog)
            case ("GET", "/api/v1/households/household-1/shopping/requests"):
                return (200, requests)
            case ("POST", "/api/v1/households/household-1/shopping/requests"):
                return onCreate(request)
            case (_, let path)
            where method == "DELETE"
                && path.hasPrefix(
                    "/api/v1/households/household-1/shopping/requests/"):
                return onDelete()
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let store = ShoppingStore(
            session: session, api: ShoppingAPI(client: client), checks: InMemoryGroceryChecks(),
            openURL: { _ in true })
        store.activate(householdID: "household-1", timeZone: .gmt, weekStartsOn: .mon)
        return Harness(store: store, transport: transport)
    }

    @Test func requestingAStoreMarksTheRowAndUndoTakesItBack() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.loadCatalog()

        #expect(store.catalogPhase == .loaded)
        #expect(store.isCatalogAvailable)
        let before = try #require(store.catalogWithRequests.first { $0.key == "kroger" })
        #expect(!before.requestedByHousehold)
        #expect(before.requests == 3)

        let created = try await store.requestStore(.key("kroger"))

        #expect(created.id == "request-1")
        let requested = try #require(store.catalogWithRequests.first { $0.key == "kroger" })
        #expect(requested.requestedByHousehold)
        // The count moves with the request, so the row doesn't need a reload to read right.
        #expect(requested.requests == 4)
        #expect(store.storeRequest(forKey: "kroger")?.id == "request-1")

        try await store.undoStoreRequest(created)

        let undone = try #require(store.catalogWithRequests.first { $0.key == "kroger" })
        #expect(!undone.requestedByHousehold)
        #expect(undone.requests == 3)
        #expect(store.storeRequest(forKey: "kroger") == nil)
    }

    @Test func aFreeTextRequestIsKeptEvenThoughNoCatalogRowMatches() async throws {
        let harness = try await makeHarness(onCreate: { _ in
            (201, Self.requestJSON(id: "request-9", key: nil, name: "Some Local Market", note: "Walkable"))
        })
        let store = harness.store
        await store.loadCatalog()

        let created = try await store.requestStore(.name("Some Local Market", note: "Walkable"))

        #expect(created.key == nil)
        #expect(created.name == "Some Local Market")
        #expect(created.note == "Walkable")
        #expect(store.storeRequests.map(\.id) == ["request-9"])
        // The typed name matches no catalog row, so the only Requested row stays the one the
        // catalog response itself reported.
        #expect(store.catalogWithRequests.filter(\.requestedByHousehold).map(\.key) == ["frys"])
    }

    @Test func aMissingCatalogEndpointHidesTheSectionInsteadOfFailing() async throws {
        let harness = try await makeHarness(catalog: nil)
        let store = harness.store

        await store.loadCatalog()

        // A 404 means the API doesn't have this yet, so it isn't an error the member should see.
        #expect(store.catalogPhase == .loaded)
        #expect(!store.isCatalogAvailable)
        #expect(store.catalog.isEmpty)
    }

    @Test func anAlreadyRemovedRequestStillCountsAsUndone() async throws {
        let harness = try await makeHarness(onDelete: { (404, Fixtures.errorJSON(code: "not_found")) })
        let store = harness.store
        await store.loadCatalog()
        let created = try await store.requestStore(.key("kroger"))

        try await store.undoStoreRequest(created)

        #expect(store.storeRequests.isEmpty)
    }

    @Test func loadedRequestsMarkTheirCatalogRows() async throws {
        let requests = Data(
            #"""
            {"items":[{"id":"request-3","key":"kroger","name":"Kroger","status":"researched","note":null,
             "requestedBy":"\#(Fixtures.user.id)","requestedAt":"2026-09-15T18:30:00Z"}]}
            """#.utf8)
        let harness = try await makeHarness(requests: requests)
        let store = harness.store

        await store.loadCatalog()

        // The catalog itself says `requestedByHousehold: false`; the request list is what's right.
        let kroger = try #require(store.catalogWithRequests.first { $0.key == "kroger" })
        #expect(kroger.requestedByHousehold)
    }

    @Test func signOutForgetsTheCatalogAndRequests() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.loadCatalog()
        _ = try await store.requestStore(.key("kroger"))

        store.reset()

        #expect(store.catalog.isEmpty)
        #expect(store.storeRequests.isEmpty)
        #expect(store.catalogPhase == .idle)
        #expect(store.isCatalogAvailable)
        #expect(!store.canRequestStore)
    }
}
