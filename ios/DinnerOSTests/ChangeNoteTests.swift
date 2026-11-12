import Foundation
import Testing

@testable import DinnerOS

/// The note on "pick a different product" reaches the line and the product picker.
struct ChangeNoteTests {
    @Test func aFlaggedProductCarriesItsNote() throws {
        let json = """
            {"productId":"1","displayName":"Ramen","productUrl":"https://www.walmart.com/ip/1",
             "changeRequested":true,"changeNote":"Get lo mein noodles"}
            """
        let product = try JSONDecoder().decode(ShoppingLineProduct.self, from: Data(json.utf8))
        #expect(product.changeRequested)
        #expect(product.changeNote == "Get lo mein noodles")

        let older = try JSONDecoder().decode(
            ShoppingLineProduct.self,
            from: Data(#"{"productId":"1","displayName":"Ramen","productUrl":"https://www.walmart.com/ip/1"}"#.utf8))
        #expect(older.changeNote.isEmpty)
    }
}
