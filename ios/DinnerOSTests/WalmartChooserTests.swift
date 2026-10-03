import Foundation
import Testing

@testable import DinnerOS

/// Choose in Walmart: stepping through the lines, which pages count as a choice, and what Save sends.
struct WalmartChooserTests {
    private func item(_ key: String, _ name: String, query: String? = nil) -> WalmartChooser.Item {
        WalmartChooser.Item(ingredientKey: key, name: name, amountText: "10 oz", searchQuery: query ?? name)
    }

    @Test func stepsThroughBackSkipAndChoose() {
        var chooser = WalmartChooser(items: [item("a", "Thyme"), item("b", "Panko"), item("c", "Potatoes")])
        #expect(chooser.current?.name == "Thyme")
        #expect(!chooser.canGoBack)
        #expect(chooser.remaining == 3)
        chooser.didChoose()
        #expect(chooser.current?.name == "Panko")
        #expect(chooser.remaining == 2)
        chooser.skip()
        #expect(chooser.current?.name == "Potatoes")
        chooser.back()
        #expect(chooser.current?.name == "Panko")
        // Skipped lines still count as left.
        chooser.skip()
        chooser.didChoose()
        #expect(chooser.isFinished)
        #expect(chooser.current == nil)
        #expect(chooser.remaining == 1)
        #expect(chooser.chosen == ["a", "c"])
    }

    @Test func searchesForTheLinesQuery() {
        let chooser = WalmartChooser(items: [item("a", "Garlic", query: "fresh whole Garlic")])
        #expect(chooser.searchURL?.absoluteString == "https://www.walmart.com/search?q=fresh%20whole%20Garlic")
    }

    @Test func onlyAProductPageIsAChoice() throws {
        let product = try #require(
            URL(string: "https://www.walmart.com/ip/Great-Value-Baking-Soda-1-lb/10315226?athbdg=L1600&from=/search"))
        #expect(WalmartChooser.productItemID(at: product) == "10315226")
        #expect(WalmartChooser.productItemID(at: URL(string: "https://www.walmart.com/search?q=baking%20soda")) == nil)
        #expect(WalmartChooser.productItemID(at: URL(string: "https://www.walmart.com/cp/baking/976759")) == nil)
        #expect(WalmartChooser.productItemID(at: URL(string: "https://www.example.com/ip/Thing/10315226")) == nil)
        #expect(WalmartChooser.productItemID(at: nil) == nil)
    }

    @Test func saveSendsTheCleanLinkThePagesNameAndPrice() throws {
        let url = try #require(
            URL(string: "https://www.walmart.com/ip/Great-Value-Baking-Soda-1-lb/10315226?athbdg=L1600&from=/search"))
        let request = try #require(
            WalmartChooser.request(
                for: item("k", "Baking Soda"), url: url, pageName: "Great Value Baking Soda, 1 lb", priceText: "$0.97"))
        #expect(request.product == .url("https://www.walmart.com/ip/Great-Value-Baking-Soda-1-lb/10315226"))
        #expect(request.displayName == "Great Value Baking Soda, 1 lb")
        #expect(request.ingredientName == "Baking Soda")
        #expect(request.price == .set(97))
        // Before the page is read, the name in the address stands in, and the price is kept as is.
        let early = try #require(
            WalmartChooser.request(for: item("k", "Baking Soda"), url: url, pageName: nil, priceText: nil))
        #expect(early.displayName == "Great Value Baking Soda 1 lb")
        #expect(early.price == .keep)
        // A search page can't be saved.
        let search = try #require(URL(string: "https://www.walmart.com/search?q=baking%20soda"))
        #expect(
            WalmartChooser.request(for: item("k", "Baking Soda"), url: search, pageName: nil, priceText: nil) == nil)
    }
}
