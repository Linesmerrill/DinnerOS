import Foundation
import Testing

@testable import DinnerOS

struct RecipeDraftStoreTests {
    private struct Harness {
        let store: RecipeDraftStore
        let transport: StubTransport
    }

    private func makeHarness(
        parse: Data = CatalogFixtures.draft, created: Data = RecipeFixtures.detail(), status: Int = 200
    ) async throws -> Harness {
        let transport = StubTransport { request in
            guard let path = request.url?.path() else { return (400, Data()) }
            if path.hasSuffix("/recipes/parse") {
                if status != 200 {
                    return (status, Data(#"{"error":{"code":"fetch_failed","message":"nope"}}"#.utf8))
                }
                return (200, parse)
            }
            if path.hasSuffix("/recipes") {
                return (201, created)
            }
            return (404, Data(#"{"error":{"code":"not_found","message":"no"}}"#.utf8))
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let store = RecipeDraftStore(
            session: session, api: RecipesAPI(client: client), householdID: "household-1")
        return Harness(store: store, transport: transport)
    }

    @Test func pastedTextBecomesADraftToReview() async throws {
        let harness = try await makeHarness()
        await harness.store.parse(text: "Chili\nIngredients\n- 2 cans kidney beans")
        #expect(harness.store.step == .review)
        #expect(harness.store.draft.name == "Weeknight Chili")
        #expect(harness.store.draft.ingredients.count == 1)
        #expect(harness.store.draft.warnings == ["Check the amounts."])
        // Nothing is stored by parsing.
        #expect(harness.transport.requests.allSatisfy { $0.url?.path().hasSuffix("/parse") == true })
    }

    @Test func aLinkIsReadByTheServerNotThePhone() async throws {
        let harness = try await makeHarness()
        await harness.store.parse(url: "https://example.test/recipe")
        #expect(harness.store.step == .review)
        let request = try #require(harness.transport.requests.first)
        #expect(request.url?.path().hasSuffix("/recipes/parse") == true)
        #expect(request.url?.host() == "api.example.test", "the phone must not fetch the pasted page itself")
        #expect(request.jsonBody?["url"] == "https://example.test/recipe")
    }

    @Test func aFailedParseStaysOnTheEntryStepWithAMessage() async throws {
        let harness = try await makeHarness(status: 422)
        await harness.store.parse(url: "https://example.test/not-a-recipe")
        #expect(harness.store.step == .entry)
        #expect(harness.store.errorMessage != nil)
    }

    @Test func emptyInputDoesNothing() async throws {
        let harness = try await makeHarness()
        await harness.store.parse(text: "   ")
        await harness.store.parse(url: "")
        #expect(harness.store.step == .entry)
        #expect(harness.transport.requests.isEmpty)
    }

    @Test func typingFromScratchStartsAnEmptyDraft() async throws {
        let harness = try await makeHarness()
        harness.store.startBlank()
        #expect(harness.store.step == .review)
        #expect(harness.store.draft.ingredients.count == 1)
        #expect(!harness.store.canSave, "a nameless draft can't be saved")

        harness.store.draft.name = "Grandma's Chili"
        #expect(harness.store.canSave)
    }

    @Test func savingSendsTheReviewedDraftWithoutBlankRowsOrWarnings() async throws {
        let harness = try await makeHarness()
        await harness.store.parse(text: "Chili")
        harness.store.draft.ingredients.append(RecipeDraftIngredient(name: "   "))
        harness.store.draft.steps.append("")

        let saved = await harness.store.save()
        #expect(saved?.id == "recipe-1")
        let request = try #require(harness.transport.requests.last)
        #expect(request.httpMethod == "POST")
        let sent = try #require(request.httpBody)
        let body = try #require(try JSONSerialization.jsonObject(with: sent) as? [String: Any])
        #expect((body["ingredients"] as? [Any])?.count == 1)
        #expect((body["steps"] as? [Any])?.count == 1)
        #expect((body["warnings"] as? [Any])?.isEmpty == true)
        #expect(body["fromUrl"] as? Bool == false)
    }

    @Test func startingOverDropsTheDraft() async throws {
        let harness = try await makeHarness()
        await harness.store.parse(text: "Chili")
        harness.store.startOver()
        #expect(harness.store.step == .entry)
        #expect(harness.store.draft.name.isEmpty)
    }
}
