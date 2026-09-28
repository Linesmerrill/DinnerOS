import Foundation
import Testing

@testable import DinnerOS

/// Synthetic backlog shaped like the API's: two recipes whose box looked different (one with
/// two flagged deliveries), one respelling, and a recipe with no steps. No real recipes.
private nonisolated enum ResolveFixtures {
    struct Row {
        let id: String
        let recipeID: String?
        let recipeName: String
        let field: String
        let value: String
    }

    static let rows: [Row] = [
        Row(
            id: "rv-1", recipeID: "recipe-1", recipeName: "Turkey Fajita Zesty Rice Bowls", field: "variant",
            value: "turkey fajita bowls"),
        Row(
            id: "rv-2", recipeID: "recipe-2", recipeName: "Turkey Black Bean Chili", field: "variant",
            value: "turkey black bean chili with pork"),
        Row(
            id: "rv-3", recipeID: "recipe-2", recipeName: "Turkey Black Bean Chili", field: "variant",
            value: "pork bean chili"),
        Row(
            id: "rv-4", recipeID: "recipe-3", recipeName: "Honey Butter Corn Bread", field: "variant",
            value: "honey butter cornbread"),
        Row(id: "rv-5", recipeID: "recipe-4", recipeName: "Sheet Pan Test Bake", field: "steps", value: ""),
    ]

    static func json(_ rows: [Row]) -> Data {
        let items = rows.map { row in
            var fields = [
                #""id":"\#(row.id)""#, #""recipeName":"\#(row.recipeName)""#, #""source":"hellofresh""#,
                #""sourceRecipeId":"src-\#(row.id)""#, #""field":"\#(row.field)""#, #""reason":"flagged""#,
                #""status":"open""#, #""createdAt":"2026-08-01T10:00:00Z""#,
                #""recipeImageUrl":"https://img.example.com/\#(row.id).jpg""#,
            ]
            if let recipeID = row.recipeID { fields.append(#""recipeId":"\#(recipeID)""#) }
            if !row.value.isEmpty { fields.append(#""value":"\#(row.value)""#) }
            return "{" + fields.joined(separator: ",") + "}"
        }
        return Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8)
    }

    static let resolvePath = "/api/v1/households/household-1/recipes/import-reviews/resolve"

    /// The decoded body of a resolve request.
    struct Body: Decodable, Equatable {
        let ids: [String]
        let resolution: String
        let recipeId: String?
    }

    static func body(_ request: URLRequest) -> Body? {
        request.httpBody.flatMap { try? JSONDecoder().decode(Body.self, from: $0) }
    }
}

/// Resolving review items: the detail screen's choices, the bulk action, and the one number
/// every badge shows.
struct ImportReviewResolveTests {
    /// A store loaded with `rows`, whose resolve calls answer `resolveStatus`.
    private func loadedStore(
        rows: [ResolveFixtures.Row] = ResolveFixtures.rows, resolveStatus: Int = 200
    ) async throws -> (ImportReviewStore, StubTransport) {
        let transport = StubTransport { request in
            if request.httpMethod == "POST" {
                let count = ResolveFixtures.body(request)?.ids.count ?? 0
                return resolveStatus == 200
                    ? (200, Data(#"{"resolved":\#(count)}"#.utf8))
                    : (resolveStatus, Fixtures.errorJSON(code: "internal"))
            }
            return (200, ResolveFixtures.json(rows))
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let store = ImportReviewStore(session: session, api: RecipesAPI(client: client))
        store.activate(householdID: "household-1")
        await store.load()
        return (store, transport)
    }

    // MARK: - Counts

    /// The report: a badge said 3 and the screen listed 90. Every badge is `openCount`, which
    /// is the sum of the screen's three tiles and the number of rows it lists.
    @Test func theBadgeCountsExactlyTheRowsTheScreenLists() async throws {
        let (store, _) = try await loadedStore()
        let digest = store.digest

        #expect(digest.differences.count == 2)
        #expect(digest.spellingOnly.count == 1)
        #expect(digest.otherItems.count == 1)
        #expect(digest.openCount == digest.differences.count + digest.spellingOnly.count + digest.otherItems.count)
        #expect(digest.openCount == 4)
        // Five items, four rows: the chili's two deliveries are one row.
        #expect(store.items.count == 5)
    }

    @Test func itemsDecodeTheirIdAndPhoto() async throws {
        let (store, _) = try await loadedStore()
        let first = try #require(store.items.first)
        #expect(first.id == "rv-1")
        #expect(first.recipeImageURL?.absoluteString == "https://img.example.com/rv-1.jpg")
        #expect(store.digest.differences.first?.imageURL?.absoluteString == "https://img.example.com/rv-1.jpg")
    }

    // MARK: - Detail actions

    @Test func aProteinSwapOffersSameDifferentOpenAndDismiss() async throws {
        let (store, _) = try await loadedStore()
        let chili = try #require(store.digest.differences.first { $0.recipeID == "recipe-2" })

        #expect(
            ImportReviewAction.actions(for: chili) == [
                .sameRecipe,
                .addAsNewRecipe(deliveredName: "turkey black bean chili with pork"),
                .addAsNewRecipe(deliveredName: "pork bean chili"),
                .openRecipe, .dismiss,
            ])
        #expect(ImportReviewAction.sameRecipe.resolution == .sameRecipe)
        #expect(ImportReviewAction.addAsNewRecipe(deliveredName: "x").resolution == .differentRecipe)
        #expect(ImportReviewAction.dismiss.resolution == .dismissed)
        #expect(ImportReviewAction.openRecipe.resolution == nil)
    }

    @Test func aRespellingIsNeverOfferedAsANewRecipe() async throws {
        let (store, _) = try await loadedStore()
        let cornBread = try #require(store.digest.spellingOnly.first)
        #expect(ImportReviewAction.actions(for: cornBread) == [.sameRecipe, .openRecipe, .dismiss])
    }

    @Test func aGapOffersOpenAndDismissAndAGoneRecipeOnlyDismiss() async throws {
        let (store, _) = try await loadedStore()
        let steps = try #require(store.digest.otherItems.first)
        #expect(ImportReviewAction.actions(for: steps) == [.openRecipe, .dismiss])

        let orphan = ImportReview(
            recipeID: nil, recipeName: "Gone", source: "hellofresh", sourceRecipeID: "g", field: "steps",
            reason: "none", createdAt: .now)
        #expect(ImportReviewAction.actions(for: orphan) == [.dismiss])
    }

    @Test func sameRecipeSendsEveryDeliveryOfTheGroupAndTakesTheRowAway() async throws {
        let (store, transport) = try await loadedStore()
        let chili = try #require(store.digest.differences.first { $0.recipeID == "recipe-2" })

        try await store.resolve(chili.items, as: .sameRecipe)

        let request = try #require(transport.requests(to: ResolveFixtures.resolvePath).first)
        #expect(request.httpMethod == "POST")
        #expect(request.bearerToken == "access-1")
        #expect(
            ResolveFixtures.body(request)
                == ResolveFixtures.Body(ids: ["rv-2", "rv-3"], resolution: "same_recipe", recipeId: nil))
        #expect(!store.items.contains { $0.recipeID == "recipe-2" })
        #expect(store.digest.openCount == 3)
        #expect(!store.isResolving)
    }

    @Test func aDifferentRecipeLinksTheRecipeTheMemberAdded() async throws {
        let (store, transport) = try await loadedStore()
        let bowls = try #require(store.digest.differences.first { $0.recipeID == "recipe-1" })

        try await store.resolve(bowls.items, as: .differentRecipe, linkedRecipeID: "recipe-new")

        let body = try #require(transport.requests(to: ResolveFixtures.resolvePath).first.flatMap(ResolveFixtures.body))
        #expect(body == ResolveFixtures.Body(ids: ["rv-1"], resolution: "different_recipe", recipeId: "recipe-new"))
        #expect(store.digest.differences.map(\.recipeID) == ["recipe-2"])
    }

    @Test func dismissingAGapTakesOnlyThatItemAway() async throws {
        let (store, _) = try await loadedStore()
        let steps = try #require(store.digest.otherItems.first)

        try await store.resolve([steps], as: .dismissed)

        #expect(store.digest.otherItems.isEmpty)
        #expect(store.digest.openCount == 3)
    }

    @Test func aFailedResolveKeepsTheRowAndThrows() async throws {
        let (store, _) = try await loadedStore(resolveStatus: 500)
        let bowls = try #require(store.digest.differences.first)

        await #expect(throws: APIError.self) {
            try await store.resolve(bowls.items, as: .sameRecipe)
        }
        #expect(store.items.count == 5)
        #expect(store.digest.openCount == 4)
        #expect(!store.isResolving)
    }

    // MARK: - Bulk

    @Test func markAllAsSameRecipeClearsEveryBoxDifferenceInOneCall() async throws {
        let (store, transport) = try await loadedStore()
        let all = store.digest.differences.flatMap(\.items)

        try await store.resolve(all, as: .sameRecipe)

        let requests = transport.requests(to: ResolveFixtures.resolvePath)
        #expect(requests.count == 1)
        #expect(requests.first.flatMap(ResolveFixtures.body)?.ids == ["rv-1", "rv-2", "rv-3"])
        #expect(store.digest.differences.isEmpty)
        #expect(store.digest.spellingOnly.count == 1)
        #expect(store.digest.otherItems.count == 1)
        #expect(store.digest.openCount == 2)
    }

    /// "Same recipe" only means something for a variant item, and the server leaves anything
    /// else open, so the store doesn't send those or take them off the list.
    @Test func sameRecipeNeverTouchesAGap() async throws {
        let (store, transport) = try await loadedStore()

        try await store.resolve(store.items, as: .sameRecipe)

        #expect(
            transport.requests(to: ResolveFixtures.resolvePath).first.flatMap(ResolveFixtures.body)?.ids.contains(
                "rv-5") == false)
        #expect(store.items.map(\.id) == ["rv-5"])
        #expect(store.digest.openCount == 1)
    }

    @Test func aBacklogLargerThanOnePageGoesOutInSeveralCalls() async throws {
        let many = (0..<(RecipesAPI.importReviewLimit + 20)).map { index in
            ResolveFixtures.Row(
                id: "bulk-\(index)", recipeID: "recipe-\(index)", recipeName: "Chicken Dish \(index)",
                field: "variant", value: "pork dish \(index)")
        }
        let (store, transport) = try await loadedStore(rows: many)

        try await store.resolve(store.items, as: .sameRecipe)

        let sizes = transport.requests(to: ResolveFixtures.resolvePath).compactMap {
            ResolveFixtures.body($0)?.ids.count
        }
        #expect(sizes == [RecipesAPI.importReviewLimit, 20])
        #expect(store.items.isEmpty)
        #expect(store.digest.openCount == 0)
    }

    // MARK: - Copy

    @Test func theDetailExplainsAProteinSwapInPlainWords() async throws {
        let (store, _) = try await loadedStore()
        let bowls = try #require(store.digest.differences.first { $0.recipeID == "recipe-1" })
        let lines = ImportReviewCopy.explanation(for: bowls)

        #expect(lines.contains("Your library has this as Turkey Fajita Zesty Rice Bowls."))
        #expect(lines.contains("At least one box said turkey fajita bowls."))
        let steps = try #require(store.digest.otherItems.first)
        #expect(ImportReviewCopy.explanation(for: steps).first == "HelloFresh had no instructions for this recipe.")
        for line in lines + ImportReviewCopy.explanation(for: steps) {
            #expect(!line.contains("—"), "\(line)")
            for sentence in line.split(separator: ".") where sentence.split(separator: " ").count > 12 {
                Issue.record("a long sentence: \(sentence)")
            }
        }
    }
}
