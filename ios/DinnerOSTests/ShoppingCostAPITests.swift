import Foundation
import Testing

@testable import DinnerOS

struct ShoppingCostAPITests {
    private func client(_ transport: StubTransport) throws -> APIClient {
        APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
    }

    private func jsonBody(_ request: URLRequest?) throws -> [String: Any] {
        let data = try #require(request?.httpBody)
        return try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONCoding.makeDecoder().decode(type, from: Data(json.utf8))
    }

    // MARK: Saved product prices

    @Test func savingAProductSendsItsPriceOnlyWhenAsked() async throws {
        let response = ShoppingFixtures.preferenceJSON(
            key: "i-sour-cream", ingredientName: "Sour Cream", productID: "100000001", displayName: "Test sour cream")
        let transport = StubTransport { _ in (200, Data(response.utf8)) }
        let api = ShoppingAPI(client: try client(transport))
        var request = ShoppingPreferenceRequest(product: .itemID("100000001"), displayName: "Test sour cream")

        for price in [FieldChange<Int>.keep, .clear, .set(248)] {
            request.price = price
            _ = try await api.savePreference(
                householdID: "household-1", provider: "walmart", ingredientKey: "i-sour-cream", preference: request,
                accessToken: "t")
        }

        let bodies = try transport.requests.map(jsonBody)
        // Absent keeps the stored price, so callers that don't know about prices never clear one.
        #expect(bodies[0]["priceCents"] == nil)
        #expect(bodies[1]["priceCents"] is NSNull)
        #expect(bodies[2]["priceCents"] as? Int == 248)
    }

    @Test func savedProductsDecodeWithAndWithoutAPrice() throws {
        let old = ShoppingFixtures.preferenceJSON(
            key: "i-rice", ingredientName: "Rice", productID: "100000004", displayName: "Test rice")
        let priced = String(old.dropLast()) + #","priceCents":399,"priceUpdatedAt":"2026-09-15T20:00:00Z"}"#

        #expect(try decode(ShoppingPreference.self, old).priceCents == nil)
        #expect(try decode(ShoppingPreference.self, old).priceUpdatedAt == nil)
        let preference = try decode(ShoppingPreference.self, priced)
        #expect(preference.priceCents == 399)
        #expect(preference.priceUpdatedAt != nil)
    }

    @Test func savedProductDraftStartsFromThePriceAndClearsIt() throws {
        var draft = SavedProductDraft(ingredientName: "Rice")
        draft.linkText = "100000004"
        #expect(draft.request(ingredientName: "Rice")?.price == .keep)
        draft.priceText = "3.99"
        #expect(draft.request(ingredientName: "Rice")?.price == .set(399))
        draft.priceText = "free"
        #expect(draft.priceError != nil)
        #expect(draft.request(ingredientName: "Rice") == nil)

        var saved = SavedProductDraft()
        saved.linkText = "100000004"
        saved.displayName = "Test rice"
        saved.startPrice(399)
        #expect(saved.priceText == "3.99")
        #expect(saved.request(ingredientName: nil)?.price == .keep)
        saved.priceText = ""
        #expect(saved.request(ingredientName: nil)?.price == .clear)
    }

    // MARK: Handoff lines

    @Test func handoffLinesDecodePriceAndPantryOrNeither() throws {
        let json = CostFixtures.handoffJSON(
            id: "handoff-1",
            lines: [
                CostFixtures.pricedLineJSON(
                    id: "l1", key: "i-cilantro", name: "Cilantro", displayName: "Test cilantro", priceCents: 78,
                    pantry: "not_tracked"),
                CostFixtures.pricedLineJSON(
                    id: "l2", key: "i-rice", name: "Rice", displayName: "Test rice", priceCents: nil, pantry: "tracked"),
                CostFixtures.pricedLineJSON(
                    id: "l3", key: "i-lime", name: "Lime", displayName: "Test limes", priceCents: nil, pantry: nil),
                CostFixtures.pricedLineJSON(
                    id: "l4", key: "i-kale", name: "Kale", displayName: "Test kale", priceCents: 1, pantry: "frozen"),
            ])

        let lines = try decode(ShoppingHandoff.self, json).lines

        #expect(lines.map(\.priceCents) == [78, nil, nil, 1])
        #expect(lines.map(\.pantry) == [.notTracked, .tracked, nil, ShoppingLinePantry(rawValue: "frozen")])
        #expect(lines[0].pantry?.text == "Used this week")
        #expect(lines[1].pantry?.text == "Added to pantry")
        #expect(lines[3].pantry?.text == nil)
    }

    @Test func confirmingSendsLinePricesOnlyWhenTyped() throws {
        let request = ConfirmShoppingOrderRequest.lines(
            [
                ConfirmedOrderLine(lineID: "l1", packages: nil, priceCents: 499),
                ConfirmedOrderLine(lineID: "l2", packages: 2),
            ], skipRest: true)

        let body = try #require(
            try JSONSerialization.jsonObject(with: JSONCoding.makeEncoder().encode(request)) as? [String: Any])
        let lines = try #require(body["lines"] as? [[String: Any]])
        #expect(lines[0]["priceCents"] as? Int == 499)
        #expect(Set(lines[1].keys) == ["lineId", "packages"])
    }

    @Test func orderConfirmationDraftNamesEveryLineOnceAPriceIsTyped() throws {
        let json = CostFixtures.handoffJSON(
            id: "handoff-1",
            lines: [
                CostFixtures.pricedLineJSON(
                    id: "l1", key: "i-rice", name: "Rice", displayName: "Test rice", priceCents: nil, pantry: nil,
                    status: "pending"),
                CostFixtures.pricedLineJSON(
                    id: "l2", key: "i-lime", name: "Lime", displayName: "Test limes", priceCents: nil, pantry: nil,
                    status: "pending"),
            ])
        let handoff = try decode(ShoppingHandoff.self, json)
        var draft = OrderConfirmationDraft(handoff: handoff)
        #expect(draft.confirmRequest == .all)

        draft.setPriceText("3.99", for: handoff.lines[0])
        #expect(
            draft.confirmRequest
                == .lines(
                    [
                        ConfirmedOrderLine(lineID: "l1", packages: nil, priceCents: 399),
                        ConfirmedOrderLine(lineID: "l2", packages: nil),
                    ], skipRest: true))

        draft.setPriceText("3.9.9", for: handoff.lines[1])
        #expect(draft.hasInvalidPrice)
        #expect(draft.confirmRequest == nil)
    }

    @Test func setLinePricesPostsLinesWithNullToClear() async throws {
        let response = CostFixtures.handoffJSON(
            id: "handoff-1",
            lines: [
                CostFixtures.pricedLineJSON(
                    id: "l1", key: "i-rice", name: "Rice", displayName: "Test rice", priceCents: 499, pantry: "tracked")
            ])
        let transport = StubTransport { _ in (200, Data(response.utf8)) }

        let handoff = try await ShoppingAPI(client: try client(transport)).setLinePrices(
            householdID: "household-1", handoffID: "handoff-1",
            request: ShoppingLinePricesRequest(lines: [
                ShoppingLinePrice(lineID: "l1", priceCents: 499), ShoppingLinePrice(lineID: "l2", priceCents: nil),
            ]),
            accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/shopping/handoffs/handoff-1/prices")
        let lines = try #require(try jsonBody(request)["lines"] as? [[String: Any]])
        #expect(lines[0]["lineId"] as? String == "l1")
        #expect(lines[0]["priceCents"] as? Int == 499)
        #expect(lines[1]["priceCents"] is NSNull)
        #expect(handoff.lines.first?.priceCents == 499)
    }

    // MARK: Week cost and savings

    @Test func weekCostDecodesEveryField() async throws {
        let transport = StubTransport { _ in (200, CostFixtures.weekCostJSON) }

        let cost = try await ShoppingAPI(client: try client(transport)).weekCost(
            householdID: "household-1", week: try #require(ISOWeek("2026-W38")), accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/shopping/weeks/2026-W38/cost")
        #expect(cost.orderTotalCents == 13_042)
        #expect(cost.spentCents == 13_042)
        #expect(cost.spentSource == .orderTotal)
        #expect(cost.itemsBought == 24)
        #expect(cost.itemsPriced == 18)
        #expect(cost.usedCents == 6_120)
        #expect(cost.stockedCents == 5_400)
        #expect(cost.earlierStockUsedCents == 310)
        #expect(cost.feesAndUnpricedCents == 1_200)
        #expect(cost.meals == 5)
        #expect(cost.costPerMealCents == 1_224)
        #expect(cost.mealKit == MealKitBaseline(weeklyCents: 13_000, meals: 5, perMealCents: 2_600))
        #expect(cost.savedCents == 6_880)
        #expect(cost.partial)
        #expect(cost.summary == "Based on 18 of 24 items with prices.")
        #expect(cost.hasContent)
        #expect(cost.items.map(\.pantry) == [.tracked, .notTracked, ShoppingLinePantry(rawValue: "sealed_away")])
        #expect(cost.items.map(\.usage) == [.measured, .wholePackage, WeekCostUsage(rawValue: "guessed")])
        #expect(cost.items[1].priceCents == nil)
        #expect(cost.items[0].id == "handoff-1|l1")
    }

    @Test func anEmptyWeekDecodesItsNulls() throws {
        let cost = try JSONCoding.makeDecoder().decode(WeekCost.self, from: CostFixtures.emptyWeekCostJSON)

        #expect(cost.spentCents == nil)
        #expect(cost.spentSource == nil)
        #expect(cost.mealKit == nil)
        #expect(cost.savedCents == nil)
        #expect(cost.summary.isEmpty)
        #expect(!cost.hasContent)
    }

    @Test func weekSpendPutsTheTotalOrNull() async throws {
        let transport = StubTransport { _ in (200, CostFixtures.weekCostJSON) }
        let api = ShoppingAPI(client: try client(transport))
        let week = try #require(ISOWeek("2026-W38"))

        _ = try await api.setWeekSpend(
            householdID: "household-1", week: week, orderTotalCents: 13_042, accessToken: "t")
        _ = try await api.setWeekSpend(householdID: "household-1", week: week, orderTotalCents: nil, accessToken: "t")

        let requests = transport.requests
        #expect(requests[0].httpMethod == "PUT")
        #expect(requests[0].url?.path() == "/api/v1/households/household-1/shopping/weeks/2026-W38/spend")
        #expect(try jsonBody(requests[0])["orderTotalCents"] as? Int == 13_042)
        #expect(try jsonBody(requests[1])["orderTotalCents"] is NSNull)
    }

    @Test func savingsDecodeNewestFirst() async throws {
        let transport = StubTransport { _ in (200, CostFixtures.savingsJSON) }

        let savings = try await ShoppingAPI(client: try client(transport)).savings(
            householdID: "household-1", limit: 12, accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/shopping/savings")
        #expect(transport.requests.first?.url?.query() == "limit=12")
        #expect(savings.weeks.map(\.week) == ["2026-W38", "2026-W37"])
        #expect(savings.weeks[1].spentCents == nil)
        #expect(savings.totalSavedCents == 6_880)
        #expect(savings.weeksCounted == 1)
        #expect(savings.mealKit?.perMealCents == 2_600)
    }

    // MARK: Household meal kit

    @Test func householdMealKitDecodesOrIsMissing() throws {
        let old = HouseholdFixtures.household(id: "household-1", name: "Lovelace")
        let withKit = String(old.dropLast()) + #","mealKit":{"weeklyCents":13000,"meals":5}}"#
        let cleared = String(old.dropLast()) + #","mealKit":null}"#

        #expect(try decode(Household.self, old).mealKit == nil)
        #expect(try decode(Household.self, cleared).mealKit == nil)
        let kit = try #require(try decode(Household.self, withKit).mealKit)
        #expect(kit.weeklyCents == 13_000)
        #expect(kit.meals == 5)
        #expect(kit.perMealCents == 2_600)
    }

    @Test func householdChangesSendTheMealKitThreeWays() throws {
        func body(_ changes: HouseholdChanges) throws -> [String: Any] {
            try #require(
                try JSONSerialization.jsonObject(with: JSONCoding.makeEncoder().encode(changes)) as? [String: Any])
        }

        #expect(try body(HouseholdChanges(name: "Renamed")).keys.sorted() == ["name"])
        #expect(HouseholdChanges().isEmpty)
        let cleared = HouseholdChanges(mealKit: .clear)
        #expect(!cleared.isEmpty)
        #expect(try body(cleared)["mealKit"] is NSNull)
        let set = try body(HouseholdChanges(mealKit: .set(MealKitInput(weeklyCents: 13_000, meals: 5))))
        #expect(set["mealKit"] as? [String: Int] == ["weeklyCents": 13_000, "meals": 5])
    }

    @Test func mealKitFormKeepsClearsOrSets() {
        let current = MealKitBaseline(weeklyCents: 13_000, meals: 5)
        #expect(MealKitForm.change(amountText: "", meals: 5, current: nil) == .keep)
        #expect(MealKitForm.change(amountText: "", meals: 5, current: current) == .clear)
        #expect(MealKitForm.change(amountText: "130", meals: 5, current: current) == .keep)
        #expect(
            MealKitForm.change(amountText: "$120.50", meals: 4, current: current)
                == .set(MealKitInput(weeklyCents: 12_050, meals: 4)))
        #expect(MealKitForm.change(amountText: "0", meals: 5, current: nil) == nil)
        #expect(MealKitForm.change(amountText: "130", meals: 22, current: nil) == nil)
        #expect(MealKitForm.error("lots") != nil)
    }

    // MARK: Pantry purchases

    @Test func pantryPurchasePricesDecodeAndPatch() async throws {
        let purchase = #"""
            {"id":"purchase-1","householdId":"household-1","itemId":"item-butter","source":"provider",
             "quantity":"1","quantityValue":1,"unit":"package","unitSize":null,"week":"2026-W38",
             "clientPurchaseId":null,"recordedBy":"user-1","purchasedAt":"2026-09-15T18:30:00Z","priceCents":499}
            """#
        let transport = StubTransport { _ in (200, Data(#"{"purchase":\#(purchase)}"#.utf8)) }
        let api = PantryAPI(client: try client(transport))

        let updated = try await api.updatePurchasePrice(
            householdID: "household-1", purchaseID: "purchase-1", priceCents: 499, accessToken: "t")
        _ = try await api.updatePurchasePrice(
            householdID: "household-1", purchaseID: "purchase-1", priceCents: nil, accessToken: "t")

        #expect(updated.priceCents == 499)
        let requests = transport.requests
        #expect(requests[0].httpMethod == "PATCH")
        #expect(requests[0].url?.path() == "/api/v1/households/household-1/pantry/purchases/purchase-1")
        #expect(try jsonBody(requests[0])["priceCents"] as? Int == 499)
        #expect(try jsonBody(requests[1])["priceCents"] is NSNull)

        let old = try JSONCoding.makeDecoder().decode(
            PantryPurchaseResponse.self, from: PantryFixtures.purchaseResponseJSON)
        #expect(old.purchase.priceCents == nil)

        var new = NewPantryPurchase(source: .manual, clientPurchaseID: "client-1")
        #expect(try body(new)["priceCents"] == nil)
        new.priceCents = 250
        #expect(try body(new)["priceCents"] as? Int == 250)
    }

    private func body(_ value: some Encodable) throws -> [String: Any] {
        try #require(try JSONSerialization.jsonObject(with: JSONCoding.makeEncoder().encode(value)) as? [String: Any])
    }
}
