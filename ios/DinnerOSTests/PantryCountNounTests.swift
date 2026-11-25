import Foundation
import Testing

@testable import DinnerOS

/// The pantry's Yellow Onions read "3" with no word (Hark's build 121 review). A count reads
/// with the word the server sends for it: "3 onions", "1 onion".
struct PantryCountNounTests {
    private let english = Locale(identifier: "en_US")
    private let onionWord = PantryCountNoun(one: "onion", other: "onions")

    @Test func decodesTheWordAndReadsOldResponsesAsNone() throws {
        let json = Data(
            #"""
            {"id":"i1","householdId":"household-1","ingredientId":null,"key":"yellow onion","displayName":"Yellow Onions",
             "category":"produce","quantity":"3","quantityValue":3,"unit":"count","status":"in_stock","isStaple":false,
             "expiresOn":null,"note":"","countNoun":{"one":"onion","other":"onions"},"updatedBy":"u1",
             "createdAt":"2026-10-01T00:00:00Z","updatedAt":"2026-10-01T00:00:00Z"}
            """#.utf8)
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let item = try decoder.decode(PantryItem.self, from: json)
        #expect(item.countNoun == onionWord)
        #expect(PantryFormat.amount(item, locale: english) == "3 onions")

        let old = Data(
            String(decoding: json, as: UTF8.self).replacingOccurrences(
                of: #""countNoun":{"one":"onion","other":"onions"},"#, with: ""
            ).utf8)
        let oldItem = try decoder.decode(PantryItem.self, from: old)
        #expect(oldItem.countNoun == nil)
        #expect(PantryFormat.amount(oldItem, locale: english) == "3")
    }

    @Test func theRecordedAmountReadsWithItsWord() {
        var three = PantryFixtures.item(name: "Yellow Onions", quantity: "3", quantityValue: 3, unit: "count")
        three.countNoun = onionWord
        #expect(PantryFormat.amount(three, locale: english) == "3 onions")
        #expect(PantryUsageFormat.rowAmount(three, locale: english) == "3 onions")

        var one = PantryFixtures.item(name: "Yellow Onions", quantity: "1", quantityValue: 1, unit: "count")
        one.countNoun = onionWord
        #expect(PantryFormat.amount(one, locale: english) == "1 onion")

        var none = PantryFixtures.item(name: "Yellow Onions", quantity: "0", quantityValue: 0, unit: "count")
        none.countNoun = onionWord
        #expect(PantryFormat.amount(none, locale: english) == "0 onions")
    }

    /// The row shows what's left by the estimate, and that reads with the word too.
    @Test func theEstimatedAmountReadsWithItsWord() {
        var onions = PantryFixtures.item(
            name: "Yellow Onions", quantity: "3", quantityValue: 3, unit: "count",
            estimate: PantryFixtures.estimate(percentRemaining: 100, unit: "count", start: 3, remaining: 3))
        onions.countNoun = onionWord
        #expect(PantryUsageFormat.rowAmount(onions, locale: english) == "3 onions")

        var lastOne = PantryFixtures.item(
            name: "Yellow Onions", quantity: "3", quantityValue: 3, unit: "count",
            estimate: PantryFixtures.estimate(percentRemaining: 10, unit: "count", start: 3, remaining: 0.3))
        lastOne.countNoun = onionWord
        #expect(PantryUsageFormat.rowAmount(lastOne, locale: english) == "1 onion")
        let short = PantryUsageFormat.remainingShort(
            lastOne.estimate!, besides: PantryUsageFormat.rowAmount(lastOne, locale: english), locale: english)
        #expect(short == "~10% left")
    }

    /// Other units keep their own label; the word is only for a count.
    @Test func otherUnitsIgnoreTheWord() {
        var garlic = PantryFixtures.item(name: "Garlic", quantity: "7", quantityValue: 7, unit: "clove")
        garlic.countNoun = PantryCountNoun(one: "garlic", other: "garlics")
        #expect(PantryFormat.amount(garlic, locale: english) == "7 cloves")
    }
}
