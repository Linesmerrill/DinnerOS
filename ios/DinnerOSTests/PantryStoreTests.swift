import Foundation
import Testing

@testable import DinnerOS

struct PantryStoreTests {
    private struct Harness {
        let store: PantryStore
        let server: FakePantryServer
    }

    private func makeHarness(
        _ pantries: [String: [FakePantryServer.Item]] = ["household-1": FakePantryServer.sampleItems]
    ) async throws -> Harness {
        let server = FakePantryServer(.init(pantries: pantries))
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let store = PantryStore(
            session: session, api: PantryAPI(client: client), ingredientsAPI: IngredientsAPI(client: client))
        return Harness(store: store, server: server)
    }

    private func listRequests(_ server: FakePantryServer) -> Int {
        server.log.filter { $0 == "GET /households/household-1/pantry" }.count
    }

    @Test func loadsItemsInAisleOrder() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        await store.activate(householdID: "household-1")

        #expect(store.phase == .loaded)
        #expect(store.householdID == "household-1")
        #expect(store.items.map(\.displayName) == ["Carrots", "Butter", "Olive Oil", "Za'atar"])

        await store.activate(householdID: "household-1")
        #expect(listRequests(harness.server) == 1)
    }

    @Test func addInsertsANewItemWhereAReloadWouldPutIt() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")

        let added = try await store.add(NewPantryItem(name: "Apples", status: .inStock))

        #expect(added.ingredientID == "i-apples")
        #expect(store.items.map(\.displayName) == ["Apples", "Carrots", "Butter", "Olive Oil", "Za'atar"])
        #expect(listRequests(harness.server) == 1)
    }

    @Test func addMergesIntoTheExistingItem() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")

        let merged = try await store.add(NewPantryItem(name: "olive oil", status: .low))

        #expect(merged.id == "item-oil")
        #expect(store.items.count == 4)
        let oil = try #require(store.items.first { $0.key == "olive oil" })
        #expect(oil.status == .low)
        #expect(oil.quantity == "3/2")
        #expect(oil.displayName == "Olive Oil")
        #expect(oil.isStaple)
    }

    @Test func updateAppliesTheReturnedItem() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        let butter = try #require(store.items.first { $0.id == "item-butter" })

        try await store.update(butter, changes: PantryItemChanges(quantity: "3/2", unit: "lb", expiresOn: "2026-09-18"))

        let updated = try #require(store.items.first { $0.id == "item-butter" })
        #expect(updated.quantity == "3/2")
        #expect(updated.quantityValue == 1.5)
        #expect(updated.expiresOn == "2026-09-18")
        #expect(listRequests(harness.server) == 1)
    }

    @Test func emptyChangesSendNothing() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        let butter = try #require(store.items.first { $0.id == "item-butter" })
        let requests = harness.server.log.count

        try await store.update(butter, changes: PantryItemChanges())
        try await store.setStatus(of: butter, to: .inStock)

        #expect(harness.server.log.count == requests)
    }

    @Test func markingOutClearsTheAmount() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        let oil = try #require(store.items.first { $0.id == "item-oil" })

        try await store.setStatus(of: oil, to: .out)

        let updated = try #require(store.items.first { $0.id == "item-oil" })
        #expect(updated.status == .out)
        #expect(updated.quantity == nil)
        #expect(updated.unit == nil)
        #expect(harness.server.log.last == "PATCH /households/household-1/pantry/item-oil")
    }

    @Test func deleteRemovesTheItem() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        let carrots = try #require(store.items.first { $0.id == "item-carrots" })

        try await store.delete(carrots)

        #expect(!store.items.contains { $0.id == "item-carrots" })
        #expect(!harness.server.items(in: "household-1").contains { $0.id == "item-carrots" })
    }

    @Test func changingAnItemDeletedElsewhereReloadsAndThrows() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        let butter = try #require(store.items.first { $0.id == "item-butter" })
        harness.server.remove("item-butter", from: "household-1")

        await #expect(throws: APIError.self) {
            try await store.setStatus(of: butter, to: .low)
        }

        #expect(listRequests(harness.server) == 2)
        #expect(store.phase == .loaded)
        #expect(!store.items.contains { $0.id == "item-butter" })
    }

    @Test func bulkUpdatesItemsAndReportsMissingOnes() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        harness.server.remove("item-carrots", from: "household-1")

        let outcome = try await store.setStatus(
            ofItemsWithIDs: ["item-carrots", "item-zaatar", "item-butter", "item-zaatar"], to: .inStock)

        #expect(outcome.updatedCount == 2)
        #expect(outcome.missingCount == 1)
        #expect(outcome.missingNames == ["Carrots"])
        #expect(harness.server.bulkSizes == [3])
        #expect(store.items.map(\.displayName) == ["Butter", "Olive Oil", "Za'atar"])
        #expect(store.items.allSatisfy { $0.status == .inStock })
        #expect(listRequests(harness.server) == 1)
        #expect(
            outcome.summary
                == "Marked 2 items as in stock. Carrots was no longer in the pantry. Someone may have removed it.")
    }

    @Test func bulkSplitsSelectionsLargerThanTheAPILimit() async throws {
        let many = (0..<201).map { FakePantryServer.Item(id: "item-\($0)", name: "Item \($0)", category: "other") }
        let harness = try await makeHarness(["household-1": many])
        let store = harness.store
        await store.activate(householdID: "household-1")

        let outcome = try await store.setStatus(ofItemsWithIDs: store.items.map(\.id), to: .low)

        #expect(harness.server.bulkSizes == [200, 1])
        #expect(outcome.updatedCount == 201)
        #expect(outcome.missingCount == 0)
        #expect(store.items.allSatisfy { $0.status == .low })
    }

    @Test func emptyBulkSelectionSendsNothing() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")

        let outcome = try await harness.store.setStatus(ofItemsWithIDs: [], to: .out)

        #expect(outcome.updatedCount == 0)
        #expect(harness.server.bulkSizes.isEmpty)
    }

    @Test func defaultStaplesAddOnceThenReloadWhenSkipped() async throws {
        let harness = try await makeHarness(["household-1": []])
        let store = harness.store
        await store.activate(householdID: "household-1")
        #expect(store.items.isEmpty)

        let first = try await store.addDefaultStaples()

        #expect(first.items.count == 2)
        #expect(first.skipped == 0)
        #expect(store.items.map(\.displayName) == ["Black Pepper", "Salt"])
        #expect(store.items.allSatisfy { $0.isStaple })
        #expect(listRequests(harness.server) == 1)

        harness.server.insert(.init(id: "item-other", name: "Carrots", category: "produce"), into: "household-1")
        let second = try await store.addDefaultStaples()

        #expect(second.items.isEmpty)
        #expect(second.skipped == 2)
        #expect(listRequests(harness.server) == 2)
        #expect(store.items.map(\.displayName) == ["Carrots", "Black Pepper", "Salt"])
    }

    @Test func loadFailureThenRetry() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        harness.server.failNext()

        await store.activate(householdID: "household-1")

        guard case .failed(let message) = store.phase else {
            Issue.record("expected a failure, got \(store.phase)")
            return
        }
        #expect(message.localizedCaseInsensitiveContains("server"))
        #expect(store.items.isEmpty)

        await store.retry()

        #expect(store.phase == .loaded)
        #expect(store.items.count == 4)
    }

    @Test func refreshFailureKeepsItemsOnScreen() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        harness.server.failNext()

        await store.refresh()

        #expect(store.phase == .loaded)
        #expect(store.refreshError != nil)
        #expect(store.items.count == 4)

        await store.refresh()
        #expect(store.refreshError == nil)
    }

    @Test func switchingHouseholdsClearsTheList() async throws {
        let harness = try await makeHarness([
            "household-1": FakePantryServer.sampleItems,
            "household-2": [.init(id: "item-x", name: "Rice", category: "pantry")],
        ])
        let store = harness.store
        await store.activate(householdID: "household-1")

        await store.activate(householdID: "household-2")

        #expect(store.householdID == "household-2")
        #expect(store.items.map(\.displayName) == ["Rice"])
    }

    @Test func resetForgetsEverything() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")

        store.reset()

        #expect(store.householdID == nil)
        #expect(store.phase == .idle)
        #expect(store.items.isEmpty)
        await #expect(throws: AuthSessionError.self) {
            try await store.add(NewPantryItem(name: "Salt"))
        }
    }

    @Test func catalogSearchSkipsBlankQueries() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        let results = try await store.searchCatalog(" oil ")
        let blank = try await store.searchCatalog("   ")

        #expect(results.map(\.name) == ["Oil", "Olive Oil"])
        #expect(blank.isEmpty)
        #expect(harness.server.log == ["GET /ingredients"])
    }

    @Test func existingItemMatchesByCatalogIDOrName() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")

        #expect(store.existingItem(named: " CARROTS ", ingredientID: nil)?.id == "item-carrots")
        #expect(store.existingItem(named: "Za’atar", ingredientID: nil) == nil)
        #expect(store.existingItem(named: "Apples", ingredientID: nil) == nil)
    }
}
