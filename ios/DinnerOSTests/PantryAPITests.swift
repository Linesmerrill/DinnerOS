import Foundation
import Testing

@testable import DinnerOS

struct PantryAPITests {
    private func makeClient(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    @Test func listDecodesItemsWithAndWithoutAmounts() async throws {
        let transport = StubTransport { _ in
            (200, PantryFixtures.list([PantryFixtures.oliveOilJSON, PantryFixtures.zaatarJSON]))
        }

        let items = try await PantryAPI(client: makeClient(transport))
            .listItems(householdID: "household-1", accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/pantry")
        #expect(request.url?.query() == nil)
        #expect(request.bearerToken == "token-1")

        let oil = try #require(items.first)
        #expect(oil.id == "item-oil")
        #expect(oil.householdID == "household-1")
        #expect(oil.ingredientID == "i-olive-oil")
        #expect(oil.key == "olive oil")
        #expect(oil.category == "pantry")
        #expect(oil.quantity == "3/2")
        #expect(oil.quantityValue == 1.5)
        #expect(oil.unit == "cup")
        #expect(oil.status == .inStock)
        #expect(oil.isStaple)
        #expect(oil.expiresOn == "2027-03-01")
        #expect(oil.note == "big tin")

        let zaatar = try #require(items.last)
        #expect(zaatar.ingredientID == nil)
        #expect(zaatar.quantity == nil)
        #expect(zaatar.quantityValue == nil)
        #expect(zaatar.unit == nil)
        #expect(zaatar.status == .low)
        #expect(zaatar.expiresOn == nil)
    }

    @Test func addSendsOnlyTheFieldsGiven() async throws {
        let transport = StubTransport { _ in (201, Data(PantryFixtures.oliveOilJSON.utf8)) }
        let newItem = NewPantryItem(
            ingredientID: "i-olive-oil", quantity: "3/2", unit: "cup", status: .inStock, isStaple: true)

        let item = try await PantryAPI(client: makeClient(transport))
            .addItem(householdID: "household-1", item: newItem, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/pantry")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        let body = PantryFixtures.body(of: request)
        #expect(Set(body.keys) == ["ingredientId", "quantity", "unit", "status", "isStaple"])
        #expect(body["ingredientId"] as? String == "i-olive-oil")
        #expect(body["quantity"] as? String == "3/2")
        #expect(body["status"] as? String == "in_stock")
        #expect(body["isStaple"] as? Bool == true)
        #expect(item.id == "item-oil")
    }

    @Test func mergedAddDecodesTheExistingItem() async throws {
        let transport = StubTransport { _ in (200, Data(PantryFixtures.zaatarJSON.utf8)) }

        let item = try await PantryAPI(client: makeClient(transport))
            .addItem(householdID: "h", item: NewPantryItem(name: "Za'atar", status: .low), accessToken: "t")

        #expect(PantryFixtures.body(of: try #require(transport.requests.first)).keys.sorted() == ["name", "status"])
        #expect(item.id == "item-zaatar")
    }

    @Test func updateSendsChangesAndEmptyStringsToClear() async throws {
        let transport = StubTransport { _ in (200, Data(PantryFixtures.zaatarJSON.utf8)) }
        let changes = PantryItemChanges(quantity: "", status: .low, expiresOn: "", note: "")

        _ = try await PantryAPI(client: makeClient(transport))
            .updateItem(householdID: "household-1", itemID: "item-zaatar", changes: changes, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "PATCH")
        #expect(request.url?.path() == "/api/v1/households/household-1/pantry/item-zaatar")
        #expect(request.jsonBody == ["quantity": "", "status": "low", "expiresOn": "", "note": ""])
    }

    @Test func emptyChangesAreEmpty() {
        #expect(PantryItemChanges().isEmpty)
        #expect(!PantryItemChanges(isStaple: false).isEmpty)
    }

    @Test func deleteAcceptsNoContent() async throws {
        let transport = StubTransport { _ in (204, Data()) }

        try await PantryAPI(client: makeClient(transport))
            .deleteItem(householdID: "household-1", itemID: "item-oil", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "DELETE")
        #expect(request.url?.path() == "/api/v1/households/household-1/pantry/item-oil")
        #expect(request.httpBody == nil)
    }

    @Test func bulkSendsStatusesAndDecodesMissing() async throws {
        let transport = StubTransport { _ in
            (200, Data(#"{"items":[\#(PantryFixtures.zaatarJSON)],"missing":["item-gone"]}"#.utf8))
        }
        let updates = [
            PantryStatusUpdate(id: "item-zaatar", status: .low), PantryStatusUpdate(id: "item-gone", status: .low),
        ]

        let response = try await PantryAPI(client: makeClient(transport))
            .setStatuses(householdID: "household-1", updates: updates, accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/pantry/bulk")
        let sent = try JSONDecoder().decode(
            [String: [PantryStatusUpdate]].self, from: try #require(request.httpBody))
        #expect(sent == ["items": updates])
        #expect(response.items.map(\.id) == ["item-zaatar"])
        #expect(response.missing == ["item-gone"])
    }

    @Test func defaultStaplesPostsWithoutABody() async throws {
        let transport = StubTransport { _ in
            (200, Data(#"{"items":[\#(PantryFixtures.oliveOilJSON)],"skipped":2}"#.utf8))
        }

        let response = try await PantryAPI(client: makeClient(transport))
            .addDefaultStaples(householdID: "household-1", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/pantry/staples/defaults")
        #expect(request.httpBody == nil)
        #expect(request.bearerToken == "t")
        #expect(response.items.map(\.displayName) == ["Olive Oil"])
        #expect(response.skipped == 2)
    }

    @Test func validationErrorsShowTheAPIMessage() async throws {
        let transport = StubTransport { _ in
            (400, Fixtures.errorJSON(code: "validation_failed", message: "an item that is out can't have a quantity"))
        }

        do {
            _ = try await PantryAPI(client: makeClient(transport))
                .addItem(householdID: "h", item: NewPantryItem(name: "Salt"), accessToken: "t")
            Issue.record("expected an error")
        } catch let error as APIError {
            #expect(error.code == "validation_failed")
            #expect(error.errorDescription == "an item that is out can't have a quantity")
        }
    }

    // MARK: - Ingredient catalog

    @Test func searchTrimsEncodesAndClampsTheLimit() async throws {
        let transport = StubTransport { _ in (200, PantryFixtures.catalogJSON) }

        _ = try await IngredientsAPI(client: makeClient(transport))
            .search(query: "  jalapeño & lime ", limit: 500, accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/ingredients")
        #expect(request.bearerToken == "token-1")
        #expect(request.url?.query(percentEncoded: true) == "q=jalape%C3%B1o%20%26%20lime&limit=50")
    }

    @Test func searchCutsLongQueriesAndKeepsALimitOfAtLeastOne() {
        let items = IngredientsAPI.queryItems(query: String(repeating: "a", count: 150), limit: 0)
        #expect(items.first?.value?.count == IngredientsAPI.maxQueryLength)
        #expect(items.last?.value == "1")
    }

    @Test func searchDecodesCatalogIngredients() async throws {
        let transport = StubTransport { _ in (200, PantryFixtures.catalogJSON) }

        let results = try await IngredientsAPI(client: makeClient(transport))
            .search(query: "oil", limit: 8, accessToken: "t")

        #expect(results.map(\.name) == ["Oil", "Olive Oil"])
        #expect(results[0].imageURLString == nil)
        #expect(results[1].imageURLString == "https://img.example.test/olive-oil.png")
        #expect(results[1].key == "olive oil")
        #expect(results[1].categoryConfident)
    }
}
