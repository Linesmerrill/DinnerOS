import Foundation
import Testing

@testable import DinnerOS

/// Recipe detail fields the redesigned screen reads, customizations, pairings, and entry customizations.
struct RecipeExtrasTests {
    private func decode<Value: Decodable>(_ type: Value.Type, _ json: String) throws -> Value {
        try JSONCoding.makeDecoder().decode(Value.self, from: Data(json.utf8))
    }

    // MARK: Recipe detail

    @Test func detailWithoutTheNewFieldsUsesDefaults() throws {
        let recipe = try JSONCoding.makeDecoder().decode(Recipe.self, from: RecipeFixtures.detail())

        #expect(recipe.sourceURL == nil)
        #expect(recipe.nutrition.isEmpty)
        #expect(recipe.nutritionValues == recipe.nutritionPerServing)
        #expect(recipe.difficulty == 1)
        #expect(recipe.allergens == ["Milk", "Wheat"])
        #expect(recipe.ingredients.allSatisfy { $0.imageURL == nil && $0.allergens.isEmpty })
        #expect(recipe.steps.first?.imageURL != nil)
    }

    @Test func detailReadsIngredientImagesAllergensNutritionAndSource() throws {
        var json = String(decoding: RecipeFixtures.detail(), as: UTF8.self)
        json = json.replacingOccurrences(
            of: #""name": "Cheddar", "category": "dairy-eggs", "pantryStaple": false,"#,
            with:
                #""name": "Cheddar", "category": "dairy-eggs", "pantryStaple": false, "imageUrl": "https://img.example.test/w_1200/cheddar.png", "allergens": ["Milk", 7],"#
        )
        json = json.replacingOccurrences(
            of: #""sourceRecipeId": "src-1","#,
            with:
                #""sourceRecipeId": "src-1", "sourceUrl": "https://recipes.example.test/tacos", "nutrition": [{"name": "Calories", "amount": 700, "unit": "kcal"}, {"name": "Fat"}],"#
        )

        let recipe = try decode(Recipe.self, json)

        #expect(recipe.sourceURL?.absoluteString == "https://recipes.example.test/tacos")
        #expect(recipe.nutrition.map(\.name) == ["Calories"])
        #expect(recipe.calories == 700)
        let cheddar = try #require(recipe.ingredients.first)
        #expect(cheddar.imageURL?.lastPathComponent == "cheddar.png")
        #expect(cheddar.allergens == ["Milk"])
        let line = try #require(recipe.ingredientLines(servings: 2).first)
        #expect(line.imageURL == cheddar.imageURL)
        #expect(line.allergens == ["Milk"])
    }

    @Test func detailWithMissingDisplayDetailsStillDecodes() throws {
        var json = String(decoding: RecipeFixtures.detail(), as: UTF8.self)
        for field in [
            #""utensils": ["Skillet"], "allergens": ["Milk", "Wheat"],"#, #""difficulty": 1,"#,
        ] {
            json = json.replacingOccurrences(of: field, with: "")
        }
        json = json.replacingOccurrences(of: #""nutritionPerServing": ["#, with: #""unusedNutrition": ["#)

        let recipe = try decode(Recipe.self, json)

        #expect(recipe.allergens.isEmpty)
        #expect(recipe.utensils.isEmpty)
        #expect(recipe.difficulty == nil)
        #expect(recipe.nutritionValues.isEmpty)
        #expect(recipe.calories == nil)
    }

    // MARK: Customizations

    @Test func decodesCustomizations() throws {
        let customizations = try decode(
            RecipeCustomizations.self,
            #"""
            {"groups":[
              {"ingredientKey":"i-pork","ingredientName":"Ground Pork","amountText":"10 ounce",
               "imageUrl":"https://img.example.test/w_1200/pork.png",
               "choices":[
                 {"id":"original","label":"Ground Pork","ingredientName":"Ground Pork","amountText":"10 ounce","kind":"original","badge":null},
                 {"id":"double","label":"2x Ground Pork","ingredientName":"Ground Pork","amountText":"20 ounce","kind":"double","badge":"Double portion"},
                 {"id":"beef","label":"Ground Beef","ingredientName":"Ground Beef","amountText":"10 ounce","kind":"swap"},
                 {"id":"tofu","label":"Tofu","kind":"future_kind","badge":""},
                 {"label":"No ID"}
               ]},
              {"ingredientKey":"i-empty","ingredientName":"Nothing","amountText":"","choices":[]}
            ]}
            """#)

        #expect(customizations.groups.map(\.id) == ["i-pork"])
        let group = try #require(customizations.groups.first)
        #expect(group.imageURL != nil)
        #expect(group.choices.map(\.id) == ["original", "double", "beef", "tofu"])
        #expect(group.originalChoice?.id == "original")
        #expect(group.choices[1].badge == "Double portion")
        #expect(group.choices[1].kind == .double)
        #expect(group.choices[3].kind == CustomizationKind(rawValue: "future_kind"))
        #expect(group.choices[3].badge == nil)
        #expect(group.choices[3].ingredientName == "Tofu")
    }

    @Test func customizationsNotFoundMeansNone() async throws {
        let transport = StubTransport { request in
            if request.url?.path().hasSuffix("/customizations") == true
                || request.url?.path().hasSuffix("/pairings") == true
            {
                return (404, Fixtures.errorJSON(code: "not_found"))
            }
            return (200, RecipeFixtures.page([], nextCursor: nil))
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let library = RecipeLibrary(session: session, api: RecipesAPI(client: client))
        await library.activate(householdID: "household-1")

        #expect(try await library.customizations(recipeID: "recipe-1").isEmpty)
        #expect(try await library.pairings(recipeID: "recipe-1", week: ISOWeek("2026-W38")).isEmpty)
        let pairingsRequest = try #require(transport.requests.last)
        #expect(pairingsRequest.url?.path() == "/api/v1/households/household-1/recipes/recipe-1/pairings")
        #expect(pairingsRequest.url?.query() == "week=2026-W38")
    }

    @Test func entryCustomizationsDecodeLeniently() throws {
        let entry = try decode(
            PlanEntry.self,
            #"""
            {"id":"e1","recipe":{"id":"recipe-1","name":"Tacos"},"day":"tue","date":"2026-09-15","servings":2,"note":"",
             "addedBy":"u1","addedAt":"2026-09-14T18:30:00Z",
             "customizations":[{"ingredientKey":"i-pork","choiceId":"beef","label":"Ground Beef"},{"choiceId":"x"}]}
            """#)
        #expect(
            entry.customizations == [
                PlanEntryCustomization(ingredientKey: "i-pork", choiceID: "beef", label: "Ground Beef")
            ])

        let plain = try decode(
            PlanEntry.self,
            #"""
            {"id":"e2","recipe":{"id":"recipe-1","name":"Tacos"},"day":null,"date":null,"servings":2,"note":"",
             "addedBy":"u1","addedAt":"2026-09-14T18:30:00Z","customizations":"unexpected"}
            """#)
        #expect(plain.customizations.isEmpty)
    }

    @Test func customizationRequestShape() async throws {
        let transport = StubTransport { _ in (200, Data(PlanFixtures.plan().utf8)) }
        let api = PlansAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))

        _ = try await api.setCustomization(
            householdID: "household-1", week: try #require(ISOWeek("2026-W38")), entryID: "entry-1",
            request: PlanCustomizationRequest(selections: [.init(ingredientKey: "i-pork", choiceID: "beef")]),
            accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path() == "/api/v1/households/household-1/plans/2026-W38/entries/entry-1/customization")
        let body = try #require(request.httpBody)
        let object = try #require(try JSONSerialization.jsonObject(with: body) as? [String: [[String: String]]])
        #expect(object["selections"] == [["ingredientKey": "i-pork", "choiceId": "beef"]])
    }

    // MARK: Pairings

    @Test func decodesPairings() throws {
        let pairings = try decode(
            RecipePairings.self,
            #"""
            {"items":[
              {"target":{"kind":"recipe","recipe":\#(MenuFixtures.summary(id: "addon-1", name: "Sample Garlic Bread"))},
               "source":"rule","confidence":0.9,"reason":"Goes with pasta","inPlan":true,"ruleId":"rule-1"},
              {"target":{"kind":"grocery_item","groceryItem":{"name":"Crackers","quantity":2,"unit":"box"}},
               "source":"learned","confidence":0.4,"inPlan":false},
              {"target":{"kind":"video","url":"https://example.test"},"source":"rule","inPlan":false}
            ]}
            """#)

        #expect(pairings.items.map(\.id) == ["recipe:addon-1", "item:Crackers"])
        let bread = try #require(pairings.items.first)
        #expect(bread.name == "Sample Garlic Bread")
        #expect(bread.inPlan)
        #expect(bread.ruleID == "rule-1")
        #expect(bread.reason == "Goes with pasta")
        guard case .groceryItem(let item) = try #require(pairings.items.last).target else {
            Issue.record("expected a grocery item")
            return
        }
        #expect(item == PairingGroceryItem(name: "Crackers", quantity: "2", unit: "box"))
    }

    // MARK: Plan store

    @Test func failedCustomizationPutsThePreviousChoicesBack() async throws {
        let server = FakePlanServer()
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let plans = PlanStore(session: session, api: PlansAPI(client: client), checks: InMemoryGroceryChecks())
        await plans.activate(householdID: "household-1", timeZone: .gmt)
        let entry = try await plans.addEntry(NewPlanEntry(recipeID: "recipe-1", servings: 2))
        server.failNext()

        await #expect(throws: APIError.self) {
            try await plans.setCustomization(
                entryID: entry.id, selections: [.init(ingredientKey: "i-pork", choiceID: "beef")])
        }

        #expect(plans.plan?.entries.first?.customizations.isEmpty == true)
    }
}
