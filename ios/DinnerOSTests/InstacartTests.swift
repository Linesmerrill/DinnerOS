import Foundation
import Testing

@testable import DinnerOS

/// Instacart as a store: the API makes a page of the list, so nothing is saved per ingredient.
struct InstacartTests {
    private func decoder() -> JSONDecoder { JSONCoding.makeDecoder() }

    private let instacartJSON = #"""
        {"key":"instacart","name":"Instacart","affiliateTracked":true,
         "capabilities":{"handoff":"list_page","pasteProductLink":false,"storeId":false,"productSearch":false,
                         "productLookup":false,"storeFinder":false,"cartWrite":false,"orderImport":false}}
        """#

    @Test func instacartIsAListPageStore() throws {
        let provider = try decoder().decode(ShoppingProvider.self, from: Data(instacartJSON.utf8))
        #expect(provider.isSupported && provider.opensListPage && !provider.writesCart)
        #expect(!provider.capabilities.pasteProductLink && !provider.capabilities.storeID)
        // Another store answering list_page isn't one this build knows.
        let other = ShoppingProvider(
            key: "somewhere", name: "Somewhere", affiliateTracked: false, capabilities: provider.capabilities)
        #expect(!other.isSupported)
    }

    @Test func settingUpInstacartSendsNoStore() throws {
        let request = UpdateShoppingSettingsRequest(
            provider: ShoppingProviderKey.instacart, storeID: nil, makeDefault: false)
        let json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any]
        #expect(json?["provider"] as? String == "instacart")
        // No store: sent as null, which clears any store.
        #expect(json?["storeId"] is NSNull)
        #expect(json?["makeDefault"] as? Bool == false)
    }

    @Test func aListPageSaysListNotCart() {
        #expect(
            ShoppingText.everythingInCart(in: "Instacart", listPage: true)
                == "Everything is already on your Instacart list")
        #expect(ShoppingText.everythingInCart(in: "Walmart") == "Everything is already in your Walmart cart")
    }
}

/// The commission disclosure next to a store's button: the API's words, else the app's own.
struct AffiliateDisclosureTests {
    @Test func usesTheAPIsWordsWhenItSendsThem() throws {
        let json = Data(
            #"""
            {"key":"walmart","name":"Walmart","affiliateTracked":true,
             "affiliateDisclosure":"DinnerOS may earn a commission when you buy through this link. Your price stays the same.",
             "capabilities":{"handoff":"cart_link","pasteProductLink":true,"storeId":true}}
            """#.utf8)
        let walmart = try JSONDecoder().decode(ShoppingProvider.self, from: json)
        #expect(
            ShoppingText.affiliateDisclosure(walmart.affiliateDisclosure, appName: "DinnerOS")
                == "DinnerOS may earn a commission when you buy through this link. Your price stays the same.")
        // An older API sends none: the app says it in its own words.
        #expect(
            ShoppingText.affiliateDisclosure(nil, appName: "DinnerOS")
                == "DinnerOS may earn a commission when you buy through this link. Your price stays the same.")
        #expect(ShoppingText.affiliateDisclosure("", appName: "Dinner").hasPrefix("Dinner may earn"))
    }
}
