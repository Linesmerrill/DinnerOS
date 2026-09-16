import Foundation
import Testing

@testable import DinnerOS

struct ShoppingAPITests {
    private let week = ISOWeek("2026-W38")

    private func makeAPI(_ transport: StubTransport) throws -> ShoppingAPI {
        ShoppingAPI(client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
    }

    private func jsonBody(_ request: URLRequest?) throws -> [String: Any] {
        let data = try #require(request?.httpBody)
        return try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    // MARK: Providers and store

    @Test func providersDecodeWithCapabilities() async throws {
        let transport = StubTransport { _ in (200, ShoppingFixtures.providersJSON) }

        let providers = try await makeAPI(transport).providers(accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/shopping/providers")
        #expect(request.bearerToken == "token-1")
        let walmart = try #require(providers.first)
        #expect(providers.count == 1)
        #expect(walmart.key == "walmart")
        #expect(walmart.name == "Walmart")
        #expect(!walmart.affiliateTracked)
        #expect(walmart.capabilities.handoff == "cart_link")
        #expect(walmart.capabilities.pasteProductLink)
        #expect(walmart.capabilities.storeID)
        #expect(walmart.isSupported)
    }

    @Test func settingsDecodeUnsetAndSet() async throws {
        let calls = Counter()
        let transport = StubTransport { _ in
            calls.increment()
            return calls.value == 1
                ? (200, ShoppingFixtures.settingsJSON(provider: nil, storeID: nil))
                : (200, ShoppingFixtures.settingsJSON(provider: "walmart", storeID: "5435"))
        }
        let api = try makeAPI(transport)

        let unset = try await api.settings(householdID: "household-1", accessToken: "t")
        let set = try await api.settings(householdID: "household-1", accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/shopping/settings")
        #expect(unset.provider == nil)
        #expect(unset.storeID == nil)
        #expect(unset.updatedAt == nil)
        #expect(set.provider == "walmart")
        #expect(set.storeID == "5435")
        #expect(set.updatedBy == Fixtures.user.id)
        #expect(set.updatedAt != nil)
    }

    @Test func updateSettingsSendsTheStoreOrNull() async throws {
        let response = ShoppingFixtures.settingsJSON(provider: "walmart", storeID: "5435")
        let transport = StubTransport { _ in (200, response) }
        let api = try makeAPI(transport)

        _ = try await api.updateSettings(
            householdID: "household-1", settings: UpdateShoppingSettingsRequest(provider: "walmart", storeID: "5435"),
            accessToken: "t")
        _ = try await api.updateSettings(
            householdID: "household-1", settings: UpdateShoppingSettingsRequest(provider: "walmart"), accessToken: "t")

        let requests = transport.requests
        #expect(requests[0].httpMethod == "PUT")
        #expect(requests[0].url?.path() == "/api/v1/households/household-1/shopping/settings")
        let withStore = try jsonBody(requests[0])
        #expect(withStore["provider"] as? String == "walmart")
        #expect(withStore["storeId"] as? String == "5435")
        let withoutStore = try jsonBody(requests[1])
        #expect(Set(withoutStore.keys) == ["provider", "storeId"])
        #expect(withoutStore["storeId"] is NSNull)
    }

    @Test(arguments: [("", nil), (" 5435 ", nil), ("012345", nil), ("1234567", "digits"), ("12a", "digits")])
    func storeNumbersAreOneToSixDigits(text: String, errorFragment: String?) {
        let error = ShoppingStoreNumber.error(text)
        if let errorFragment {
            #expect(error?.contains(errorFragment) == true)
        } else {
            #expect(error == nil)
        }
        #expect(ShoppingStoreNumber.normalized(" 5435 ") == "5435")
        #expect(ShoppingStoreNumber.normalized("  ") == nil)
    }

    // MARK: Saved products

    @Test func preferencesDecodeWithAndWithoutAPackageSize() async throws {
        let body = #"""
            {"items":[
              \#(ShoppingFixtures.preferenceJSON(
                key: "i-beef", ingredientName: "Ground Beef", productID: "100000001",
                displayName: "Test Brand ground beef", size: ShoppingFixtures.amount("16", "oz"))),
              \#(ShoppingFixtures.preferenceJSON(
                key: "name:flour tortillas", ingredientName: "Flour Tortillas", productID: "100000009",
                displayName: "Test tortillas"))
            ]}
            """#
        let transport = StubTransport { _ in (200, Data(body.utf8)) }

        let items = try await makeAPI(transport).preferences(
            householdID: "household-1", provider: "walmart", accessToken: "t")

        #expect(transport.requests.first?.url?.path() == "/api/v1/households/household-1/shopping/walmart/preferences")
        #expect(items.map(\.ingredientKey) == ["i-beef", "name:flour tortillas"])
        #expect(items[0].ingredientID == "i-beef")
        #expect(items[0].productID == "100000001")
        #expect(items[0].productURLString == "https://www.walmart.com/ip/100000001")
        #expect(items[0].packageSize?.text == "16 oz")
        #expect(items[0].packageSize?.quantityValue == 16)
        #expect(items[1].ingredientID == nil)
        #expect(items[1].packageSize == nil)
    }

    @Test func savePreferencePercentEncodesTheKeyAndSendsTheLink() async throws {
        let response = ShoppingFixtures.preferenceJSON(
            key: "name:red onion", ingredientName: "Red Onion", productID: "100000005", displayName: "Test red onions",
            size: ShoppingFixtures.amount("3", "lb"))
        let transport = StubTransport { _ in (201, Data(response.utf8)) }
        let api = try makeAPI(transport)

        let saved = try await api.savePreference(
            householdID: "household-1", provider: "walmart", ingredientKey: "name:red onion",
            preference: ShoppingPreferenceRequest(
                product: .url("https://www.walmart.com/ip/Test-Red-Onions/100000005?classType=REGULAR"),
                displayName: "Test red onions", packageSize: ShoppingPackageSizeInput(quantity: "3", unit: "lb"),
                ingredientName: "Red Onion"),
            accessToken: "t")
        _ = try await api.savePreference(
            householdID: "household-1", provider: "walmart", ingredientKey: "name:half & half/2",
            preference: ShoppingPreferenceRequest(product: .itemID("100000006"), displayName: "Test half and half"),
            accessToken: "t")

        let requests = transport.requests
        #expect(requests[0].httpMethod == "PUT")
        #expect(
            requests[0].url?.path() == "/api/v1/households/household-1/shopping/walmart/preferences/name:red%20onion")
        let linkBody = try jsonBody(requests[0])
        #expect(Set(linkBody.keys) == ["productUrl", "displayName", "packageSize", "ingredientName"])
        #expect(
            linkBody["productUrl"] as? String
                == "https://www.walmart.com/ip/Test-Red-Onions/100000005?classType=REGULAR")
        #expect(linkBody["displayName"] as? String == "Test red onions")
        #expect(linkBody["packageSize"] as? [String: String] == ["quantity": "3", "unit": "lb"])
        #expect(linkBody["ingredientName"] as? String == "Red Onion")
        #expect(saved.ingredientKey == "name:red onion")

        // A `/` or `&` inside a key stays inside one path segment, encoded once.
        #expect(
            requests[1].url?.absoluteString
                == "https://api.example.test/api/v1/households/household-1/shopping/walmart/preferences/name:half%20%26%20half%2F2"
        )
        let idBody = try jsonBody(requests[1])
        #expect(Set(idBody.keys) == ["productId", "displayName", "packageSize"])
        #expect(idBody["productId"] as? String == "100000006")
        #expect(idBody["packageSize"] is NSNull)
    }

    @Test func deletePreferenceAcceptsNoContent() async throws {
        let transport = StubTransport { _ in (204, Data()) }

        try await makeAPI(transport).deletePreference(
            householdID: "household-1", provider: "walmart", ingredientKey: "name:red onion", accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "DELETE")
        #expect(request.url?.path() == "/api/v1/households/household-1/shopping/walmart/preferences/name:red%20onion")
        #expect(request.httpBody == nil)
    }

    // MARK: Match and handoff

    @Test func anEmptyMatchRequestIsAnEmptyObject() async throws {
        let transport = StubTransport { _ in (200, ShoppingFixtures.everyStateProposal) }

        _ = try await makeAPI(transport).match(
            householdID: "household-1", week: try #require(week), provider: "walmart",
            request: ShoppingMatchRequest(), accessToken: "t")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/plans/2026-W38/shopping/walmart/match")
        #expect(try jsonBody(request).isEmpty)
    }

    @Test func matchSendsSelectedLinesAndDecodesEveryLineState() async throws {
        let transport = StubTransport { _ in (200, ShoppingFixtures.everyStateProposal) }

        let proposal = try await makeAPI(transport).match(
            householdID: "household-1", week: try #require(week), provider: "walmart",
            request: ShoppingMatchRequest(
                lines: [
                    ShoppingLineSelection(ingredientKey: "i-beef", packages: 5),
                    ShoppingLineSelection(ingredientKey: "i-garlic"),
                ],
                checkedOffKeys: ["i-lime"]),
            accessToken: "t")

        let body = try jsonBody(transport.requests.first)
        #expect(Set(body.keys) == ["lines", "checkedOffKeys"])
        let lines = try #require(body["lines"] as? [[String: Any]])
        #expect(lines[0]["ingredientKey"] as? String == "i-beef")
        #expect(lines[0]["packages"] as? Int == 5)
        #expect(Set(lines[1].keys) == ["ingredientKey"])
        #expect(body["checkedOffKeys"] as? [String] == ["i-lime"])

        #expect(proposal.provider == "walmart")
        #expect(proposal.week == "2026-W38")
        #expect(proposal.storeID == "5435")
        #expect(!proposal.affiliateTracked)

        // Ready lines, including each "check amount" reason.
        #expect(proposal.lines.map(\.id) == ["l1", "l2", "l3", "l4"])
        #expect(proposal.lines.map(\.reason) == [nil, nil, .noPackageSize, .packageCountCapped])
        #expect(proposal.lines.map(\.checkAmount) == [false, false, true, true])
        let beef = proposal.lines[0]
        #expect(beef.name == "Ground Beef")
        #expect(beef.category == "meat-seafood")
        #expect(beef.groceryStatus == .toBuy)
        #expect(beef.product.productID == "100000001")
        #expect(beef.product.packageSize?.text == "16 oz")
        #expect(beef.computedPackages == 3)
        #expect(beef.packages == 3)
        #expect(!beef.packagesOverridden)
        #expect(beef.coverageText == "3 × 16 oz covers 36 oz")
        #expect(beef.reasonText == nil)
        #expect(beef.confirmation == nil)
        // A measurable need is counted exactly, whatever the coverage rule.
        #expect(beef.coverage == "per_amount")
        #expect(!beef.coversWeek)

        // Garlic: 4 cloves can't be measured against a bulb, so one bulb is taken to cover the
        // week rather than the line being flagged for someone to check every week.
        let garlic = proposal.lines[1]
        #expect(garlic.reasonText == nil)
        #expect(garlic.coverage == "per_week")
        #expect(garlic.coversWeek)
        #expect(garlic.coverageText == "1 × 1 ct covers this week (4 cloves)")
        // The bare ingredient name is the wrong search, so the API sends a better one.
        #expect(garlic.searchTerms.query == "fresh whole Garlic")
        #expect(garlic.searchTerms.qualifiers == ["fresh", "whole"])
        #expect(garlic.searchTerms.avoid.contains("powder"))
        #expect(garlic.searchTerms.why.hasPrefix("Produce"))
        #expect(proposal.lines[2].product.packageSize == nil)
        #expect(proposal.lines[3].packagesOverridden)
        #expect(proposal.lines[3].computedPackages == 99)
        #expect(proposal.lines[3].packages == 12)

        // Lines without a product are listed in `excluded`, and belong under "Needs a Product".
        #expect(proposal.needsProduct.map(\.ingredientKey) == ["name:flour tortillas"])
        #expect(proposal.needsProduct.first?.ingredientID == nil)
        #expect(proposal.needsProduct.first?.text == "Choose a Walmart product")
        // A line needing a product is exactly where the suggested search is used.
        #expect(proposal.needsProduct.first?.searchTerms.query == "Flour Tortillas")
        #expect(
            proposal.notIncluded.map(\.reason) == [
                .inPantry, .pantryHint, .houseMade, .checkedOff, .excluded, .notSelected, .notOnList,
            ])
        #expect(proposal.notIncluded.last?.groceryStatus == nil)
        #expect(proposal.notIncluded.first?.groceryStatus == .inPantry)
        #expect(Set(proposal.excluded.map(\.id)).count == proposal.excluded.count)

        #expect(proposal.cartLinks.map(\.lineIDs) == [["l1", "l2"], ["l3", "l4"]])
        #expect(proposal.cartLinks.map(\.itemCount) == [2, 2])
        #expect(
            proposal.cartLinks.first?.url?.absoluteString
                == "https://www.walmart.com/sc/cart/addToCart?items=100000001_3,100000002&storeId=5435")
    }

    @Test func createHandoffDecodesItsStatusAndConfirmations() async throws {
        let fields = ShoppingFixtures.proposalFields(
            lines: [
                ShoppingFixtures.lineJSON(
                    id: "l1", key: "i-beef", name: "Ground Beef", productID: "100000001", displayName: "Test beef",
                    size: nil, computed: 1, confirmation: ShoppingFixtures.confirmationJSON(status: "pending")),
                ShoppingFixtures.lineJSON(
                    id: "l2", key: "i-cilantro", name: "Cilantro", productID: "100000002", displayName: "Test cilantro",
                    size: nil, computed: 1,
                    confirmation: ShoppingFixtures.confirmationJSON(status: "confirmed", packages: 2)),
                ShoppingFixtures.lineJSON(
                    id: "l3", key: "i-lime", name: "Lime", productID: "100000003", displayName: "Test limes",
                    size: nil, computed: 1, confirmation: ShoppingFixtures.confirmationJSON(status: "skipped")),
            ],
            excluded: [], links: [], affiliateTracked: true)
        let json = ShoppingFixtures.handoffJSON(id: "handoff-1", status: "open", fields: fields)
        let transport = StubTransport { _ in (201, Data(json.utf8)) }

        let handoff = try await makeAPI(transport).createHandoff(
            householdID: "household-1", week: try #require(week), provider: "walmart",
            request: ShoppingMatchRequest(), accessToken: "t")

        #expect(
            transport.requests.first?.url?.path()
                == "/api/v1/households/household-1/plans/2026-W38/shopping/walmart/handoffs")
        #expect(handoff.id == "handoff-1")
        #expect(handoff.status == .open)
        #expect(handoff.week == "2026-W38")
        #expect(handoff.createdBy == Fixtures.user.id)
        #expect(handoff.proposal.affiliateTracked)
        #expect(handoff.lines.map(\.confirmation?.status) == [.pending, .confirmed, .skipped])
        #expect(handoff.lines[1].confirmation?.packages == 2)
        #expect(handoff.lines[1].confirmation?.purchaseID == "purchase-1")
        #expect(handoff.lines[2].confirmation?.skippedAt != nil)
    }

    @Test func handoffListSendsTheWeekAndStatus() async throws {
        let json = ShoppingFixtures.handoffJSON(
            id: "handoff-2", status: "open", fields: ShoppingFixtures.proposalFields(lines: [], excluded: [], links: [])
        )
        let transport = StubTransport { _ in (200, Data(#"{"items":[\#(json)]}"#.utf8)) }
        let api = try makeAPI(transport)

        let items = try await api.handoffs(
            householdID: "household-1", week: week, status: .open, accessToken: "t")
        _ = try await api.handoffs(householdID: "household-1", limit: 5, accessToken: "t")

        let requests = transport.requests
        #expect(requests[0].url?.path() == "/api/v1/households/household-1/shopping/handoffs")
        #expect(requests[0].url?.query(percentEncoded: true) == "week=2026-W38&status=open")
        #expect(requests[1].url?.query(percentEncoded: true) == "limit=5")
        #expect(items.map(\.id) == ["handoff-2"])
    }

    // MARK: Confirm

    @Test func confirmEncodesAllOrLinesWithSkipRest() async throws {
        let transport = StubTransport { _ in (200, Self.confirmResponse) }
        let api = try makeAPI(transport)

        for request in [
            ConfirmShoppingOrderRequest.all,
            .lines([ConfirmedOrderLine(lineID: "l1", packages: 2), ConfirmedOrderLine(lineID: "l3")], skipRest: true),
            .lines([ConfirmedOrderLine(lineID: "l1")], skipRest: false),
        ] {
            _ = try await api.confirm(
                householdID: "household-1", handoffID: "handoff-1", request: request, accessToken: "t")
        }

        let requests = transport.requests
        #expect(requests[0].httpMethod == "POST")
        #expect(requests[0].url?.path() == "/api/v1/households/household-1/shopping/handoffs/handoff-1/confirm")
        let all = try jsonBody(requests[0])
        #expect(Set(all.keys) == ["all"])
        #expect(all["all"] as? Bool == true)

        let subset = try jsonBody(requests[1])
        #expect(Set(subset.keys) == ["lines", "skipRest"])
        #expect(subset["skipRest"] as? Bool == true)
        let lines = try #require(subset["lines"] as? [[String: Any]])
        #expect(lines[0]["lineId"] as? String == "l1")
        #expect(lines[0]["packages"] as? Int == 2)
        #expect(Set(lines[1].keys) == ["lineId"])

        #expect(Set(try jsonBody(requests[2]).keys) == ["lines"])
    }

    @Test func confirmResponseDecodesPurchasesWithTheirProvider() async throws {
        let transport = StubTransport { _ in (200, Self.confirmResponse) }

        let response = try await makeAPI(transport).confirm(
            householdID: "household-1", handoffID: "handoff-1", request: .all, accessToken: "t")

        #expect(response.handoff.status == .done)
        #expect(response.purchases.map(\.lineID) == ["l1", "l2"])
        let first = try #require(response.purchases.first)
        #expect(first.ingredientKey == "i-beef")
        #expect(first.created)
        #expect(first.purchase?.source == .provider)
        #expect(first.purchase?.unit == "package")
        #expect(first.purchase?.unitSize?.unit == "oz")
        #expect(
            first.purchase?.provider
                == PantryPurchaseProvider(key: "walmart", handoffID: "handoff-1", lineID: "l1", productID: "100000001"))
        #expect(first.item?.displayName == "Butter")
        // A purchase this build can't read doesn't hide that the line was recorded.
        let second = try #require(response.purchases.last)
        #expect(!second.created)
        #expect(second.purchase == nil)
        #expect(second.ingredientKey == "i-cilantro")
    }

    @Test func errorsKeepValidationMessagesAndExplainAnUnavailableProvider() async throws {
        let calls = Counter()
        let transport = StubTransport { _ in
            calls.increment()
            return calls.value == 1
                ? (400, Fixtures.errorJSON(code: "validation_failed", message: "short links aren't supported"))
                : (503, Fixtures.errorJSON(code: "provider_unavailable"))
        }
        let api = try makeAPI(transport)

        for _ in 0..<2 {
            do {
                _ = try await api.savePreference(
                    householdID: "household-1", provider: "walmart", ingredientKey: "i-beef",
                    preference: ShoppingPreferenceRequest(product: .url("https://walmrt.us/x"), displayName: "Beef"),
                    accessToken: "t")
                Issue.record("expected an error")
            } catch {
                let message = ShoppingStore.message(for: error)
                if calls.value == 1 {
                    #expect(message == "short links aren't supported")
                } else {
                    #expect(message.contains("Walmart"))
                }
            }
        }
    }

    nonisolated private static let confirmResponse = Data(
        #"""
        {"handoff":\#(ShoppingFixtures.handoffJSON(
            id: "handoff-1", status: "done", fields: ShoppingFixtures.proposalFields(lines: [], excluded: [], links: []))),
         "purchases":[
           {"lineId":"l1","ingredientKey":"i-beef","created":true,
            "purchase":{"id":"purchase-1","householdId":"household-1","itemId":"item-butter","source":"provider",
              "quantity":"2","quantityValue":2,"unit":"package",
              "unitSize":{"per":"package","quantity":"16","quantityValue":16,"unit":"oz"},"week":"2026-W38",
              "clientPurchaseId":null,"recordedBy":"user-1","purchasedAt":"2026-09-15T20:00:00Z",
              "provider":{"key":"walmart","handoffId":"handoff-1","lineId":"l1","productId":"100000001"}},
            "item":\#(PantryFixtures.butterUsageJSON)},
           {"lineId":"l2","ingredientKey":"i-cilantro","created":false,"purchase":{"id":5},"item":null}
         ]}
        """#.utf8)
}
