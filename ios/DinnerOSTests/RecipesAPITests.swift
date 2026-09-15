import Foundation
import Testing

@testable import DinnerOS

struct RecipesAPITests {
    private func makeAPI(_ transport: StubTransport) throws -> RecipesAPI {
        RecipesAPI(client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    @Test func listSendsEveryFilterInOrderAndEncodesStrictly() async throws {
        let transport = StubTransport { _ in (200, RecipeFixtures.page([], nextCursor: nil)) }
        let filters = RecipeListFilters(
            search: "  salt & pepper+lime  ", sort: .popular, kind: .mains, tag: "Quick", cuisine: "Tex-Mex")

        _ = try await makeAPI(transport).listRecipes(
            householdID: "household-1", filters: filters, cursor: "eyJzIjoi=", limit: 30, accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/recipes")
        #expect(request.bearerToken == "token-1")
        #expect(
            request.url?.query(percentEncoded: true)
                == "sort=popular&q=salt%20%26%20pepper%2Blime&addons=false&tag=Quick&cuisine=Tex-Mex&limit=30"
                + "&cursor=eyJzIjoi%3D")
        // Decoded the way the server's parser sees it.
        let items = URLComponents(url: try #require(request.url), resolvingAgainstBaseURL: false)?.queryItems
        #expect(items?.first { $0.name == "q" }?.value == "salt & pepper+lime")
    }

    @Test func listOmitsUnsetFilters() async throws {
        let transport = StubTransport { _ in (200, RecipeFixtures.page([], nextCursor: nil)) }

        _ = try await makeAPI(transport).listRecipes(
            householdID: "h", filters: RecipeListFilters(search: "   "), cursor: nil, limit: 50, accessToken: "t")

        #expect(transport.requests.first?.url?.query(percentEncoded: true) == "sort=recent&limit=50")
    }

    @Test(arguments: [(RecipeKind.all, nil), (.mains, "false"), (.addons, "true")] as [(RecipeKind, String?)])
    func addonsParameter(kind: RecipeKind, expected: String?) {
        let items = RecipesAPI.queryItems(filters: RecipeListFilters(kind: kind), cursor: nil, limit: 10)
        #expect(items.first { $0.name == "addons" }?.value == expected)
    }

    @Test func searchIsCutToTheAPILimit() {
        let filters = RecipeListFilters(search: String(repeating: "é", count: 150))
        let items = RecipesAPI.queryItems(filters: filters, cursor: nil, limit: 10)
        #expect(items.first { $0.name == "q" }?.value?.count == RecipeListFilters.maxSearchLength)
    }

    @Test func nonASCIISearchIsPercentEncoded() {
        #expect(APIClient.encodeQueryComponent("jalapeño crème") == "jalape%C3%B1o%20cr%C3%A8me")
    }

    @Test func decodesPageWithCursor() async throws {
        let body = RecipeFixtures.page(
            [RecipeFixtures.summary(id: "r1", name: "Taco Test", timesOrdered: 3, lastOrderedWeek: "2026-W30")],
            nextCursor: "next-1")
        let transport = StubTransport { _ in (200, body) }

        let page = try await makeAPI(transport).listRecipes(
            householdID: "h", filters: RecipeListFilters(), cursor: nil, limit: 1, accessToken: "t")

        #expect(page.nextCursor == "next-1")
        let item = try #require(page.items.first)
        #expect(item.id == "r1")
        #expect(item.headline == "with Test Sauce")
        #expect(item.imageURL == URL(string: "https://img.example.test/r1.jpg"))
        #expect(item.totalMinutes == 30)
        #expect(item.timesOrdered == 3)
        #expect(item.lastOrderedWeek == "2026-W30")
        #expect(item.tags == ["Quick"])
    }

    @Test func decodesSummaryWithOmittedFields() async throws {
        let transport = StubTransport { _ in (200, RecipeFixtures.minimalPage) }

        let page = try await makeAPI(transport).listRecipes(
            householdID: "h", filters: RecipeListFilters(), cursor: nil, limit: 1, accessToken: "t")

        let item = try #require(page.items.first)
        #expect(page.nextCursor == nil)
        #expect(item.headline == nil)
        #expect(item.imageURL == nil)
        #expect(item.totalMinutes == nil)
        #expect(item.lastOrderedWeek == nil)
        #expect(item.isAddon)
    }

    @Test func getRecipeDecodesFullRecipe() async throws {
        let transport = StubTransport { _ in (200, RecipeFixtures.detail()) }

        let recipe = try await makeAPI(transport).recipe(householdID: "household-1", id: "recipe-1", accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/recipes/recipe-1")
        #expect(transport.requests.first?.url?.query() == nil)
        #expect(recipe.householdID == "household-1")
        #expect(recipe.servings == [2, 4])
        #expect(recipe.difficulty == 1)
        #expect(recipe.nutritionPerServing.last == RecipeNutrient(name: "Protein", amount: 32.5, unit: "g"))
        #expect(recipe.allergens == ["Milk", "Wheat"])
        #expect(recipe.utensils == ["Skillet"])
        #expect(recipe.orderWeeks == ["2026-W12", "2026-W37"])

        let cheddar = try #require(recipe.ingredients.first)
        #expect(cheddar.ingredientID == "i-cheddar")
        #expect(cheddar.category == "dairy-eggs")
        #expect(
            cheddar.amounts.first
                == RecipeAmount(
                    servings: 2, quantity: "1/2", quantityValue: 0.5, unit: "oz", sourceUnit: "ounce",
                    rawText: "½ ounce Cheddar"))
        let salt = try #require(recipe.ingredients.first { $0.name == "Salt" })
        #expect(salt.pantryStaple)
        #expect(salt.amounts.first?.quantity == nil)
        #expect(salt.amounts.first?.quantityValue == nil)

        #expect(recipe.steps.map(\.index) == [1, 2])
        #expect(recipe.steps[0].imageURL == URL(string: "https://img.example.test/step-1.jpg"))
        #expect(recipe.steps[1].imageURL == nil)
        #expect(recipe.steps[1].text == "• Fill.\n• Serve.")
        #expect(recipe.createdAt.timeIntervalSince1970.rounded(.down) == recipe.updatedAt.timeIntervalSince1970)
    }

    @Test func notFoundMapsToAPIError() async throws {
        let transport = StubTransport { _ in (404, Fixtures.errorJSON(code: "not_found")) }

        await #expect(throws: APIError.self) {
            try await makeAPI(transport).recipe(householdID: "h", id: "missing", accessToken: "t")
        }
    }
}
