import Foundation
import Testing

@testable import DinnerOS

struct PantryUsageStoreTests {
    private struct Harness {
        let store: PantryStore
        let server: FakePantryServer
        /// Calls to `onChange`.
        let changes: Counter
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
        let store = PantryStore(session: session, api: PantryAPI(client: client), ingredientsAPI: nil)
        let changes = Counter()
        store.onChange = { changes.increment() }
        return Harness(store: store, server: server, changes: changes)
    }

    private func listRequests(_ server: FakePantryServer, household: String = "household-1") -> Int {
        server.log.filter { $0 == "GET /households/\(household)/pantry" }.count
    }

    @Test func loadingAndChangesReportForTheBadge() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        await store.activate(householdID: "household-1")
        #expect(harness.changes.value == 1)

        let carrots = try #require(store.items.first { $0.id == "item-carrots" })
        try await store.setStatus(of: carrots, to: .inStock)
        #expect(harness.changes.value == 2)
    }

    @Test func restockAppliesTheTrackedItem() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        let purchase = NewPantryPurchase(
            itemID: "item-carrots", source: .manual, quantity: "6", unit: "count", clientPurchaseID: "client-1")

        let response = try await store.recordPurchase(purchase)

        #expect(response.purchase.itemID == "item-carrots")
        let carrots = try #require(store.items.first { $0.id == "item-carrots" })
        #expect(carrots.status == .inStock)
        #expect(carrots.statusSource == .person)
        #expect(carrots.quantity == "6")
        #expect(carrots.estimate?.percentRemaining == 100)
        #expect(listRequests(harness.server) == 1)
        #expect(harness.changes.value == 2)
        #expect(harness.server.purchases.map(\.source) == ["manual"])
    }

    @Test func groceryPurchaseAddsAnItemThePantryDidntHave() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")

        try await store.recordPurchase(
            NewPantryPurchase(
                ingredientID: "i-apples", source: .groceryList, quantity: "4", unit: "count", week: "2026-W38",
                clientPurchaseID: "client-apples"))

        #expect(store.items.map(\.displayName) == ["Apples", "Carrots", "Butter", "Olive Oil", "Za'atar"])
    }

    @Test func aRepeatedClientPurchaseIDRecordsOnce() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        let purchase = NewPantryPurchase(
            itemID: "item-butter", source: .manual, quantity: "2", unit: "lb", clientPurchaseID: "client-same")

        let first = try await store.recordPurchase(purchase)
        let second = try await store.recordPurchase(purchase)

        #expect(first.purchase.id == second.purchase.id)
        #expect(harness.server.purchases.count == 1)
    }

    @Test func aPurchaseForAnotherHouseholdLeavesTheShownPantryAlone() async throws {
        let harness = try await makeHarness([
            "household-1": FakePantryServer.sampleItems,
            "household-2": [.init(id: "item-rice", name: "Rice", category: "pantry", status: "out")],
        ])
        let store = harness.store
        await store.activate(householdID: "household-1")
        let before = store.items

        try await store.recordPurchase(
            NewPantryPurchase(itemID: "item-rice", source: .manual, quantity: "1", unit: "lb", clientPurchaseID: "c"),
            householdID: "household-2")

        #expect(store.items == before)
        #expect(harness.server.items(in: "household-2").first?.status == "in_stock")
    }

    @Test func aGroceryListCanRecordBeforeThePantryLoads() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        try await store.recordPurchase(
            NewPantryPurchase(ingredientID: "i-salt", source: .groceryList, clientPurchaseID: "c"),
            householdID: "household-1")

        #expect(store.phase == .idle)
        #expect(store.items.isEmpty)
        #expect(harness.server.items(in: "household-1").contains { $0.name == "Salt" })
        #expect(harness.changes.value == 1)
    }

    @Test func aForbiddenPurchaseReloadsAndThrows() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        harness.server.forbidPurchases()

        await #expect(throws: APIError.self) {
            try await store.recordPurchase(
                NewPantryPurchase(itemID: "item-butter", source: .manual, clientPurchaseID: "c"))
        }

        #expect(listRequests(harness.server) == 2)
        #expect(harness.server.purchases.isEmpty)
    }

    @Test func purchaseHistoryIsNewestFirst() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        for id in ["client-1", "client-2"] {
            try await store.recordPurchase(
                NewPantryPurchase(
                    itemID: "item-butter", source: .manual, quantity: "1", unit: "lb", clientPurchaseID: id))
        }

        let history = try await store.purchases(ofItemWithID: "item-butter")

        #expect(history.map(\.clientPurchaseID) == ["client-2", "client-1"])
        #expect(harness.server.log.last == "GET /households/household-1/pantry/item-butter/purchases")
    }

    @Test func householdThresholdLoadsAndUpdatesEstimates() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        try await store.recordPurchase(
            NewPantryPurchase(itemID: "item-butter", source: .manual, quantity: "1", unit: "lb", clientPurchaseID: "c"))

        let loaded = try await store.loadSettings()
        #expect(loaded.lowThresholdPercent == 80)
        #expect(store.settings?.updatedBy == nil)

        try await store.updateSettings(lowThresholdPercent: 70)

        #expect(store.settings?.lowThresholdPercent == 70)
        #expect(harness.server.threshold(for: "household-1") == 70)
        #expect(listRequests(harness.server) == 2)
        let butter = try #require(store.items.first { $0.id == "item-butter" })
        #expect(butter.estimate?.lowThresholdPercent == 70)
        #expect(butter.estimate?.thresholdSource == .household)
    }

    @Test func anItemThresholdCanBeSetAndReturnedToTheHouseholds() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        await store.activate(householdID: "household-1")
        let butter = try #require(store.items.first { $0.id == "item-butter" })

        let own = try await store.update(butter, changes: PantryItemChanges(lowThresholdPercent: .percent(60)))
        #expect(own.lowThresholdPercent == 60)
        #expect(store.items.first { $0.id == "item-butter" }?.lowThresholdPercent == 60)

        let inherited = try await store.update(own, changes: PantryItemChanges(lowThresholdPercent: .household))
        #expect(inherited.lowThresholdPercent == nil)
    }

    @Test func switchingHouseholdsForgetsSettings() async throws {
        let harness = try await makeHarness(["household-1": [], "household-2": []])
        let store = harness.store
        await store.activate(householdID: "household-1")
        try await store.loadSettings()
        #expect(store.settings != nil)

        await store.activate(householdID: "household-2")
        #expect(store.settings == nil)

        try await store.loadSettings()
        store.reset()
        #expect(store.settings == nil)
    }
}
