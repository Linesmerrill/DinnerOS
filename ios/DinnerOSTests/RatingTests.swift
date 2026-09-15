import Foundation
import Synchronization
import Testing

@testable import DinnerOS

struct RatingsAPITests {
    private func makeAPI(_ transport: StubTransport) throws -> RecipesAPI {
        RecipesAPI(client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private func jsonObject(_ request: URLRequest) throws -> [String: Any] {
        let body = try #require(request.httpBody)
        return try #require(try JSONSerialization.jsonObject(with: body) as? [String: Any])
    }

    @Test func rateSendsPUTWithScoreTagsAndComment() async throws {
        let response = RecipeFixtures.rating(score: 4, tags: ["make-again", "too-spicy"], comment: "Less chili")
        let transport = StubTransport { _ in (200, Data(response.utf8)) }
        var draft = RatingDraft(score: 4, comment: "  Less chili \n")
        draft.toggle(.tooSpicy)
        draft.toggle(.makeAgain)

        let saved = try await makeAPI(transport).rate(
            householdID: "household-1", recipeID: "recipe-1", request: draft.request, accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path() == "/api/v1/households/household-1/recipes/recipe-1/rating")
        #expect(request.bearerToken == "token-1")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        let body = try jsonObject(request)
        #expect(body["score"] as? Int == 4)
        #expect(body["comment"] as? String == "Less chili")
        #expect(body["tags"] as? [String] == ["make-again", "too-spicy"])
        #expect(Set(body.keys) == ["score", "comment", "tags"])

        #expect(saved.recipeID == "recipe-1")
        #expect(saved.userID == Fixtures.user.id)
        #expect(saved.score == 4)
        #expect(saved.tags == [.makeAgain, .tooSpicy])
        #expect(saved.comment == "Less chili")
    }

    @Test func rateOmitsAnEmptyComment() async throws {
        let transport = StubTransport { _ in (200, Data(RecipeFixtures.rating(score: 5).utf8)) }

        _ = try await makeAPI(transport).rate(
            householdID: "h", recipeID: "r", request: RatingDraft(score: 5, comment: "   ").request, accessToken: "t")

        let body = try jsonObject(try #require(transport.requests.first))
        #expect(Set(body.keys) == ["score", "tags"])
        #expect(body["tags"] as? [String] == [])
    }

    @Test func removeSendsDELETE() async throws {
        let transport = StubTransport { _ in (204, Data()) }

        try await makeAPI(transport).removeRating(householdID: "household-1", recipeID: "recipe-1", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "DELETE")
        #expect(request.url?.path() == "/api/v1/households/household-1/recipes/recipe-1/rating")
        #expect(request.httpBody == nil)
    }

    @Test func listDecodesMembersWithDisplayNames() async throws {
        let body = Data(
            #"""
            {"householdRating":{"average":4.5,"count":2},
             "items":[
               \#(RecipeFixtures.rating(userID: "user-2", score: 4, tags: ["future-tag"]).dropLast()),"displayName":""},
               \#(RecipeFixtures.rating(score: 5, comment: "Great").dropLast()),"displayName":"Ada Lovelace"}
             ]}
            """#.utf8)
        let transport = StubTransport { _ in (200, body) }

        let list = try await makeAPI(transport).ratings(householdID: "h", recipeID: "recipe-1", accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/h/recipes/recipe-1/ratings")
        #expect(list.householdRating == HouseholdRating(average: 4.5, count: 2))
        #expect(list.items.map(\.id) == ["user-2", Fixtures.user.id])
        #expect(list.items[0].name == "Unnamed member")
        #expect(list.items[0].rating.tags == [RatingTag(rawValue: "future-tag")])
        #expect(list.items[1].name == "Ada Lovelace")
        #expect(list.items[1].rating.comment == "Great")
    }

    @Test func summariesAndDetailsDecodeRatingFields() async throws {
        let rated = RecipeFixtures.summary(
            id: "r1", name: "Rated", householdRating: #"{"average":4.33,"count":3}"#,
            myRating: RecipeFixtures.rating(recipeID: "r1", score: 5, tags: ["kid-favorite"]))
        let unrated = RecipeFixtures.summary(id: "r2", name: "Unrated")
        let transport = StubTransport { request in
            request.url?.path().hasSuffix("/recipes") == true
                ? (200, RecipeFixtures.page([rated, unrated], nextCursor: nil))
                : (
                    200,
                    RecipeFixtures.detail(
                        householdRating: #"{"average":5,"count":1}"#, myRating: RecipeFixtures.rating(score: 5))
                )
        }
        let api = try makeAPI(transport)

        let page = try await api.listRecipes(
            householdID: "h", filters: RecipeListFilters(), cursor: nil, limit: 2, accessToken: "t")
        let recipe = try await api.recipe(householdID: "h", id: "recipe-1", accessToken: "t")

        #expect(page.items[0].householdRating == HouseholdRating(average: 4.33, count: 3))
        #expect(page.items[0].myRating?.tags == [.kidFavorite])
        #expect(page.items[1].householdRating == .unrated)
        #expect(page.items[1].myRating == nil)
        #expect(recipe.householdRating.average == 5)
        #expect(recipe.myRating?.score == 5)
    }

    @Test func missingHouseholdRatingIsADecodingError() async throws {
        let transport = StubTransport { _ in
            (200, Data(#"{"items":[{"id":"r","name":"N","timesOrdered":0,"isAddon":false,"tags":[]}]}"#.utf8))
        }

        await #expect(throws: APIError.decoding(type: "RecipeListPage")) {
            try await makeAPI(transport).listRecipes(
                householdID: "h", filters: RecipeListFilters(), cursor: nil, limit: 1, accessToken: "t")
        }
    }

    @Test func validationFailureShowsTheServerMessage() async throws {
        let transport = StubTransport { _ in
            (
                400,
                Fixtures.errorJSON(
                    code: "validation_failed", message: "tags make-again and never-again can't be used together")
            )
        }

        do {
            _ = try await makeAPI(transport).rate(
                householdID: "h", recipeID: "r", request: RatingDraft(score: 3).request, accessToken: "t")
            Issue.record("expected validation_failed")
        } catch let error as APIError {
            #expect(HouseholdStore.message(for: error).contains("never-again"))
        }
    }
}

struct RatingDraftTests {
    @Test func makeAgainAndNeverAgainAreMutuallyExclusive() {
        var draft = RatingDraft(score: 4)

        draft.toggle(.makeAgain)
        draft.toggle(.kidFavorite)
        #expect(draft.tags == [.makeAgain, .kidFavorite])

        draft.toggle(.neverAgain)
        #expect(draft.tags == [.neverAgain, .kidFavorite])
        #expect(!draft.contains(.makeAgain))

        draft.toggle(.makeAgain)
        #expect(draft.tags == [.makeAgain, .kidFavorite])

        draft.toggle(.makeAgain)
        #expect(draft.tags == [.kidFavorite])
    }

    @Test func onlyTheTwoAgainTagsExcludeEachOther() {
        #expect(RatingTag.makeAgain.excluded == .neverAgain)
        #expect(RatingTag.neverAgain.excluded == .makeAgain)
        for tag in RatingTag.known where tag != .makeAgain && tag != .neverAgain {
            #expect(tag.excluded == nil)
        }
        #expect(
            RatingTag.known.map(\.rawValue) == [
                "make-again", "never-again", "kid-favorite", "kids-disliked", "too-spicy", "too-bland", "too-much-work",
                "great-leftovers",
            ])
    }

    @Test func tagsStayInCanonicalOrderWithoutDuplicates() {
        let draft = RatingDraft(
            score: 3, tags: [.greatLeftovers, .tooSpicy, RatingTag(rawValue: "future-tag"), .tooSpicy, .makeAgain])

        #expect(draft.tags == [.makeAgain, .tooSpicy, .greatLeftovers, RatingTag(rawValue: "future-tag")])
    }

    @Test func initialConflictingTagsKeepTheLastOne() {
        #expect(RatingDraft(score: 3, tags: [.makeAgain, .neverAgain]).tags == [.neverAgain])
    }

    @Test func saveNeedsAScoreAndACommentWithinTheLimit() {
        var draft = RatingDraft()
        #expect(!draft.canSave)

        draft.score = 5
        #expect(draft.canSave)

        draft.comment = "\n  " + String(repeating: "a", count: RatingLimits.maxCommentLength) + "  "
        #expect(draft.commentLength == RatingLimits.maxCommentLength)
        #expect(draft.canSave)

        // Counted in Unicode scalars, like the API's rune count.
        draft.comment = String(repeating: "a", count: RatingLimits.maxCommentLength - 1) + "👍🏽"
        #expect(draft.commentLength == RatingLimits.maxCommentLength + 1)
        #expect(!draft.canSave)
    }

    @Test func differsComparesWithTheSavedRating() throws {
        let data = Data(RecipeFixtures.rating(score: 4, tags: ["too-bland"], comment: "Salt").utf8)
        let saved = try JSONCoding.makeDecoder().decode(RecipeRating.self, from: data)
        var draft = RatingDraft(rating: saved)

        #expect(draft == RatingDraft(score: 4, comment: "Salt", tags: [.tooBland]))
        #expect(!draft.differs(from: saved))
        draft.comment = "Salt "
        #expect(!draft.differs(from: saved))
        draft.toggle(.makeAgain)
        #expect(draft.differs(from: saved))
        #expect(RatingDraft(score: 1).differs(from: nil))
    }

    @Test func summaryText() {
        let locale = Locale(identifier: "en_US")
        #expect(RatingFormat.summary(HouseholdRating(average: 4.5, count: 2), locale: locale) == "4.5 ★ from 2")
        #expect(RatingFormat.summary(HouseholdRating(average: 4.33, count: 3), locale: locale) == "4.3 ★ from 3")
        #expect(RatingFormat.summary(.unrated, locale: locale) == nil)
    }
}

/// An in-memory stand-in for one recipe's list, detail, and rating endpoints. Another
/// member has rated the recipe 3.
nonisolated final class FakeRatingServer: Sendable {
    struct State: Sendable {
        var mine: (score: Int, tags: [String], comment: String)?
        var failDetail = false
        var log: [String] = []
    }

    private let state = Mutex(State())

    var log: [String] { state.withLock { $0.log } }

    func update(_ change: (inout State) -> Void) {
        state.withLock { change(&$0) }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        let method = request.httpMethod ?? "GET"
        let path = request.url?.path() ?? ""
        return state.withLock { state in
            state.log.append("\(method) \(path)")
            let base = "/api/v1/households/household-1/recipes"
            switch (method, path) {
            case ("GET", base):
                return (200, RecipeFixtures.page([summary(state)], nextCursor: nil))
            case ("GET", base + "/recipe-1"):
                if state.failDetail {
                    return (500, Fixtures.errorJSON(code: "internal"))
                }
                return (200, RecipeFixtures.detail(householdRating: aggregate(state), myRating: myRating(state)))
            case ("PUT", base + "/recipe-1/rating"):
                guard
                    let body = request.httpBody,
                    let object = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
                    let score = object["score"] as? Int
                else { return (400, Fixtures.errorJSON(code: "invalid_request")) }
                state.mine = (score, object["tags"] as? [String] ?? [], object["comment"] as? String ?? "")
                return (200, Data(myRating(state).utf8))
            case ("DELETE", base + "/recipe-1/rating"):
                state.mine = nil
                return (204, Data())
            default:
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
        }
    }

    private func summary(_ state: State) -> String {
        RecipeFixtures.summary(
            id: "recipe-1", name: "Test Kitchen Tacos", householdRating: aggregate(state), myRating: myRating(state))
    }

    private func aggregate(_ state: State) -> String {
        guard let mine = state.mine else { return #"{"average":3,"count":1}"# }
        return #"{"average":\#(Double(mine.score + 3) / 2),"count":2}"#
    }

    private func myRating(_ state: State) -> String {
        guard let mine = state.mine else { return "null" }
        return RecipeFixtures.rating(score: mine.score, tags: mine.tags, comment: mine.comment)
    }
}

struct RecipeLibraryRatingTests {
    private func makeLibrary(_ server: FakeRatingServer) async throws -> RecipeLibrary {
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let library = RecipeLibrary(session: session, api: RecipesAPI(client: client))
        await library.activate(householdID: "household-1")
        return library
    }

    @Test func savingRefreshesTheCachedRecipeAndTheListRow() async throws {
        let server = FakeRatingServer()
        let library = try await makeLibrary(server)
        _ = try await library.recipe(id: "recipe-1")
        #expect(library.items.first?.householdRating == HouseholdRating(average: 3, count: 1))

        var draft = RatingDraft(score: 5)
        draft.toggle(.makeAgain)
        let saved = try await library.saveRating(draft, recipeID: "recipe-1")

        #expect(saved.score == 5)
        #expect(
            server.log.suffix(2) == [
                "PUT /api/v1/households/household-1/recipes/recipe-1/rating",
                "GET /api/v1/households/household-1/recipes/recipe-1",
            ])
        let cached = try #require(library.cachedRecipe(id: "recipe-1"))
        #expect(cached.householdRating == HouseholdRating(average: 4, count: 2))
        #expect(cached.myRating?.tags == [.makeAgain])
        #expect(library.items.first?.householdRating == HouseholdRating(average: 4, count: 2))
        #expect(library.items.first?.myRating?.score == 5)
    }

    @Test func aFailedReloadStillShowsTheSavedRating() async throws {
        let server = FakeRatingServer()
        let library = try await makeLibrary(server)
        _ = try await library.recipe(id: "recipe-1")
        server.update { $0.failDetail = true }

        try await library.saveRating(RatingDraft(score: 2), recipeID: "recipe-1")

        #expect(library.cachedRecipe(id: "recipe-1")?.myRating?.score == 2)
        #expect(library.items.first?.myRating?.score == 2)
        // The average waits for the next successful load.
        #expect(library.items.first?.householdRating == HouseholdRating(average: 3, count: 1))
    }

    @Test func removingRefreshesTheRecipe() async throws {
        let server = FakeRatingServer()
        server.update { $0.mine = (4, [], "") }
        let library = try await makeLibrary(server)
        #expect(library.items.first?.myRating?.score == 4)

        try await library.removeRating(recipeID: "recipe-1")

        #expect(server.log.contains("DELETE /api/v1/households/household-1/recipes/recipe-1/rating"))
        #expect(library.cachedRecipe(id: "recipe-1")?.myRating == nil)
        #expect(library.items.first?.myRating == nil)
        #expect(library.items.first?.householdRating == HouseholdRating(average: 3, count: 1))
    }

    @Test func aRejectedSaveChangesNothing() async throws {
        let server = FakeRatingServer()
        let library = try await makeLibrary(server)
        let listRequests = server.log.count

        await #expect(throws: APIError.self) {
            // Not a recipe the fake knows.
            try await library.saveRating(RatingDraft(score: 3), recipeID: "recipe-missing")
        }

        #expect(server.log.count == listRequests + 1)
        #expect(library.items.first?.myRating == nil)
    }
}
