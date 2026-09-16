import Foundation
import Testing

@testable import DinnerOS

/// Synthetic items shaped like the API's. No real recipes or orders.
///
/// `nonisolated` so the stub transport's async closures can read them, as `RecipeFixtures` is.
private nonisolated enum ImportReviewFixtures {
    static func item(
        recipeID: String? = "recipe-1", recipeName: String, field: String = "variant", value: String = "",
        reason: String = "delivered variant differed", sourceRecipeID: String = "src-1", day: Int = 1
    ) -> ImportReview {
        ImportReview(
            recipeID: recipeID, recipeName: recipeName, source: "hellofresh", sourceRecipeID: sourceRecipeID,
            field: field, value: value, reason: reason,
            createdAt: Date(timeIntervalSince1970: TimeInterval(day) * 86_400))
    }

    static func listJSON(_ items: [String]) -> Data {
        Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8)
    }

    static let fullItemJSON = #"""
        {"recipeId":"recipe-1","recipeName":"Chicken Sausage Cavatappi Bolognese","source":"hellofresh",
         "sourceRecipeId":"src-1","field":"variant","value":"Pork Sausage Cavatappi Bolognese",
         "reason":"delivered menu variant differed","status":"open","createdAt":"2026-08-01T10:00:00.123456789Z"}
        """#

    /// `recipeId` and `value` are `omitempty` on the server.
    static let minimalItemJSON = #"""
        {"recipeName":"Sheet Pan Test Bake","source":"hellofresh","sourceRecipeId":"src-9","field":"steps",
         "reason":"the page had no instructions","status":"open","createdAt":"2026-08-02T10:00:00Z"}
        """#
}

/// The import review backlog: reading it, sorting it, and the one heuristic the screen uses.
struct ImportReviewTests {
    private func makeAPI(_ transport: StubTransport) throws -> RecipesAPI {
        RecipesAPI(client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private func makeStore(_ transport: StubTransport) async throws -> ImportReviewStore {
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        return ImportReviewStore(session: session, api: RecipesAPI(client: client))
    }

    // MARK: - API

    @Test func importReviewsRequestsOpenItemsForTheHousehold() async throws {
        let transport = StubTransport { _ in (200, ImportReviewFixtures.listJSON([ImportReviewFixtures.fullItemJSON])) }

        _ = try await makeAPI(transport).importReviews(householdID: "household-1", accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/recipes/import-reviews")
        #expect(request.url?.query(percentEncoded: true) == "status=open&limit=500")
        #expect(request.bearerToken == "token-1")
    }

    @Test func importReviewsDecodesEveryFieldAndTheOmittedOnes() async throws {
        let transport = StubTransport { _ in
            (
                200,
                ImportReviewFixtures.listJSON([ImportReviewFixtures.fullItemJSON, ImportReviewFixtures.minimalItemJSON])
            )
        }

        let items = try await makeAPI(transport).importReviews(householdID: "h", accessToken: "t")

        #expect(items.count == 2)
        let variant = try #require(items.first)
        #expect(variant.recipeID == "recipe-1")
        #expect(variant.recipeName == "Chicken Sausage Cavatappi Bolognese")
        #expect(variant.value == "Pork Sausage Cavatappi Bolognese")
        #expect(variant.status == ImportReview.openStatus)
        #expect(variant.kind == .variant)

        let steps = try #require(items.last)
        #expect(steps.recipeID == nil)
        #expect(steps.value.isEmpty)
        #expect(steps.kind == .steps)
    }

    @Test func ingredientUnitFieldCarriesTheIngredientName() {
        let unknownUnit = ImportReviewFixtures.item(
            recipeName: "Pickled Onion Bowls", field: "ingredients.Red Onion.unit", value: "pick")
        #expect(unknownUnit.kind == .ingredientUnit(ingredient: "Red Onion"))
        #expect(ImportReviewFixtures.item(recipeName: "X", field: "cookTime").kind == .other)
        // A variant item with no delivered name has nothing to compare, so it isn't one.
        #expect(ImportReviewFixtures.item(recipeName: "X", field: "variant", value: "").kind == .other)
    }

    // MARK: - The heuristic

    @Test func spellingOnlyIgnoresCaseSpacingAndPunctuation() {
        #expect(
            ImportVariantMatch.isSpellingOnly(
                stored: "Honey Butter Corn Bread", delivered: "honey butter cornbread"))
        #expect(ImportVariantMatch.isSpellingOnly(stored: "Bánh Mì Burgers", delivered: "Banh Mi Burgers"))
        #expect(ImportVariantMatch.isSpellingOnly(stored: "Mac 'n' Cheese", delivered: "Mac n Cheese"))
    }

    @Test func aDifferentProteinIsNeverSpellingOnly() {
        #expect(
            !ImportVariantMatch.isSpellingOnly(
                stored: "Chicken Sausage Cavatappi Bolognese", delivered: "Pork Sausage Cavatappi Bolognese"))
        #expect(!ImportVariantMatch.isSpellingOnly(stored: "Beef Tacos", delivered: "Beef Tacos with Chorizo"))
        // Nothing to compare is not a match: the conservative direction.
        #expect(!ImportVariantMatch.isSpellingOnly(stored: "", delivered: ""))
    }

    // MARK: - Digest

    @Test func digestLeadsWithRealDifferencesAndFoldsSpellingAway() {
        let digest = ImportReviewDigest(items: [
            ImportReviewFixtures.item(
                recipeName: "Chicken Sausage Cavatappi Bolognese", value: "Pork Sausage Cavatappi Bolognese",
                sourceRecipeID: "src-1", day: 1),
            ImportReviewFixtures.item(
                recipeID: "recipe-2", recipeName: "Honey Butter Corn Bread", value: "honey butter cornbread",
                sourceRecipeID: "src-2", day: 2),
            ImportReviewFixtures.item(
                recipeID: "recipe-3", recipeName: "Sheet Pan Test Bake", field: "steps", sourceRecipeID: "src-3",
                day: 3),
        ])

        #expect(digest.differences.map(\.storedName) == ["Chicken Sausage Cavatappi Bolognese"])
        #expect(digest.spellingOnly.map(\.storedName) == ["Honey Butter Corn Bread"])
        #expect(digest.otherItems.map(\.recipeName) == ["Sheet Pan Test Bake"])
        #expect(digest.variantRecipeCount == 2)
        #expect(!digest.isEmpty)
    }

    @Test func everyFlaggedDeliveryOfOneRecipeBecomesOneGroup() throws {
        let digest = ImportReviewDigest(items: [
            ImportReviewFixtures.item(
                recipeName: "Cavatappi Bolognese", value: "Pork Sausage Cavatappi", sourceRecipeID: "a", day: 1),
            ImportReviewFixtures.item(
                recipeName: "Cavatappi Bolognese", value: "Pork Sausage Cavatappi", sourceRecipeID: "b", day: 2),
            ImportReviewFixtures.item(
                recipeName: "Cavatappi Bolognese", value: "Turkey Cavatappi", sourceRecipeID: "c", day: 3),
        ])

        #expect(digest.differences.count == 1)
        let group = try #require(digest.differences.first)
        #expect(group.flaggedDeliveries == 3)
        // Distinct delivered names, oldest first, without repeating the two pork clones.
        #expect(group.deliveredNames == ["Pork Sausage Cavatappi", "Turkey Cavatappi"])
        #expect(group.recipeID == "recipe-1")
    }

    @Test func aGroupWhoseRecipeIsGoneKeepsItsNameAndCantBeOpened() throws {
        let digest = ImportReviewDigest(items: [
            ImportReviewFixtures.item(
                recipeID: nil, recipeName: "Retired Dish", value: "Retired Dish with Pork", sourceRecipeID: "z")
        ])

        let group = try #require(digest.differences.first)
        #expect(group.recipeID == nil)
        #expect(group.storedName == "Retired Dish")
    }

    @Test func oneRealDifferenceKeepsTheWholeRecipeOutOfTheSpellingPile() {
        let digest = ImportReviewDigest(items: [
            ImportReviewFixtures.item(recipeName: "Corn Bread", value: "corn bread", sourceRecipeID: "a", day: 1),
            ImportReviewFixtures.item(
                recipeName: "Corn Bread", value: "Jalapeño Corn Bread", sourceRecipeID: "b", day: 2),
        ])

        #expect(digest.differences.count == 1)
        #expect(digest.spellingOnly.isEmpty)
    }

    @Test func anEmptyBacklogIsEmpty() {
        #expect(ImportReviewDigest(items: []).isEmpty)
    }

    // MARK: - Store

    @Test func loadKeepsTheBacklogOldestFirst() async throws {
        let transport = StubTransport { _ in
            (
                200,
                ImportReviewFixtures.listJSON([ImportReviewFixtures.fullItemJSON, ImportReviewFixtures.minimalItemJSON])
            )
        }
        let store = try await makeStore(transport)

        store.activate(householdID: "household-1")
        await store.load()

        #expect(store.phase == .loaded)
        #expect(store.items.map(\.sourceRecipeID) == ["src-1", "src-9"])
        #expect(store.isAvailable)
        #expect(store.digest.differences.count == 1)
    }

    @Test func aNotFoundHidesTheScreenInsteadOfShowingAnError() async throws {
        let transport = StubTransport { _ in (404, Fixtures.errorJSON(code: "not_found")) }
        let store = try await makeStore(transport)

        store.activate(householdID: "household-1")
        await store.load()

        #expect(!store.isAvailable)
        #expect(store.phase == .loaded)
        #expect(store.items.isEmpty)
        #expect(store.refreshError == nil)
    }

    @Test func aFailedLoadSaysSoRatherThanHiding() async throws {
        let transport = StubTransport { _ in (500, Fixtures.errorJSON(code: "internal")) }
        let store = try await makeStore(transport)

        store.activate(householdID: "household-1")
        await store.load()

        #expect(store.isAvailable)
        #expect(store.items.isEmpty)
        if case .failed = store.phase {
        } else {
            Issue.record("expected a failed phase, got \(store.phase)")
        }
    }

    @Test func anotherHouseholdNeverSeesTheOneBeforeIt() async throws {
        let transport = StubTransport { _ in (200, ImportReviewFixtures.listJSON([ImportReviewFixtures.fullItemJSON])) }
        let store = try await makeStore(transport)
        store.activate(householdID: "household-1")
        await store.load()
        #expect(!store.items.isEmpty)

        store.activate(householdID: "household-2")

        #expect(store.items.isEmpty)
        #expect(store.phase == .idle)
        #expect(store.householdID == "household-2")
    }
}
