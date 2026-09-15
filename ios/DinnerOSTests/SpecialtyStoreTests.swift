import Foundation
import Testing

@testable import DinnerOS

struct SpecialtyStoreTests {
    private struct Harness {
        let store: SpecialtyStore
        let server: FakeSpecialtyServer
    }

    private final class BatchRecorder {
        var items: [PantryItem] = []
        var householdIDs: [String] = []
    }

    private let path = "/households/household-1/specialty-ingredients"

    private func makeHarness(activate: Bool = true) async throws -> Harness {
        let server = FakeSpecialtyServer()
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let store = SpecialtyStore(session: session, api: SpecialtiesAPI(client: client))
        if activate {
            await store.activate(householdID: "household-1")
        }
        return Harness(store: store, server: server)
    }

    @Test func activateLoadsMostUsedFirstOnce() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        #expect(store.phase == .loaded)
        #expect(store.items.map(\.id) == ["chicken-stock-concentrate", "southwest-spice-blend", "sweet-soy-glaze"])
        #expect(store.needsChoiceCount == 2)
        #expect(store.ingredient(withID: "chicken-stock-concentrate")?.isKeptAsIs == true)
        #expect(store.revision == 0)

        await store.activate(householdID: "household-1")
        #expect(harness.server.log == ["GET \(path)"])
    }

    @Test func useSuggestedForAllChoosesDefaultsAndSummarizes() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        let outcome = try await store.applyDefaults()

        #expect(outcome.chosenNames == ["Southwest Spice Blend", "Sweet Soy Glaze"])
        #expect(outcome.skipped == 1)
        #expect(
            outcome.summary
                == "Chose the suggested option for 2 specialty ingredients. 1 already had a choice and wasn't changed.")
        #expect(store.needsChoiceCount == 0)
        #expect(store.ingredient(withID: "southwest-spice-blend")?.hasBatchChoice == true)
        #expect(store.ingredient(withID: "sweet-soy-glaze")?.choice?.type == .storeAlternative)
        #expect(store.revision == 1)

        let again = try await store.applyDefaults()
        #expect(again.chosenNames.isEmpty)
        #expect(again.skipped == 3)
        #expect(again.summary == "Every specialty ingredient already had a choice, so nothing changed.")
        #expect(
            SpecialtyDefaultsOutcome(chosenNames: ["Fry Seasoning"], skipped: 0).summary
                == "Chose the suggested option for Fry Seasoning.")
    }

    @Test func chooseKeepAsIsAndClear() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        let chosen = try await store.choose(optionID: "sweet-soy-glaze.batch", forSpecialtyWithID: "sweet-soy-glaze")
        #expect(chosen.choice?.type == .houseMadeBatch)
        #expect(store.ingredient(withID: "sweet-soy-glaze")?.chosenOption?.name == "Sweet soy glaze (house batch)")
        #expect(harness.server.bodies(endingWith: "/choice").first?["optionId"] as? String == "sweet-soy-glaze.batch")

        try await store.keepAsIs(specialtyID: "sweet-soy-glaze")
        #expect(store.ingredient(withID: "sweet-soy-glaze")?.isKeptAsIs == true)
        #expect(harness.server.choice(for: "sweet-soy-glaze") == "as_is")

        try await store.clearChoice(specialtyID: "sweet-soy-glaze")
        #expect(store.ingredient(withID: "sweet-soy-glaze")?.choice == nil)
        #expect(
            harness.server.log.suffix(2) == ["DELETE \(path)/sweet-soy-glaze/choice", "GET \(path)/sweet-soy-glaze"])
        #expect(store.revision == 3)
        // The list keeps its order.
        #expect(store.items.map(\.id) == ["chicken-stock-concentrate", "southwest-spice-blend", "sweet-soy-glaze"])
    }

    @Test func aRejectedChoiceChangesNothing() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        do {
            try await store.choose(optionID: "not-an-option", forSpecialtyWithID: "sweet-soy-glaze")
            Issue.record("Expected a validation error")
        } catch let error as APIError {
            #expect(error.status == 400)
        }

        #expect(store.ingredient(withID: "sweet-soy-glaze")?.choice == nil)
        #expect(store.revision == 0)
    }

    @Test func customizeCopiesAnOptionAndChoosesTheCopy() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        let options = try #require(store.ingredient(withID: "southwest-spice-blend")?.options)
        let original = try #require(options.first { $0.isBatch })
        var draft = SpecialtyOptionDraft(copying: original)
        draft.ingredients[0].quantityText = "1 1/2"

        let updated = try await store.addOption(try draft.request(), toSpecialtyWithID: "southwest-spice-blend")

        let copy = try #require(updated.options.last)
        #expect(copy.isHousehold)
        #expect(copy.basedOnOptionID == original.id)
        #expect(copy.name == "Southwest spice blend (house blend) (custom)")
        #expect(updated.choice?.optionID == copy.id)
        #expect(store.ingredient(withID: "southwest-spice-blend")?.chosenOption?.id == copy.id)
        #expect(
            harness.server.log.suffix(2) == [
                "POST \(path)/southwest-spice-blend/options", "PUT \(path)/southwest-spice-blend/choice",
            ])
        let sent = try #require(harness.server.bodies(endingWith: "/options").first)
        #expect(sent["basedOnOptionId"] as? String == original.id)
        let ingredients = try #require(sent["ingredients"] as? [[String: Any]])
        #expect(ingredients.first?["quantity"] as? String == "3/2")
        #expect(store.revision == 1)
    }

    @Test func aCopyThatCantBeChosenIsKeptAndChosenOnTheNextTry() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        let original = try #require(store.ingredient(withID: "sweet-soy-glaze")?.options.first)
        harness.server.failNextChoice()

        var savedOptionID: String?
        do {
            try await store.addOption(
                try SpecialtyOptionDraft(copying: original).request(), toSpecialtyWithID: "sweet-soy-glaze")
            Issue.record("Expected the choice to fail")
        } catch let error as SpecialtyOptionNotChosenError {
            savedOptionID = error.optionID
            #expect(!error.isForbidden)
        }

        let optionID = try #require(savedOptionID)
        #expect(store.ingredient(withID: "sweet-soy-glaze")?.options.contains { $0.id == optionID } == true)
        #expect(store.ingredient(withID: "sweet-soy-glaze")?.choice == nil)

        try await store.choose(optionID: optionID, forSpecialtyWithID: "sweet-soy-glaze")
        #expect(store.ingredient(withID: "sweet-soy-glaze")?.choice?.optionID == optionID)
        #expect(harness.server.log.filter { $0.hasSuffix("/options") }.count == 1)
    }

    @Test func editingAndDeletingHouseholdOptions() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        let original = try #require(store.ingredient(withID: "sweet-soy-glaze")?.options.first)
        let added = try await store.addOption(
            try SpecialtyOptionDraft(copying: original).request(), toSpecialtyWithID: "sweet-soy-glaze")
        let copy = try #require(added.options.last)

        var draft = SpecialtyOptionDraft(editing: copy)
        draft.name = "Less sweet glaze"
        try await store.updateOption(try draft.request(), optionID: copy.id, specialtyID: "sweet-soy-glaze")
        #expect(store.ingredient(withID: "sweet-soy-glaze")?.options.last?.name == "Less sweet glaze")
        #expect(harness.server.log.contains("PUT \(path)/sweet-soy-glaze/options/\(copy.id)"))

        try await store.deleteOption(optionID: copy.id, specialtyID: "sweet-soy-glaze")
        let ingredient = try #require(store.ingredient(withID: "sweet-soy-glaze"))
        #expect(!ingredient.options.contains { $0.id == copy.id })
        #expect(ingredient.choice == nil)
        #expect(store.revision == 3)
    }

    @Test func madeABatchIsRecordedOnceAndRestocksThePantry() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        let recorder = BatchRecorder()
        store.onBatchRecorded = { item, householdID in
            recorder.items.append(item)
            recorder.householdIDs.append(householdID)
        }
        try await store.choose(optionID: "southwest-spice-blend.batch", forSpecialtyWithID: "southwest-spice-blend")

        let response = try await store.recordBatch(specialtyID: "southwest-spice-blend", clientPurchaseID: "batch-1")

        #expect(response.purchase.source == .houseMade)
        #expect(response.item.quantity == "12")
        #expect(response.item.unit == "tbsp")
        #expect(recorder.items.map(\.id) == ["item-batch-southwest-spice-blend"])
        #expect(recorder.householdIDs == ["household-1"])
        #expect(store.ingredient(withID: "southwest-spice-blend")?.batch?.remaining?.text == "12 tbsp")
        let first = try #require(harness.server.bodies(endingWith: "/batches").first)
        #expect(first["clientPurchaseId"] as? String == "batch-1")
        #expect(first["batches"] == nil)
        #expect(first["optionId"] == nil)

        // The same ID again records nothing new.
        let repeated = try await store.recordBatch(specialtyID: "southwest-spice-blend", clientPurchaseID: "batch-1")
        #expect(repeated.purchase.id == response.purchase.id)
        #expect(harness.server.batches.count == 1)

        try await store.recordBatch(
            specialtyID: "southwest-spice-blend", optionID: "southwest-spice-blend.batch", batches: 2,
            clientPurchaseID: "batch-2")
        let second = try #require(harness.server.bodies(endingWith: "/batches").last)
        #expect(second["batches"] as? Int == 2)
        #expect(second["optionId"] as? String == "southwest-spice-blend.batch")
        #expect(harness.server.batches.count == 2)
        #expect(store.ingredient(withID: "southwest-spice-blend")?.batch?.remaining?.text == "24 tbsp")
        #expect(store.revision == 4)
    }

    @Test func aLostBatchResponseIsRetriedWithTheSameIDAndRecordedOnce() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        harness.server.loseNextBatchResponse()

        do {
            try await store.recordBatch(
                specialtyID: "southwest-spice-blend", optionID: "southwest-spice-blend.batch",
                clientPurchaseID: "batch-1")
            Issue.record("Expected the response to be lost")
        } catch let error as APIError {
            #expect(error.status == 500)
        }
        try await store.recordBatch(
            specialtyID: "southwest-spice-blend", optionID: "southwest-spice-blend.batch", clientPurchaseID: "batch-1")

        #expect(harness.server.batches.count == 1)
        #expect(harness.server.log.filter { $0.hasSuffix("/batches") }.count == 2)
    }

    @Test func aBatchNeedsABatchOption() async throws {
        let harness = try await makeHarness()

        do {
            try await harness.store.recordBatch(specialtyID: "sweet-soy-glaze", clientPurchaseID: "batch-1")
            Issue.record("Expected a validation error")
        } catch let error as APIError {
            #expect(error.status == 400)
        }
        #expect(harness.server.batches.isEmpty)
        #expect(harness.store.revision == 0)
    }

    @Test func aForbiddenChangeReloadsAndThrows() async throws {
        let harness = try await makeHarness()
        let store = harness.store
        harness.server.forbidChanges()

        do {
            _ = try await store.applyDefaults()
            Issue.record("Expected a forbidden error")
        } catch let error as APIError {
            #expect(error.status == 403)
        }

        #expect(store.revision == 0)
        #expect(harness.server.log.last == "GET \(path)")
        #expect(store.needsChoiceCount == 2)
    }

    @Test func aGroceryListCanChooseBeforeTheListLoads() async throws {
        let harness = try await makeHarness(activate: false)
        let store = harness.store

        try await store.choose(
            optionID: "sweet-soy-glaze.store", forSpecialtyWithID: "sweet-soy-glaze", householdID: "household-1")

        #expect(harness.server.choice(for: "sweet-soy-glaze") == "sweet-soy-glaze.store")
        #expect(store.items.isEmpty)
        #expect(store.phase == .idle)
        #expect(store.revision == 1)
    }

    @Test func resetAndAnotherHouseholdStartOver() async throws {
        let harness = try await makeHarness()
        let store = harness.store

        store.reset()
        #expect(store.items.isEmpty)
        #expect(store.phase == .idle)
        #expect(store.householdID == nil)

        await store.activate(householdID: "household-2")
        #expect(store.householdID == "household-2")
        #expect(store.items.isEmpty)
        if case .failed = store.phase {
        } else {
            Issue.record("Expected the unknown household to fail")
        }
    }
}
