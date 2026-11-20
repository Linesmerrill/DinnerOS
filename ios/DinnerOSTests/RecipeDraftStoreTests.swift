import Foundation
import Testing

@testable import DinnerOS

struct RecipeDraftStoreTests {
    private struct Harness {
        let store: RecipeDraftStore
        let transport: StubTransport
    }

    private func makeHarness(
        parse: Data = CatalogFixtures.draft, created: Data = RecipeFixtures.detail(), status: Int = 200,
        acceptsPhonePage: Bool = false,
        /// Answers a parse request from its JSON body, in place of `status` and `acceptsPhonePage`.
        parseResponse: (@Sendable ([String: String]) -> (Int, Data))? = nil,
        pageLoader: @escaping @Sendable (String) async throws -> String = { _ in throw URLError(.notConnectedToInternet)
        },
        webPageLoader: @escaping @Sendable (String) async throws -> String = { _ in throw URLError(.timedOut) }
    ) async throws -> Harness {
        let transport = StubTransport { request in
            guard let path = request.url?.path() else { return (400, Data()) }
            if path.hasSuffix("/recipes/parse"), let parseResponse {
                return parseResponse(request.jsonBody ?? [:])
            }
            if path.hasSuffix("/recipes/parse") {
                if acceptsPhonePage, request.jsonBody?["html"] != nil {
                    return (200, parse)
                }
                if status != 200 {
                    return (status, Data(#"{"error":{"code":"fetch_failed","message":"nope"}}"#.utf8))
                }
                return (200, parse)
            }
            if path.hasSuffix("/recipe-photos") {
                guard request.value(forHTTPHeaderField: "Content-Type") == "image/jpeg" else { return (400, Data()) }
                return (201, Data(#"{"imageUrl":"https://api.test/api/v1/recipe-photos/abc"}"#.utf8))
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
        store.pageLoader = pageLoader
        store.webPageLoader = webPageLoader
        return Harness(store: store, transport: transport)
    }

    private nonisolated static func failure(_ code: String, _ message: String) -> (Int, Data) {
        (422, Data(#"{"error":{"code":"\#(code)","message":"\#(message)"}}"#.utf8))
    }

    /// Counts calls to a page loader and answers each with `answer`.
    private nonisolated final class LoaderCalls: @unchecked Sendable {
        private let lock = NSLock()
        private var links: [String] = []
        var count: Int { lock.withLock { links.count } }

        func loader(_ answer: @escaping @Sendable (String) throws -> String)
            -> @Sendable (String) async throws -> String
        {
            { link in
                self.lock.withLock { self.links.append(link) }
                return try answer(link)
            }
        }
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

    @Test func aFailedParseStaysOnTheEntryStepWithTheServersMessage() async throws {
        let harness = try await makeHarness(status: 422)
        await harness.store.parse(url: "https://example.test/not-a-recipe")
        #expect(harness.store.step == .entry)
        // The server's message is written for people, so it's shown as it is.
        #expect(harness.store.errorMessage == "nope")
        #expect(harness.store.failedLink == "https://example.test/not-a-recipe")
    }

    @Test func aFailureWithNoMessageSaysSoPlainly() {
        let error = APIError.server(status: 422, code: "fetch_failed", message: "", requestID: nil)
        #expect(
            error.errorDescription
                == "Couldn’t find a recipe at that link. Check the link, or copy the recipe and paste it as text.")
    }

    @Test func aStoreSiteGoesStraightToTheWebView() async throws {
        let fetches = LoaderCalls()
        let renders = LoaderCalls()
        let harness = try await makeHarness(
            parseResponse: { body in
                body["html"] == "<rendered>"
                    ? (200, CatalogFixtures.draft)
                    : Self.failure("page_needs_phone", "That site only shows its recipes in a web browser.")
            },
            pageLoader: fetches.loader { _ in "<shell>" },
            webPageLoader: renders.loader { _ in "<rendered>" })
        await harness.store.parse(url: "https://www.frysfood.com/r/plant-based-calzones-recipe/62519060")
        #expect(harness.store.step == .review)
        #expect(fetches.count == 0, "the phone's plain fetch only gets the store's empty shell")
        #expect(renders.count == 1)
        #expect(harness.transport.requests.count == 2)
        #expect(
            harness.transport.requests.last?.jsonBody?["url"]
                == "https://www.frysfood.com/r/plant-based-calzones-recipe/62519060")
    }

    @Test func anEmptyShellFromThePhonesFetchFallsBackToTheWebView() async throws {
        let renders = LoaderCalls()
        let harness = try await makeHarness(
            parseResponse: { body in
                switch body["html"] {
                case nil: Self.failure("fetch_failed", "That site didn't let us read the page.")
                case "<rendered>": (200, CatalogFixtures.draft)
                default: Self.failure("fetch_failed", "We couldn't find a recipe on that page.")
                }
            },
            pageLoader: { _ in "<shell>" },
            webPageLoader: renders.loader { _ in "<rendered>" })
        await harness.store.parse(url: "https://shop.example.test/r/hand-pies")
        #expect(harness.store.step == .review)
        #expect(renders.count == 1)
        #expect(harness.transport.requests.map { $0.jsonBody?["html"] } == [nil, "<shell>", "<rendered>"])
    }

    @Test func aPageThePhoneCantFetchIsOpenedInTheWebView() async throws {
        let harness = try await makeHarness(
            parseResponse: { body in
                body["html"] == "<rendered>"
                    ? (200, CatalogFixtures.draft)
                    : Self.failure("fetch_failed", "That site didn't let us read the page.")
            },
            pageLoader: { _ in throw URLError(.badServerResponse) },
            webPageLoader: { _ in "<rendered>" })
        await harness.store.parse(url: "https://example.test/blocked")
        #expect(harness.store.step == .review)
        #expect(harness.transport.requests.count == 2)
    }

    @Test func whenNothingWorksTheMessageSaysWhyAndWriteYourOwnKeepsTheLink() async throws {
        let link = "https://example.test/no-recipe-here"
        let harness = try await makeHarness(
            parseResponse: { body in
                body["html"] == nil
                    ? Self.failure("fetch_failed", "That site didn't let us read the page.")
                    : Self.failure("fetch_failed", "We couldn't find a recipe on that page.")
            },
            pageLoader: { _ in "<shell>" },
            webPageLoader: { _ in "<rendered>" })
        await harness.store.parse(url: link)
        #expect(harness.store.step == .entry)
        #expect(harness.store.errorMessage == "We couldn't find a recipe on that page.")
        #expect(harness.store.failedLink == link)
        #expect(harness.transport.requests.count == 3)

        harness.store.writeOwnKeepingLink()
        #expect(harness.store.step == .review)
        #expect(harness.store.errorMessage == nil)
        #expect(harness.store.draft.sourceURL == link)
        #expect(!harness.store.draft.fromURL, "a typed recipe keeps its steps and times")
        #expect(harness.store.draft.steps == [""])

        harness.store.draft.name = "Hand Pies"
        await harness.store.save()
        let sent = try #require(harness.transport.requests.last?.httpBody)
        let body = try #require(try JSONSerialization.jsonObject(with: sent) as? [String: Any])
        #expect(body["sourceUrl"] as? String == link)
        #expect(body["fromUrl"] as? Bool == false)
    }

    @Test func aStoreSiteTheWebViewCantLoadSaysThePageDidntLoad() async throws {
        let harness = try await makeHarness(
            parseResponse: { _ in Self.failure("page_needs_phone", "That site only shows its recipes in a web browser.")
            })
        await harness.store.parse(url: "https://www.kroger.com/r/x")
        #expect(harness.store.errorMessage == "That page didn’t load. Check the link and try again.")
        #expect(harness.store.failedLink == "https://www.kroger.com/r/x")
    }

    @Test func whenThePhoneCantLoadThePageEitherTheServersReasonIsShown() async throws {
        let harness = try await makeHarness(
            parseResponse: { _ in Self.failure("fetch_failed", "That page didn’t load. Check the link and try again.") }
        )
        await harness.store.parse(url: "https://recipes.invalid/nope")
        #expect(harness.store.errorMessage == "That page didn’t load. Check the link and try again.")
        #expect(harness.transport.requests.count == 1, "nothing to send when neither loader got the page")
    }

    @Test func otherServerErrorsAreNotRetriedOnThePhone() async throws {
        let renders = LoaderCalls()
        let harness = try await makeHarness(
            parseResponse: { _ in (403, Data(#"{"error":{"code":"forbidden","message":"no"}}"#.utf8)) },
            webPageLoader: renders.loader { _ in "<rendered>" })
        await harness.store.parse(url: "https://example.test/a")
        #expect(harness.store.errorMessage == "Your role in this household doesn’t allow that.")
        #expect(renders.count == 0)
        #expect(harness.store.failedLink == "https://example.test/a")
    }

    @Test func startingAgainClearsTheFailedLink() async throws {
        let harness = try await makeHarness(status: 422)
        await harness.store.parse(url: "https://example.test/not-a-recipe")
        harness.store.startBlank()
        #expect(harness.store.failedLink == nil)
        #expect(harness.store.draft.sourceURL == nil)
    }

    @Test func aSiteThatTurnsTheServerAwayIsLoadedOnThePhone() async throws {
        let harness = try await makeHarness(
            status: 422, acceptsPhonePage: true, pageLoader: { link in "<html>\(link)</html>" })
        await harness.store.parse(url: "Try this one: https://example.test/blocked")
        #expect(harness.store.step == .review)
        #expect(harness.transport.requests.count == 2)
        let retry = try #require(harness.transport.requests.last)
        #expect(retry.jsonBody?["url"] == "https://example.test/blocked")
        #expect(retry.jsonBody?["html"] == "<html>https://example.test/blocked</html>")
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
        // Ingredients are added one at a time in their own sheet, so none start blank.
        #expect(harness.store.draft.ingredients.isEmpty)
        #expect(harness.store.draft.steps == [""])
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

    @Test func nutritionAndNotesAreSavedWithTheRecipe() async throws {
        let harness = try await makeHarness()
        harness.store.startBlank()
        harness.store.draft.name = "No Bake Cookies"
        harness.store.draft.ingredients.append(
            RecipeDraftIngredient(
                fields: IngredientFields(quantity: "2", unit: "tbsp", name: "Butter", note: "softened")))
        harness.store.draft.nutrition[.calories] = "180"
        harness.store.draft.cookTime = 10
        await harness.store.save()
        let sent = try #require(harness.transport.requests.last?.httpBody)
        let body = try #require(try JSONSerialization.jsonObject(with: sent) as? [String: Any])
        let lines = try #require(body["ingredients"] as? [[String: Any]])
        #expect(lines.first?["note"] as? String == "softened")
        let nutrients = try #require(body["nutritionPerServing"] as? [[String: Any]])
        #expect(nutrients.first?["name"] as? String == "Calories")
        #expect(nutrients.first?["amount"] as? Double == 180)
        #expect(body["totalMinutes"] as? Int == 10)
        #expect(body["nutrition"] == nil, "the typed text isn't sent")
    }

    @Test func aDraftWithoutNutritionSendsNone() async throws {
        let harness = try await makeHarness()
        harness.store.startBlank()
        harness.store.draft.name = "Toast"
        await harness.store.save()
        let sent = try #require(harness.transport.requests.last?.httpBody)
        let body = try #require(try JSONSerialization.jsonObject(with: sent) as? [String: Any])
        #expect(body["nutritionPerServing"] == nil)
    }

    @Test func aPhotoIsSentAndItsLinkSavedWithTheRecipe() async throws {
        let harness = try await makeHarness()
        harness.store.startBlank()
        harness.store.draft.name = "Grandma's Chili"
        harness.store.draft.imageURL = try await harness.store.uploadPhoto(Data([0xFF, 0xD8, 0xFF]))
        #expect(harness.store.draft.imageURL == "https://api.test/api/v1/recipe-photos/abc")
        await harness.store.save()
        let sent = try #require(harness.transport.requests.last?.httpBody)
        let body = try #require(try JSONSerialization.jsonObject(with: sent) as? [String: Any])
        #expect(body["imageUrl"] as? String == "https://api.test/api/v1/recipe-photos/abc")
    }
}
