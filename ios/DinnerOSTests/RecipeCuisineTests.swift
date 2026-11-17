import Foundation
import Testing

@testable import DinnerOS

/// A recipe's cuisine: what the row says, what the list offers, what Edit Recipe sends, and
/// how a title's guess fills in until the member picks (decisions 720 to 723).
struct RecipeCuisineTests {
    private func recipe(cuisineValues: String = #"["mexican"]"#) throws -> Recipe {
        let json = """
            {"id":"r1","householdId":"h1","source":"hellofresh","name":"Carnitas Tacos","isAddon":false,"servings":[2],
             "cuisines":["Mexican"],"cuisineValues":\(cuisineValues),"cuisineLabels":["Mexican"],
             "tags":[],"orderWeeks":[],"timesOrdered":0,
             "createdAt":"2026-11-01T00:00:00Z","updatedAt":"2026-11-01T00:00:00Z",
             "householdRating":{"count":0,"average":null},
             "ingredients":[
               {"ingredientId":"i-pork","name":"Pork","category":"meat","pantryStaple":false,"amounts":[
                 {"servings":2,"quantity":"1","quantityValue":1,"unit":"lb","sourceUnit":"","rawText":""}]}],
             "steps":[{"index":1,"text":"Braise."}]}
            """
        return try JSONCoding.makeDecoder().decode(Recipe.self, from: Data(json.utf8))
    }

    private func json(_ value: some Encodable) throws -> [String: Any] {
        let data = try JSONEncoder().encode(value)
        return try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    /// A session with no network, for a store that never sends anything.
    @MainActor
    private func session() throws -> AuthSession {
        let client = APIClient(
            baseURL: try #require(URL(string: Fixtures.baseURLString)),
            transport: StubTransport { _ in (404, Data()) })
        return AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore())
    }

    @Test func aRecipeReadsItsCuisine() throws {
        let r = try recipe()
        #expect(r.cuisineValues == ["mexican"])
        #expect(r.cuisineLabels == ["Mexican"])
    }

    @Test func anOlderServerHasNoCuisineValues() throws {
        let json = """
            {"id":"r1","householdId":"h1","source":"manual","name":"Soup","isAddon":false,"servings":[2],
             "cuisines":[],"tags":[],"orderWeeks":[],"timesOrdered":0,"ingredients":[],"steps":[],
             "createdAt":"2026-11-01T00:00:00Z","updatedAt":"2026-11-01T00:00:00Z",
             "householdRating":{"count":0,"average":null}}
            """
        let r = try JSONCoding.makeDecoder().decode(Recipe.self, from: Data(json.utf8))
        #expect(r.cuisineValues.isEmpty)
        #expect(r.cuisineLabels.isEmpty)
    }

    /// A new cuisine alone sends only the cuisine, so a meal kit recipe isn't marked edited.
    @Test func aCuisineAloneSendsOnlyTheCuisine() throws {
        var draft = RecipeEditDraft(recipe: try recipe())
        #expect(draft.cuisines == ["mexican"])
        #expect(!draft.hasChanges)
        draft.cuisines = ["italian"]
        #expect(draft.hasChanges)
        let body = try json(draft.request)
        #expect(body["cuisines"] as? [String] == ["italian"])
        #expect(body["ingredients"] == nil)
        #expect(body["steps"] == nil)

        draft.cuisines = []
        #expect(try json(draft.request)["cuisines"] as? [String] == [])
    }

    @Test func aCuisineWithOtherChangesSendsBoth() throws {
        var draft = RecipeEditDraft(recipe: try recipe())
        draft.cuisines = ["tex-mex"]
        draft.steps = ["Braise low and slow."]
        let body = try json(draft.request)
        #expect(body["cuisines"] as? [String] == ["tex-mex"])
        #expect(body["steps"] as? [String] == ["Braise low and slow."])
    }

    @Test func anUnchangedCuisineIsLeftOut() throws {
        var draft = RecipeEditDraft(recipe: try recipe())
        draft.steps = ["Braise low and slow."]
        #expect(try json(draft.request)["cuisines"] == nil)
    }

    @Test func theRowSaysWhatIsPicked() {
        let options = RecipeCuisines.standard
        #expect(RecipeCuisines.summary([], in: options) == "None")
        #expect(RecipeCuisines.summary(["mexican"], in: options) == "Mexican")
        #expect(RecipeCuisines.summary(["mexican", "middle eastern"], in: options) == "Mexican and Middle Eastern")
        #expect(RecipeCuisines.label(for: "klingon fusion", in: options) == "Klingon Fusion")
    }

    @Test func theListKeepsTheRecipesOwnCuisineAndSearches() {
        let choices = RecipeCuisines.choices([], keeping: ["klingon", "mexican"])
        #expect(choices.first?.value == "klingon")
        #expect(choices.filter { $0.value == "mexican" }.count == 1)
        #expect(RecipeCuisines.filter(choices, query: "ital").map(\.value) == ["italian"])
        #expect(RecipeCuisines.filter(choices, query: " ").count == choices.count)
        #expect(RecipeCuisines.standard.contains { $0.value == "mediterranean" })
        #expect(RecipeCuisines.standard.contains { $0.value == "middle eastern" })
    }

    @Test @MainActor func aGuessFillsInUntilTheMemberPicks() async throws {
        let store = RecipeDraftStore.preview(session: try session(), draft: RecipeDraft(name: "Carnitas Tacos"))
        await store.guessCuisine { _ in ["mexican"] }
        #expect(store.draft.cuisines == ["mexican"])
        #expect(store.draft.cuisinesChosen == nil)

        // A new title replaces an earlier guess.
        store.draft.name = "Pesto Penne"
        await store.guessCuisine { _ in ["italian"] }
        #expect(store.draft.cuisines == ["italian"])

        // Once the member picks, even None, no guess replaces it.
        store.pickCuisines([])
        store.draft.name = "Chicken Tikka Masala"
        await store.guessCuisine { _ in ["indian"] }
        #expect(store.draft.cuisines.isEmpty)
        #expect(store.draft.cuisinesChosen == true)
    }

    @Test @MainActor func aGuessNeverReplacesAPagesOwnCuisine() async throws {
        let store = RecipeDraftStore.preview(
            session: try session(), draft: RecipeDraft(name: "Weeknight Chili", cuisines: ["tex-mex"]))
        await store.guessCuisine { _ in ["mexican"] }
        #expect(store.draft.cuisines == ["tex-mex"])
    }

    @Test func aPickIsSentWithTheDraft() throws {
        var draft = RecipeDraft(name: "Tacos")
        #expect(try json(draft)["cuisinesChosen"] == nil)
        draft.cuisines = ["mexican"]
        draft.cuisinesChosen = true
        let body = try json(draft)
        #expect(body["cuisinesChosen"] as? Bool == true)
        #expect(body["cuisines"] as? [String] == ["mexican"])
    }
}
