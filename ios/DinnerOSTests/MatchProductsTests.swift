import Foundation
import Testing

@testable import DinnerOS

/// Starting Fry's from Walmart's saved products: pages of picks, keeping, swapping, or
/// skipping each, and saving them together.
@MainActor
struct MatchProductsTests {
    private struct Failure: Error {}

    private func product(_ id: String, _ name: String, stock: String? = "high") -> ShoppingSearchProduct {
        ShoppingSearchProduct(
            productID: id, name: name, brand: nil, size: "1 lb", packageSize: nil, imageURLString: nil,
            urlString: "https://www.frysfood.com/p/item/\(id)", priceCents: 199, stock: stock)
    }

    private func suggestion(
        _ key: String, _ name: String, pick: ShoppingSearchProduct?, alternatives: [ShoppingSearchProduct] = []
    )
        -> ShoppingMatchSuggestion
    {
        ShoppingMatchSuggestion(
            ingredientKey: key, ingredientName: name,
            from: .init(provider: "walmart", productID: "100", displayName: "\(name) (Walmart)", packageSize: nil),
            product: pick, alternatives: alternatives)
    }

    @Test func decodesAPageWithAndWithoutAPick() throws {
        let json = Data(
            #"""
            {"items":[
              {"ingredientKey":"k1","ingredientName":"Onion",
               "from":{"provider":"walmart","productId":"100","displayName":"Fresh Yellow Onion","packageSize":null},
               "product":{"productId":"0000000004093","name":"Jumbo Yellow Onions","brand":null,"size":"1 lb",
                          "packageSize":null,"imageUrl":null,"url":"https://www.frysfood.com/p/item/0000000004093",
                          "priceCents":129,"stock":"high"},
               "alternatives":[]},
              {"ingredientKey":"k2","ingredientName":"Shawarma Seasoning Blend",
               "from":{"provider":"walmart","productId":"101","displayName":"Shawarma Seasoning Blend","packageSize":null},
               "product":null}
            ],"next":"k2","total":76}
            """#.utf8)
        let page = try JSONDecoder().decode(ShoppingMatchSuggestionPage.self, from: json)
        #expect(page.total == 76 && page.next == "k2" && page.items.count == 2)
        #expect(
            page.items[0].product?.name == "Jumbo Yellow Onions"
                && page.items[0].from.displayName == "Fresh Yellow Onion")
        #expect(page.items[1].product == nil && page.items[1].alternatives.isEmpty)
    }

    @Test func loadsEveryPageWithPicksSelected() async {
        let model = MatchProductsModel()
        let pages = [
            ShoppingMatchSuggestionPage(
                items: [suggestion("k1", "Onion", pick: product("1", "Yellow Onions"))], next: "k1", total: 2),
            ShoppingMatchSuggestionPage(items: [suggestion("k2", "Shawarma", pick: nil)], next: nil, total: 2),
        ]
        var asked: [String?] = []
        await model.loadAll(
            fetch: { after in
                asked.append(after)
                return pages[asked.count - 1]
            }, message: { _ in "x" })
        #expect(asked == [nil, "k1"])
        #expect(model.items.count == 2 && model.total == 2 && model.phase == .loaded)
        #expect(model.selectedCount == 1 && model.product(for: model.items[0])?.productID == "1")
        #expect(model.product(for: model.items[1]) == nil)
    }

    @Test func aFailedPageKeepsWhatLoadedAndRetriesFromThere() async {
        let model = MatchProductsModel()
        var calls = 0
        let fetch: (String?) async throws -> ShoppingMatchSuggestionPage = { after in
            calls += 1
            if calls == 2 { throw Failure() }
            return after == nil
                ? ShoppingMatchSuggestionPage(
                    items: [suggestion("k1", "Onion", pick: product("1", "Onion"))], next: "k1", total: 2)
                : ShoppingMatchSuggestionPage(
                    items: [suggestion("k2", "Lime", pick: product("2", "Lime"))], next: nil, total: 2)
        }
        await model.loadAll(fetch: fetch, message: { _ in "Couldn't reach the store." })
        #expect(model.phase == .failed("Couldn't reach the store.") && model.items.count == 1)
        await model.loadAll(fetch: fetch, message: { _ in "x" })
        #expect(model.phase == .loaded && model.items.map(\.ingredientKey) == ["k1", "k2"])
    }

    @Test func keepSwapOrSkip() {
        let model = MatchProductsModel()
        let pick = product("1", "Fresh Poblano Peppers")
        let alt = product("2", "Diced Green Poblano Peppers")
        let out = product("3", "Organic Poblano", stock: "out")
        let item = suggestion("k1", "Long Green Pepper", pick: pick, alternatives: [alt, pick, out])
        #expect(model.options(for: item).map(\.productID) == ["1", "2", "3"])

        model.choose(alt, for: item)
        #expect(model.product(for: item)?.productID == "2")
        model.toggle(item)
        #expect(model.product(for: item) == nil && model.selectedCount == 0)
        model.toggle(item)
        #expect(model.product(for: item)?.productID == "1")
        model.choose(nil, for: item)
        #expect(model.selectedCount == 0)
    }

    @Test func savesTheChosenAndStopsAtAFailure() async {
        let model = MatchProductsModel()
        await model.loadAll(
            fetch: { _ in
                ShoppingMatchSuggestionPage(
                    items: [
                        suggestion("k1", "Onion", pick: product("1", "Onion")),
                        suggestion("k2", "Lime", pick: product("2", "Lime")),
                        suggestion("k3", "Shawarma", pick: nil),
                    ], next: nil, total: 3)
            }, message: { _ in "x" })
        var saved: [String] = []
        let ok = await model.save(
            using: { item, product in
                if item.ingredientKey == "k2", saved.count == 1 { throw Failure() }
                saved.append("\(item.ingredientKey)=\(product.productID)")
            }, message: { _ in "Couldn't save." })
        #expect(!ok && saved == ["k1=1"] && model.saveError == "Couldn't save.")
        #expect(model.items.map(\.ingredientKey) == ["k2", "k3"] && model.savedCount == 1 && model.total == 2)

        let retried = await model.save(
            using: { item, product in saved.append("\(item.ingredientKey)=\(product.productID)") },
            message: { _ in "x" })
        #expect(
            retried && saved == ["k1=1", "k2=2"] && model.items.map(\.ingredientKey) == ["k3"]
                && model.selectedCount == 0)
    }

    @Test func theSuggestionsRequestAsksForFrysFromWalmart() async throws {
        let transport = StubTransport { _ in (200, Data(#"{"items":[],"next":null,"total":0}"#.utf8)) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let page = try await ShoppingAPI(client: client).matchSuggestions(
            householdID: "household-1", provider: "kroger", from: "walmart", after: "k9", limit: 12,
            accessToken: "access-1")
        #expect(page.total == 0)
        let url = try #require(transport.requests.first?.url)
        #expect(url.path() == "/api/v1/households/household-1/shopping/kroger/suggestions")
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        #expect(query.contains(URLQueryItem(name: "from", value: "walmart")))
        #expect(query.contains(URLQueryItem(name: "after", value: "k9")))
        #expect(query.contains(URLQueryItem(name: "limit", value: "12")))
    }

    @Test func decodesTheStoreRequestReport() throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let json = Data(
            #"""
            {"items":[{"key":"sprouts","name":"Sprouts Farmers Market","status":"unsupported","inCatalog":true,"custom":false,
                       "households":3,"sources":{"nearby":2,"catalog":1},
                       "places":[{"name":"Sprouts #412","address":"1 Test St"}],"notes":["closest to us"],
                       "lastRequestedAt":"2026-11-01T18:00:00Z"}],
             "households":3,"custom":0}
            """#.utf8)
        let summary = try decoder.decode(StoreRequestSummary.self, from: json)
        #expect(summary.households == 3 && summary.items.first?.places.first?.address == "1 Test St")
        #expect(summary.items.first?.sourceSummary == "2 picked nearby · 1 from the list")
    }
}
