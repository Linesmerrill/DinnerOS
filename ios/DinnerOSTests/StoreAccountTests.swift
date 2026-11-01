import Foundation
import Testing

@testable import DinnerOS

/// Fry's through Kroger (decision 649): the store account, store finder, product search, and
/// the Shop tab's words for a store whose cart the API fills.
struct StoreAccountTests {
    private func decoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }

    @Test func decodesAKrogerProviderAndOldResponses() throws {
        let kroger = Data(
            #"""
            {"key":"kroger","name":"Fry's","affiliateTracked":false,
             "capabilities":{"handoff":"cart_write","pasteProductLink":true,"storeId":true,"productSearch":true,
                             "productLookup":false,"storeFinder":true,"cartWrite":true,"orderImport":false,"customerAccount":true}}
            """#.utf8)
        let provider = try decoder().decode(ShoppingProvider.self, from: kroger)
        #expect(provider.isSupported && provider.writesCart && provider.capabilities.customerAccount)

        let walmart = Data(
            #"{"key":"walmart","name":"Walmart","affiliateTracked":true,"capabilities":{"handoff":"cart_link","pasteProductLink":true,"storeId":true}}"#
                .utf8)
        let old = try decoder().decode(ShoppingProvider.self, from: walmart)
        #expect(old.isSupported && !old.writesCart && !old.capabilities.customerAccount)
    }

    @Test func settingsCarryTheStoreName() throws {
        let json = Data(
            #"{"provider":"kroger","storeId":"66000123","storeName":"Fry's Test Store","updatedBy":null,"updatedAt":null}"#
                .utf8)
        let settings = try decoder().decode(ShoppingSettings.self, from: json)
        #expect(settings.storeName == "Fry's Test Store")

        let request = UpdateShoppingSettingsRequest(
            provider: "kroger", storeID: "66000123", storeName: "Fry's Test Store")
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any]
        #expect(body?["storeName"] as? String == "Fry's Test Store" && body?["storeId"] as? String == "66000123")
        // Walmart's request sends no name.
        let walmart =
            try JSONSerialization.jsonObject(
                with: JSONEncoder().encode(UpdateShoppingSettingsRequest(provider: "walmart", storeID: nil)))
            as? [String: Any]
        #expect(walmart?["storeName"] == nil)
    }

    @Test func readsHowASignInEnded() throws {
        let connected = try #require(URL(string: "dinneros://shopping/connected?provider=kroger&result=connected"))
        #expect(ShoppingConnectResult(callback: connected) == .connected)
        let cancelled = try #require(URL(string: "dinneros://shopping/connected?provider=kroger&result=cancelled"))
        #expect(ShoppingConnectResult(callback: cancelled) == .cancelled)
        let odd = try #require(URL(string: "dinneros://shopping/connected"))
        #expect(ShoppingConnectResult(callback: odd) == .failed)
    }

    @Test func savingASearchedProductSendsItsUPCSizeAndPrice() throws {
        let json = Data(
            #"""
            {"productId":"0001111041700","name":"Test Pineapple Chunks","brand":"Test Farms","size":"20 oz",
             "packageSize":{"quantity":"20","quantityValue":20,"unit":"oz","text":"20 oz"},
             "imageUrl":"https://img.example.test/m.jpg","url":"https://www.frysfood.com/p/item/0001111041700",
             "priceCents":299,"stock":"low"}
            """#.utf8)
        let product = try decoder().decode(ShoppingSearchProduct.self, from: json)
        #expect(!product.isOutOfStock && product.imageURL != nil)
        let body =
            try JSONSerialization.jsonObject(
                with: JSONEncoder().encode(product.preferenceRequest(ingredientName: "Pineapple"))) as? [String: Any]
        #expect(body?["productId"] as? String == "0001111041700")
        #expect(body?["displayName"] as? String == "Test Pineapple Chunks")
        #expect(body?["priceCents"] as? Int == 299)
        #expect((body?["packageSize"] as? [String: Any])?["unit"] as? String == "oz")
        #expect(body?["ingredientName"] as? String == "Pineapple")
    }

    @Test func cartWordsNameTheStore() {
        #expect(ShoppingText.inCart(2) == "In Walmart cart · 2")
        #expect(
            ShoppingText.cartStatus(sent: 2, wanted: 1, store: "Fry's")
                == "In Fry's cart · 2 · Remove 1 in the Fry's app")
        #expect(ShoppingText.everythingInCart(in: "Fry's") == "Everything is already in your Fry's cart")
        #expect(ShoppingText.everythingInCart == "Everything is already in your Walmart cart")
    }
}
