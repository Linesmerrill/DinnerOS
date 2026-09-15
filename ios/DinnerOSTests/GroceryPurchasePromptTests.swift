import Foundation
import Testing

@testable import DinnerOS

/// Checking off a grocery line → "Add to pantry?" → a recorded purchase.
struct GroceryPurchasePromptTests {
    private struct Harness {
        let model: GroceryListModel
        let pantryServer: FakePantryServer
        let pantry: PantryStore
    }

    private func makeHarness(canAddToPantry: Bool = true) async throws -> Harness {
        let planServer = FakePlanServer()
        let pantryServer = FakePantryServer(.init(pantries: ["household-1": FakePantryServer.sampleItems]))
        let transport = StubTransport { request in
            request.url?.path().contains("/pantry") == true
                ? pantryServer.handle(request) : planServer.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let pantry = PantryStore(session: session, api: PantryAPI(client: client), ingredientsAPI: nil)
        let model = GroceryListModel(
            householdID: "household-1", week: try #require(ISOWeek("2026-W38")), session: session,
            api: PlansAPI(client: client), checks: InMemoryGroceryChecks(), purchases: pantry,
            canAddToPantry: canAddToPantry)
        await model.load()
        return Harness(model: model, pantryServer: pantryServer, pantry: pantry)
    }

    /// Yellow Onion (1 ½ count + 8 oz), Salt (no amount), Black Pepper (1 tsp).
    private func lines(_ model: GroceryListModel) throws -> (onion: GroceryItem, salt: GroceryItem, pepper: GroceryItem)
    {
        let items = try #require(model.list?.allItems)
        try #require(items.count == 3)
        return (items[0], items[1], items[2])
    }

    @Test func checkingOffALineAsksWithItsAmount() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let onion = try lines(model).onion

        model.toggle(onion)

        #expect(model.isChecked(onion))
        let prompt = try #require(model.purchasePrompt)
        #expect(prompt.item == onion)
        #expect(model.purchaseDraft.quantityText == "1 1/2")
        #expect(model.purchaseDraft.unit == "count")
        #expect(harness.pantryServer.log.isEmpty)
    }

    @Test func confirmingRecordsAGroceryListPurchase() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let onion = try lines(model).onion
        model.toggle(onion)
        let promptID = try #require(model.purchasePrompt?.id)

        model.purchaseDraft.quantityText = "2"
        await model.confirmPurchase()

        #expect(model.purchasePrompt == nil)
        #expect(model.purchaseFailure == nil)
        #expect(model.lastRecordedPurchase == .init(id: promptID, name: "Yellow Onion"))
        #expect(model.purchasesInFlight == 0)
        #expect(model.isChecked(onion))
        let purchase = try #require(harness.pantryServer.purchases.first)
        #expect(harness.pantryServer.purchases.count == 1)
        #expect(purchase.source == "grocery_list")
        #expect(purchase.ingredientID == "i-onion")
        #expect(purchase.name == nil)
        #expect(purchase.quantity == "2")
        #expect(purchase.unit == "count")
        #expect(purchase.week == "2026-W38")
        #expect(purchase.clientPurchaseID == promptID)

        model.dismissRecordedPurchase(id: "another")
        #expect(model.lastRecordedPurchase != nil)
        model.dismissRecordedPurchase(id: promptID)
        #expect(model.lastRecordedPurchase == nil)
    }

    @Test func aLineWithoutAnAmountIsAddedUntracked() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        model.toggle(try lines(model).salt)
        #expect(model.purchaseDraft.quantityText.isEmpty)

        await model.confirmPurchase()

        let purchase = try #require(harness.pantryServer.purchases.first)
        #expect(purchase.ingredientID == "i-salt")
        #expect(purchase.quantity == nil)
        #expect(purchase.unit == nil)
    }

    @Test func skippingRecordsNothing() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let onion = try lines(model).onion
        model.toggle(onion)

        model.skipPurchase()

        #expect(model.purchasePrompt == nil)
        #expect(model.isChecked(onion))
        #expect(harness.pantryServer.log.isEmpty)
    }

    @Test func uncheckingOrCheckingAnotherLineReplacesThePrompt() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let (onion, salt, pepper) = try lines(model)

        model.toggle(onion)
        model.toggle(pepper)
        #expect(model.purchasePrompt?.item == pepper)

        // Unchecking another line keeps the prompt; unchecking its own line closes it.
        model.toggle(onion)
        #expect(model.purchasePrompt?.item == pepper)
        model.toggle(pepper)
        #expect(model.purchasePrompt == nil)

        model.toggle(salt)
        model.uncheckAll()
        #expect(model.purchasePrompt == nil)
        #expect(harness.pantryServer.log.isEmpty)
    }

    @Test func dontAskDuringThisTripLastsForTheListVisit() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let (onion, salt, _) = try lines(model)
        model.toggle(onion)

        model.stopAskingThisTrip()

        #expect(model.purchasePrompt == nil)
        #expect(model.isSkippingPurchasePrompts)
        model.toggle(salt)
        #expect(model.purchasePrompt == nil)
        #expect(harness.pantryServer.log.isEmpty)

        let nextTrip = try await makeHarness()
        nextTrip.model.toggle(try lines(nextTrip.model).onion)
        #expect(nextTrip.model.purchasePrompt != nil)
    }

    @Test func membersWithoutPantryEditAreNotAsked() async throws {
        let harness = try await makeHarness(canAddToPantry: false)
        let model = harness.model
        let (onion, salt, _) = try lines(model)

        model.toggle(onion)
        #expect(model.purchasePrompt == nil)

        model.setCanAddToPantry(true)
        model.toggle(salt)
        #expect(model.purchasePrompt != nil)
        model.setCanAddToPantry(false)
        #expect(model.purchasePrompt == nil)
    }

    @Test func aForbiddenPurchaseStopsAsking() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        let (onion, _, pepper) = try lines(model)
        harness.pantryServer.forbidPurchases()
        model.toggle(onion)

        await model.confirmPurchase()

        let failure = try #require(model.purchaseFailure)
        #expect(failure.isForbidden)
        #expect(!model.canAddToPantry)
        model.toggle(pepper)
        #expect(model.purchasePrompt == nil)

        let requests = harness.pantryServer.log.count
        await model.retryPurchase()
        #expect(harness.pantryServer.log.count == requests)
        #expect(model.purchaseFailure != nil)
    }

    @Test func aLostResponseIsRetriedWithTheSameIDAndRecordedOnce() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        model.toggle(try lines(model).onion)
        let promptID = try #require(model.purchasePrompt?.id)
        harness.pantryServer.loseNextPurchaseResponse()

        await model.confirmPurchase()

        let failure = try #require(model.purchaseFailure)
        #expect(!failure.isForbidden)
        #expect(failure.prompt.id == promptID)
        #expect(model.lastRecordedPurchase == nil)

        await model.retryPurchase()

        #expect(model.purchaseFailure == nil)
        #expect(model.lastRecordedPurchase?.id == promptID)
        #expect(harness.pantryServer.purchases.count == 1)
        #expect(harness.pantryServer.purchases.first?.clientPurchaseID == promptID)
        #expect(
            harness.pantryServer.log.filter { $0 == "POST /households/household-1/pantry/purchases" }.count == 2)
    }

    @Test func anInvalidAmountIsNotSent() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        model.toggle(try lines(model).onion)

        model.purchaseDraft.quantityText = "a handful"
        await model.confirmPurchase()

        #expect(model.purchasePrompt != nil)
        #expect(model.purchaseDraft.quantityError != nil)
        #expect(harness.pantryServer.log.isEmpty)
    }

    @Test func aPurchaseUpdatesALoadedPantry() async throws {
        let harness = try await makeHarness()
        let model = harness.model
        await harness.pantry.activate(householdID: "household-1")
        model.toggle(try lines(model).onion)

        await model.confirmPurchase()

        let onion = try #require(harness.pantry.items.first { $0.ingredientID == "i-onion" })
        #expect(onion.quantity == "3/2")
        #expect(onion.estimate?.percentRemaining == 100)
    }
}
