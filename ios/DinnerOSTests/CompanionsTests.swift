import Foundation
import Testing

@testable import DinnerOS

/// Companions (decisions 715-719): the list and its choices, what the editor sends, the
/// store's changes, offline replay, and companion suggestions in the week's pairings.
struct CompanionsTests {
    private nonisolated static let listJSON = """
        {
          "items": [
            {"id": "c1", "name": "Chips and salsa", "storeItem": "Tortilla chips", "quantity": 1, "unit": "count",
             "productUrl": "https://example.com/chips", "linkedRecipe": null, "cuisines": ["mexican"],
             "recipes": [{"id": "r1", "name": "Beef Tacos"}], "mealCategories": [], "frequency": "none",
             "mode": "suggest", "summary": "Goes with Mexican and Beef Tacos · Suggested", "lastListedWeek": "2026-W47",
             "lastListedAt": "2026-11-16T18:00:00Z", "createdAt": "2026-11-16T18:00:00Z", "updatedAt": "2026-11-16T18:00:00Z"},
            {"id": "c2", "name": "Garlic Bread", "storeItem": null, "quantity": null, "unit": null, "productUrl": null,
             "linkedRecipe": {"id": "r9", "name": "Garlic Bread"}, "cuisines": [], "recipes": [],
             "mealCategories": ["pasta"], "frequency": "none", "mode": "suggest", "summary": "Goes with pasta meals · Suggested"},
            {"name": "no id, left out"}
          ],
          "frequencies": [{"value": "every_week", "label": "Each Week"}],
          "modes": [{"value": "auto", "label": "Add It for Me"}]
        }
        """

    @Test func listDecodesAndKeepsWhatItCan() throws {
        let list = try JSONCoding.makeDecoder().decode(CompanionList.self, from: Data(Self.listJSON.utf8))
        #expect(list.items.count == 2)
        let chips = try #require(list.items.first)
        #expect(chips.storeItem == "Tortilla chips")
        #expect(chips.quantity == 1)
        #expect(chips.productURL == "https://example.com/chips")
        #expect(chips.recipes == [CompanionRecipe(id: "r1", name: "Beef Tacos")])
        #expect(chips.summary.hasPrefix("Goes with Mexican"))
        let bread = list.items[1]
        #expect(bread.linkedRecipe?.name == "Garlic Bread")
        #expect(bread.mealCategories == ["pasta"])
        // The server's words win over the bundled ones.
        #expect(list.frequencies.map(\.label) == ["Each Week"])
        #expect(list.modes.map(\.value) == ["auto"])
    }

    @Test func olderServerGetsTheBundledChoices() throws {
        let list = try JSONCoding.makeDecoder().decode(CompanionList.self, from: Data(#"{"items": []}"#.utf8))
        #expect(list.frequencies.map(\.label) == ["Every Week", "Every 2 Weeks", "Once a Month", "Only With Meals It Goes With"])
        #expect(list.modes.map(\.label) == ["Add It for Me", "Just Suggest It", "Never"])
    }

    @Test func draftSendsOnlyWhatWasFilledIn() throws {
        var draft = CompanionDraft()
        #expect(!draft.canSave)
        draft.name = "  Paper towels "
        #expect(draft.canSave)
        #expect(!draft.goesWithSomething)
        var request = draft.request
        #expect(request.name == "Paper towels")
        #expect(request.storeItem == nil)
        #expect(request.quantity == nil)
        #expect(request.unit == nil)
        #expect(request.productURL == nil)
        #expect(request.frequency == "none")
        #expect(request.mode == "suggest")

        draft.storeItem = "Tortilla chips"
        draft.quantity = 2
        draft.productLink = " https://example.com/chips "
        draft.toggleCuisine("mexican")
        draft.toggleCuisine("italian")
        draft.toggleCuisine("italian")
        draft.toggleRecipe(CompanionRecipe(id: "r1", name: "Beef Tacos"))
        draft.mode = "auto"
        request = draft.request
        #expect(request.storeItem == "Tortilla chips")
        #expect(request.quantity == 2)
        #expect(request.productURL == "https://example.com/chips")
        #expect(request.cuisines == ["mexican"])
        #expect(request.recipeIDs == ["r1"])
        #expect(draft.goesWithSomething)

        let json = try #require(
            JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any])
        #expect(json["recipeIds"] as? [String] == ["r1"])
        #expect(json["productUrl"] as? String == "https://example.com/chips")
    }

    @Test func editingKeepsWhatMigrationCarriedOver() throws {
        let list = try JSONCoding.makeDecoder().decode(CompanionList.self, from: Data(Self.listJSON.utf8))
        let draft = CompanionDraft(list.items[1])
        #expect(draft.linkedRecipe?.id == "r9")
        #expect(draft.request.mealCategories == ["pasta"])
        #expect(draft.request.storeItem == nil)
    }

    @Test func companionPairingDecodes() throws {
        let json = """
            {"key": "companion:c1", "target": {"kind": "grocery_item", "groceryItem": {"name": "Tortilla chips", "quantity": 1, "unit": "count"}},
             "servings": null, "source": "companion", "frequency": "suggest", "reason": "Goes with Mexican meals",
             "inPlan": false, "canMakeRule": false, "companionId": "c1"}
            """
        let pairing = try JSONCoding.makeDecoder().decode(Pairing.self, from: Data(json.utf8))
        #expect(pairing.source == .companion)
        #expect(pairing.companionID == "c1")
        #expect(pairing.isCompanion)
        #expect(PairingFormat.reason(pairing, vocabulary: nil) == "Goes with Mexican meals")
        #expect(PairingFormat.addPrompt(pairing) == "Add Tortilla chips to the list?")
    }

    // MARK: Store

    private func makeStore(
        _ handler: @escaping StubTransport.Handler, cache: ResponseCache? = nil
    ) async throws -> (CompanionsStore, StubTransport) {
        let transport = StubTransport(handler)
        let client = APIClient(
            baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport, cache: cache)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        return (CompanionsStore(session: session, api: CompanionsAPI(client: client)), transport)
    }

    private nonisolated static let savedJSON = Data(
        #"{"id": "c3", "name": "Paper towels", "cuisines": [], "recipes": [], "mealCategories": [], "frequency": "every_week", "mode": "auto", "summary": "Every week · Added for you"}"#
            .utf8)

    @MainActor
    @Test func loadSaveAndDelete() async throws {
        let (store, transport) = try await makeStore { request in
            switch (request.httpMethod ?? "GET", request.url?.path() ?? "") {
            case ("GET", "/api/v1/households/household-1/companions"):
                return (200, Data(Self.listJSON.utf8))
            case ("POST", "/api/v1/households/household-1/companions"):
                return (201, Self.savedJSON)
            case ("PUT", "/api/v1/households/household-1/companions/c3"):
                return (200, Self.savedJSON)
            case ("DELETE", "/api/v1/households/household-1/companions/c1"):
                return (204, Data())
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
        let changes = Counter()
        store.onChange = { changes.increment() }
        await store.load(householdID: "household-1")
        #expect(store.phase == .loaded)
        #expect(store.items.map(\.id) == ["c1", "c2"])

        var draft = CompanionDraft()
        draft.name = "Paper towels"
        draft.frequency = "every_week"
        draft.mode = "auto"
        let saved = try await store.save(draft, id: nil, householdID: "household-1")
        #expect(saved.id == "c3")
        #expect(store.items.map(\.id) == ["c1", "c2", "c3"])
        _ = try await store.save(draft, id: "c3", householdID: "household-1")
        #expect(store.items.count == 3)

        try await store.delete(id: "c1", householdID: "household-1")
        #expect(store.items.map(\.id) == ["c2", "c3"])
        #expect(changes.value == 3)
        let methods = transport.requests.compactMap(\.httpMethod)
        #expect(methods == ["GET", "POST", "PUT", "DELETE"])

        store.reset()
        #expect(store.items.isEmpty)
        #expect(store.phase == .idle)
    }

    @MainActor
    @Test func saveErrorsReachTheEditor() async throws {
        let (store, _) = try await makeStore { _ in
            (400, Fixtures.errorJSON(code: "validation_failed", message: "Give it a name."))
        }
        await #expect(throws: APIError.self) {
            try await store.save(CompanionDraft(), id: nil, householdID: "household-1")
        }
        #expect(store.items.isEmpty)
    }

    @MainActor
    @Test func listReplaysOffline() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "companions-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = ResponseCache(directory: directory)
        let online = Counter()
        let (store, _) = try await makeStore(
            { _ in
                if online.value == 0 {
                    online.increment()
                    return (200, Data(Self.listJSON.utf8))
                }
                throw URLError(.notConnectedToInternet)
            }, cache: cache)
        await store.load(householdID: "household-1")
        #expect(store.items.count == 2)
        store.reset()
        await store.load(householdID: "household-1")
        #expect(store.items.count == 2)
        #expect(store.phase == .loaded)
    }
}
