import Foundation
import Testing

@testable import DinnerOS

struct PantryUsageAPITests {
    private func makeClient(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    @Test func itemsDecodeUsageFieldsAndOlderResponsesDefaultThem() async throws {
        let transport = StubTransport { _ in
            (200, PantryFixtures.list([PantryFixtures.butterUsageJSON, PantryFixtures.oliveOilJSON]))
        }

        let items = try await PantryAPI(client: makeClient(transport))
            .listItems(householdID: "household-1", accessToken: "t")

        let butter = try #require(items.first)
        #expect(butter.statusSource == .estimate)
        #expect(butter.isEstimatedLow)
        #expect(butter.lowThresholdPercent == 60)
        #expect(butter.unitSize == PantryUnitSize(per: "package", quantity: "8", quantityValue: 8, unit: "oz"))
        let estimate = try #require(butter.estimate)
        #expect(estimate.cycleID == "66e5a1f2c3b4a5d6e7f80f01")
        #expect(estimate.cycleSource == "grocery_list")
        #expect(estimate.adjustedAt == nil)
        #expect(estimate.unit == "tbsp")
        #expect(estimate.startAmount == PantryAmount(quantity: "16", quantityValue: 16))
        #expect(estimate.remaining.quantityValue == 5)
        #expect(estimate.percentRemaining == 31)
        #expect(estimate.percentUsed == 69)
        #expect(estimate.recipeUse == PantryRecipeUse(count: 2, quantity: "6", quantityValue: 6))
        #expect(estimate.dailyRate?.basedOnSegments == 3)
        #expect(estimate.skippedRecipes == 1)
        #expect(estimate.lowThresholdPercent == 60)
        #expect(estimate.thresholdSource == .item)
        #expect(estimate.belowThreshold)
        #expect(estimate.summary.hasPrefix("About 31% left"))
        #expect(estimate.estimatedAt == JSONCoding.parseDate("2026-09-20T18:30:00Z"))

        let oil = try #require(items.last)
        #expect(oil.statusSource == .person)
        #expect(!oil.isEstimatedLow)
        #expect(oil.lowThresholdPercent == nil)
        #expect(oil.unitSize == nil)
        #expect(oil.estimate == nil)
    }

    @Test func anUnreadableEstimateStillShowsTheItem() async throws {
        let transport = StubTransport { _ in (200, PantryFixtures.list([PantryFixtures.unreadableEstimateJSON])) }

        let items = try await PantryAPI(client: makeClient(transport))
            .listItems(householdID: "household-1", accessToken: "t")

        let rice = try #require(items.first)
        #expect(rice.displayName == "Rice")
        #expect(rice.estimate == nil)
        #expect(rice.statusSource.rawValue == "robot")
        #expect(!rice.isEstimatedLow)
    }

    @Test func groceryPurchaseSendsTheLineAndDecodesTheResponse() async throws {
        let transport = StubTransport { _ in (201, PantryFixtures.purchaseResponseJSON) }
        let purchase = NewPantryPurchase(
            ingredientID: "i-butter", source: .groceryList, quantity: "1", unit: "cup", week: "2026-W38",
            clientPurchaseID: "client-1")

        let response = try await PantryAPI(client: makeClient(transport))
            .recordPurchase(householdID: "household-1", purchase: purchase, accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/pantry/purchases")
        #expect(request.bearerToken == "token-1")
        #expect(
            request.jsonBody == [
                "ingredientId": "i-butter", "source": "grocery_list", "quantity": "1", "unit": "cup",
                "week": "2026-W38", "clientPurchaseId": "client-1",
            ])
        #expect(response.purchase.id == "purchase-1")
        #expect(response.purchase.source == .groceryList)
        #expect(response.purchase.clientPurchaseID == "client-1")
        #expect(response.purchase.week == "2026-W38")
        #expect(response.item.id == "item-butter")
        #expect(response.item.estimate?.percentRemaining == 31)
    }

    @Test func restockSendsTheItemAndPackageSize() async throws {
        let transport = StubTransport { _ in (201, PantryFixtures.purchaseResponseJSON) }
        let purchase = NewPantryPurchase(
            itemID: "item-butter", source: .manual, quantity: "2", unit: "package",
            unitSize: PantryUnitSizeInput(quantity: "8", unit: "oz"), clientPurchaseID: "client-2")

        _ = try await PantryAPI(client: makeClient(transport))
            .recordPurchase(householdID: "household-1", purchase: purchase, accessToken: "t")

        let body = PantryFixtures.body(of: try #require(transport.requests.first))
        #expect(Set(body.keys) == ["itemId", "source", "quantity", "unit", "unitSize", "clientPurchaseId"])
        #expect(body["source"] as? String == "manual")
        let size = try #require(body["unitSize"] as? [String: String])
        #expect(size == ["quantity": "8", "unit": "oz"])
    }

    @Test func purchaseHistoryDecodesWithAndWithoutAmounts() async throws {
        let transport = StubTransport { _ in (200, PantryFixtures.purchaseHistoryJSON) }

        let purchases = try await PantryAPI(client: makeClient(transport))
            .purchases(householdID: "household-1", itemID: "item-butter", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/pantry/item-butter/purchases")
        #expect(purchases.map(\.id) == ["purchase-2", "purchase-1"])
        #expect(purchases[0].unitSize?.unit == "oz")
        #expect(purchases[0].source == .manual)
        #expect(purchases[1].quantity == nil)
        #expect(purchases[1].clientPurchaseID == nil)
    }

    @Test func settingsReadAndUpdate() async throws {
        let transport = StubTransport { request in
            request.httpMethod == "PUT"
                ? (
                    200,
                    Data(
                        #"{"lowThresholdPercent":70,"defaultLowThresholdPercent":80,"updatedBy":"user-1","updatedAt":"2026-09-15T18:30:00Z"}"#
                            .utf8)
                )
                : (200, PantryFixtures.settingsJSON)
        }
        let api = PantryAPI(client: try makeClient(transport))

        let current = try await api.settings(householdID: "household-1", accessToken: "t")
        let updated = try await api.updateSettings(
            householdID: "household-1", lowThresholdPercent: 70, accessToken: "t")

        #expect(current == PantrySettings(lowThresholdPercent: 80))
        #expect(updated.lowThresholdPercent == 70)
        #expect(updated.updatedBy == "user-1")
        let requests = transport.requests
        #expect(requests.map(\.httpMethod) == ["GET", "PUT"])
        #expect(requests.allSatisfy { $0.url?.path() == "/api/v1/households/household-1/pantry/settings" })
        let body = PantryFixtures.body(of: requests[1])
        #expect(body.count == 1)
        #expect(body["lowThresholdPercent"] as? Int == 70)
    }

    @Test func settingsWithoutADefaultUseEighty() throws {
        let settings = try JSONCoding.makeDecoder().decode(
            PantrySettings.self, from: Data(#"{"lowThresholdPercent":65}"#.utf8))
        #expect(settings.lowThresholdPercent == 65)
        #expect(settings.defaultLowThresholdPercent == 80)
    }

    @Test func thresholdChangesSendAnIntegerNullOrNothing() throws {
        func encoded(_ changes: PantryItemChanges) throws -> [String: Any] {
            let data = try JSONCoding.makeEncoder().encode(changes)
            return try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        }

        let own = try encoded(PantryItemChanges(lowThresholdPercent: .percent(65)))
        #expect(own.count == 1)
        #expect(own["lowThresholdPercent"] as? Int == 65)

        let household = try encoded(PantryItemChanges(lowThresholdPercent: .household))
        #expect(household.count == 1)
        #expect(household["lowThresholdPercent"] is NSNull)

        let untouched = try encoded(PantryItemChanges(note: "big tin"))
        #expect(Set(untouched.keys) == ["note"])
        #expect(!PantryItemChanges(lowThresholdPercent: .household).isEmpty)
    }
}
