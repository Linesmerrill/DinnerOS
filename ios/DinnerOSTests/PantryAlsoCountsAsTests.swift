import Foundation
import Testing

@testable import DinnerOS

/// "Also Counts As": a pantry item linked to other ingredients, so a jar of thyme kept dried
/// answers for a recipe's "Dried Thyme" (decision 641).
struct PantryAlsoCountsAsTests {
    @Test func decodesTheItemsOtherNamesAndReadsOldResponsesAsNone() throws {
        let json = Data(
            #"""
            {"id":"i1","householdId":"household-1","ingredientId":null,"key":"thyme","displayName":"Thyme",
             "category":"spices","quantity":"4","quantityValue":4,"unit":"oz","status":"in_stock","isStaple":false,
             "expiresOn":null,"note":"","alsoCountsAs":["Dried Thyme"],"updatedBy":"u1",
             "createdAt":"2026-10-01T00:00:00Z","updatedAt":"2026-10-01T00:00:00Z"}
            """#.utf8)
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let item = try decoder.decode(PantryItem.self, from: json)
        #expect(item.alsoCountsAs == ["Dried Thyme"])

        let old = Data(
            String(decoding: json, as: UTF8.self).replacingOccurrences(
                of: #""alsoCountsAs":["Dried Thyme"],"#, with: ""
            ).utf8)
        #expect(try decoder.decode(PantryItem.self, from: old).alsoCountsAs.isEmpty)
    }

    @Test func sendsOnlyAChangedListCleanedUp() throws {
        let item = PantryFixtures.item(name: "Thyme", quantity: "4", quantityValue: 4, unit: "oz")
        var draft = PantryItemDraft(item: item)
        #expect(try draft.changes(from: item).alsoCountsAs == nil)

        draft.alsoCountsAs = ["  Dried   Thyme ", "dried thyme", ""]
        #expect(try draft.changes(from: item).alsoCountsAs == ["Dried Thyme"])

        // Removing every name sends an empty list, which clears them.
        var linked = item
        linked.alsoCountsAs = ["Dried Thyme"]
        var clearing = PantryItemDraft(item: linked)
        #expect(clearing.alsoCountsAs == ["Dried Thyme"])
        clearing.alsoCountsAs = []
        let changes = try clearing.changes(from: linked)
        #expect(changes.alsoCountsAs == [])
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(changes)) as? [String: Any]
        #expect((body?["alsoCountsAs"] as? [String]) == [])
    }
}

/// Re-choosing a product opens Choose in Walmart for that one line.
struct WalmartRechooseItemTests {
    @Test func aCheckedLineSearchesForItsName() {
        let line = ProductCheckLine(
            ingredientKey: "k1", name: "Tomato Paste", productID: "1", productName: "Old Paste", productURL: nil,
            result: WalmartProductResult(status: .gone, checkedAt: Date(timeIntervalSince1970: 0)))
        let item = WalmartChooser.Item(rechoosing: line)
        #expect(item.ingredientKey == "k1")
        #expect(item.searchQuery == "Tomato Paste")
        let chooser = WalmartChooser(items: [item])
        #expect(chooser.searchURL?.absoluteString == "https://www.walmart.com/search?q=Tomato%20Paste")
    }
}
