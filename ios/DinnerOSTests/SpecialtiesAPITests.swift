import Foundation
import Testing

@testable import DinnerOS

struct SpecialtiesAPITests {
    private func makeAPI(_ transport: StubTransport) throws -> SpecialtiesAPI {
        SpecialtiesAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private let basePath = "/api/v1/households/household-1/specialty-ingredients"

    @Test func listDecodesOptionsChoiceAndBatchStock() async throws {
        let transport = StubTransport { _ in (200, SpecialtyFixtures.list([SpecialtyFixtures.southwestJSON])) }

        let items = try await makeAPI(transport).list(householdID: "household-1", accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == basePath)
        #expect(request.url?.query() == nil)
        #expect(request.bearerToken == "token-1")

        let blend = try #require(items.first)
        #expect(blend.id == "southwest-spice-blend")
        #expect(blend.key == "southwest spice blend")
        #expect(blend.aliases == ["Southwestern Spice Blend"])
        #expect(blend.ingredientIDs == ["i-southwest"])
        #expect(blend.recipeCount == 52)
        #expect(blend.unitSizes.first?.text == "1 tbsp")
        #expect(blend.defaultOptionID == "southwest-spice-blend.batch")
        #expect(!blend.retired)

        let choice = try #require(blend.choice)
        #expect(choice.type == .houseMadeBatch)
        #expect(choice.optionName == "Southwest spice blend (house blend)")
        #expect(choice.chosenAt == JSONCoding.parseDate("2026-09-15T18:30:00Z"))
        #expect(blend.hasBatchChoice)
        #expect(!blend.isKeptAsIs)
        #expect(blend.chosenOption?.id == "southwest-spice-blend.batch")

        #expect(blend.options.map(\.source) == [.curated, .curated, .household])
        let store = blend.options[0]
        #expect(store.type == .storeAlternative)
        #expect(store.per?.text == "1 tbsp")
        #expect(store.batchYield == nil)
        #expect(store.shelfLifeDays == nil)
        #expect(store.ingredients.map(\.text) == ["1 ½ tsp Chili Powder", "¾ tsp Ground Cumin"])
        #expect(store.ingredients[1].category == "spices")
        #expect(store.summary == "1 tbsp = 1 ½ tsp Chili Powder + ¾ tsp Ground Cumin")

        let batch = blend.options[1]
        #expect(batch.isBatch)
        #expect(batch.isDefault)
        #expect(batch.per == nil)
        #expect(batch.batchYield?.text == "12 tbsp")
        #expect(batch.shelfLifeDays == 180)
        #expect(batch.steps.count == 2)
        #expect(batch.ingredients.last?.quantity == nil)
        #expect(batch.notes == "Keep away from heat.")
        #expect(blend.isChosen(batch))

        let mine = blend.options[2]
        #expect(mine.isHousehold)
        #expect(mine.basedOnOptionID == "southwest-spice-blend.batch")
        #expect(mine.createdAt == JSONCoding.parseDate("2026-09-15T18:30:00Z"))

        let stock = try #require(blend.batch)
        #expect(stock.pantryItemID == "item-southwest")
        #expect(stock.status == .inStock)
        #expect(stock.remaining?.text == "9 tbsp")
        #expect(stock.percentRemaining == 75)
        #expect(stock.expiresOn == "2027-03-14")
    }

    @Test func browsingEverythingSendsAll() async throws {
        let transport = StubTransport { _ in (200, SpecialtyFixtures.list([])) }

        let items = try await makeAPI(transport).list(householdID: "household-1", all: true, accessToken: "t")

        #expect(items.isEmpty)
        #expect(transport.requests.first?.url?.query() == "all=true")
    }

    @Test func choosingAndClearingSendTheChoice() async throws {
        let transport = StubTransport { request in
            request.httpMethod == "DELETE" ? (204, Data()) : (200, Data(SpecialtyFixtures.southwestJSON.utf8))
        }
        let api = try makeAPI(transport)

        let updated = try await api.setChoice(
            householdID: "household-1", specialtyID: "southwest-spice-blend", optionID: SpecialtyChoice.asIsOptionID,
            accessToken: "t")
        try await api.clearChoice(householdID: "household-1", specialtyID: "southwest-spice-blend", accessToken: "t")

        #expect(updated.id == "southwest-spice-blend")
        let put = try #require(transport.requests.first)
        #expect(put.httpMethod == "PUT")
        #expect(put.url?.path() == basePath + "/southwest-spice-blend/choice")
        #expect(put.jsonBody == ["optionId": "as_is"])
        let delete = try #require(transport.requests.last)
        #expect(delete.httpMethod == "DELETE")
        #expect(delete.url?.path() == basePath + "/southwest-spice-blend/choice")
        #expect(delete.httpBody == nil)
    }

    @Test func useSuggestedForAllPostsWithoutABody() async throws {
        let transport = StubTransport { _ in (200, SpecialtyFixtures.defaultsJSON) }

        let response = try await makeAPI(transport).applyDefaults(householdID: "household-1", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == basePath + "/choices/defaults")
        #expect(request.httpBody == nil)
        #expect(response.items.map(\.id) == ["southwest-spice-blend"])
        #expect(response.skipped == 2)
    }

    @Test func householdOptionRequestsFollowTheContract() async throws {
        let transport = StubTransport { request in
            switch request.httpMethod {
            case "DELETE": (204, Data())
            case "POST": (201, Data(SpecialtyFixtures.householdOptionJSON.utf8))
            default: (200, Data(SpecialtyFixtures.householdOptionJSON.utf8))
            }
        }
        let api = try makeAPI(transport)
        let option = SpecialtyOptionRequest(
            type: .houseMadeBatch, name: "Mild southwest blend",
            ingredients: [
                SpecialtyIngredientInput(name: "Chili Powder", quantity: "3/2", unit: "tbsp"),
                SpecialtyIngredientInput(name: "Salt"),
            ],
            steps: ["Mix."], batchYield: SpecialtyAmountInput(quantity: "3", unit: "tbsp"), shelfLifeDays: 90,
            basedOnOptionID: "southwest-spice-blend.batch")

        let created = try await api.createOption(
            householdID: "household-1", specialtyID: "southwest-spice-blend", option: option, accessToken: "t")
        _ = try await api.updateOption(
            householdID: "household-1", specialtyID: "southwest-spice-blend", optionID: created.id, option: option,
            accessToken: "t")
        try await api.deleteOption(
            householdID: "household-1", specialtyID: "southwest-spice-blend", optionID: created.id, accessToken: "t")

        let requests = transport.requests
        try #require(requests.count == 3)
        #expect(requests[0].httpMethod == "POST")
        #expect(requests[0].url?.path() == basePath + "/southwest-spice-blend/options")
        let body = PantryFixtures.body(of: requests[0])
        #expect(
            Set(body.keys) == ["type", "name", "ingredients", "steps", "yield", "shelfLifeDays", "basedOnOptionId"])
        #expect(body["type"] as? String == "house_made_batch")
        #expect(body["basedOnOptionId"] as? String == "southwest-spice-blend.batch")
        #expect((body["yield"] as? [String: String]) == ["quantity": "3", "unit": "tbsp"])
        let ingredients = try #require(body["ingredients"] as? [[String: String]])
        #expect(ingredients == [["name": "Chili Powder", "quantity": "3/2", "unit": "tbsp"], ["name": "Salt"]])

        #expect(requests[1].httpMethod == "PUT")
        #expect(requests[1].url?.path() == basePath + "/southwest-spice-blend/options/66e5a1f2c3b4a5d6e7f80c01")
        #expect(requests[2].httpMethod == "DELETE")
        #expect(requests[2].url?.path() == basePath + "/southwest-spice-blend/options/66e5a1f2c3b4a5d6e7f80c01")
    }

    @Test func madeABatchSendsOnlyWhatsSetAndDecodesTheHouseMadePurchase() async throws {
        let transport = StubTransport { _ in (201, SpecialtyFixtures.batchResponseJSON) }

        let response = try await makeAPI(transport).recordBatch(
            householdID: "household-1", specialtyID: "southwest-spice-blend",
            request: RecordSpecialtyBatchRequest(clientPurchaseID: "client-1"), accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == basePath + "/southwest-spice-blend/batches")
        #expect(request.jsonBody == ["clientPurchaseId": "client-1"])
        #expect(response.purchase.source == .houseMade)
        #expect(response.purchase.quantity == "12")
        #expect(response.item.displayName == "Southwest Spice Blend (house-made)")
        #expect(response.item.unitSize?.per == "count")
        #expect(response.option.id == "southwest-spice-blend.batch")
        #expect(PantryUsageFormat.purchaseSource(response.purchase.source) == "House-made batch")
    }

    /// The household's standing strategy can pick an option that nobody chose, so the API sends
    /// a choice with no chooser and no time. Decoding those as non-optional failed the whole
    /// screen the first time a strategy applied.
    @Test func choiceDecodesWithoutAChooser() throws {
        let json = #"""
            {"optionId":"southwest-spice-blend.batch","type":"house_made_batch",
             "optionName":"Southwest spice blend (house blend)","chosenBy":null,"chosenAt":null}
            """#

        let choice = try JSONCoding.makeDecoder().decode(
            SpecialtyChoice.self, from: try #require(json.data(using: .utf8)))

        #expect(choice.optionID == "southwest-spice-blend.batch")
        #expect(choice.type == .houseMadeBatch)
        #expect(choice.chosenBy == nil)
        #expect(choice.chosenAt == nil)
    }
}
