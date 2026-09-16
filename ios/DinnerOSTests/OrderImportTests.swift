import Foundation
import Testing

@testable import DinnerOS

struct MoneyTextTests {
    @Test(arguments: [
        ("4.98", 498), ("$4.98", 498), ("4", 400), (" $ 4.5 ", 450), ("$1,234.50", 123_450), ("0.99", 99),
        (".75", 75), ("0", 0),
    ])
    func parsesDollarText(text: String, cents: Int) {
        #expect(MoneyText.cents(from: text) == cents)
    }

    @Test(arguments: ["", "  ", "abc", "-4.98", "$-1", "4.989", "4.9.8", "1,23", "$", "4 dollars", "12,34.00"])
    func rejectsWhatIsntAPrice(text: String) {
        #expect(MoneyText.cents(from: text) == nil)
    }

    @Test func formatsAndEditsCents() {
        let locale = Locale(identifier: "en_US")
        #expect(MoneyText.format(498, locale: locale) == "$4.98")
        #expect(MoneyText.format(13_042, locale: locale) == "$130.42")
        #expect(MoneyText.format(0, locale: locale) == "$0.00")
        #expect(MoneyText.editingText(498) == "4.98")
        #expect(MoneyText.editingText(405) == "4.05")
        #expect(MoneyText.editingText(400) == "4.00")
    }

    @Test func errorsExplainInvalidAndTooLargePrices() {
        #expect(MoneyText.error("") == nil)
        #expect(MoneyText.error("4.98") == nil)
        #expect(MoneyText.error("four") != nil)
        #expect(MoneyText.error("10000.01") != nil)
        #expect(MoneyText.error("10000") == nil)
    }

    @Test func aPriceFieldKeepsClearsOrSets() {
        #expect(FieldChange.price(text: "", original: nil) == .keep)
        #expect(FieldChange.price(text: " ", original: 498) == .clear)
        #expect(FieldChange.price(text: "4.98", original: 498) == .keep)
        #expect(FieldChange.price(text: "$5", original: 498) == .set(500))
        #expect(FieldChange.price(text: "oops", original: nil) == nil)
    }

    @Test func weekCostComparisonReadsBothWays() {
        let locale = Locale(identifier: "en_US")
        #expect(WeekCostText.comparison(savedCents: 6_880, locale: locale) == "Saved $68.80 vs meal kit")
        #expect(WeekCostText.comparison(savedCents: -400, locale: locale) == "$4.00 more than meal kit")
        #expect(WeekCostText.comparison(savedCents: 0, locale: locale) == "Same as meal kit")
    }
}

struct OrderScreenshotParserTests {
    @Test func readsItemsQuantitiesAndTotals() {
        let order = OrderScreenshotParser.parse(text: CostFixtures.orderDetailsText)

        #expect(
            order.items.map(\.name) == [
                "Great Value Sour Cream, 16 oz", "Harborline Soy Sauce, 10 fl oz", "Fresh Bananas, each",
                "Great Value Light Brown Sugar, 2 lb Bag", "Testfield Farms Ground Beef 80/20, 1 lb",
                "Sample Brand Cilantro Bunch",
            ])
        // Was-prices, per-unit prices, and an item's savings never become its price.
        #expect(order.items.map(\.priceCents) == [248, 596, 152, 424, 547, 78])
        #expect(order.items.map(\.quantity) == [1, 2, 6, 2, 1, 1])
        #expect(order.items.map(\.isWeightAdjusted) == [false, false, true, false, false, false])
        #expect(order.items.map(\.status) == [.ordered, .ordered, .ordered, .ordered, .substituted, .unavailable])
        #expect(order.items.map(\.wasCharged) == [true, true, true, true, true, false])
        #expect(order.subtotalCents == 11_842)
        #expect(order.savingsCents == 300)
        #expect(order.feesCents == 995)
        #expect(order.taxCents == 405)
        #expect(order.tipCents == 600)
        #expect(order.totalCents == 13_042)
    }

    @Test func overlappingScreenshotsDontRepeatItems() {
        let lines = (CostFixtures.orderDetailsText + "\n" + CostFixtures.overlappingScreenshotText)
            .components(separatedBy: .newlines)

        let order = OrderScreenshotParser.parse(lines: lines)

        #expect(order.items.count == 6)
        #expect(order.items.map(\.id) == Array(0..<6))
    }

    @Test func readsPricesAboveNamesAndTotalsOnTheNextLine() {
        let order = OrderScreenshotParser.parse(text: CostFixtures.pricesFirstText)

        #expect(
            order.items.map(\.name) == [
                "Testfield Farms Shredded Cheddar Cheese, 8 oz", "Sample Brand Flour Tortillas 10 ct",
                "Yellow Onions, 3 lb Bag",
            ])
        #expect(order.items.map(\.priceCents) == [397, 624, 118])
        #expect(order.items.map(\.quantity) == [1, 2, 1])
        #expect(order.subtotalCents == 1_139)
        #expect(order.taxCents == 80)
        #expect(order.totalCents == 1_219)
    }

    @Test func readsNamesWithTheirPriceOnTheSameRow() {
        let order = OrderScreenshotParser.parse(text: CostFixtures.sameRowText)

        #expect(order.items.map(\.name) == ["Great Value Sour Cream, 16 oz", "Harborline Soy Sauce, 10 fl oz"])
        #expect(order.items.map(\.priceCents) == [248, 596])
        #expect(order.items.map(\.quantity) == [1, 2])
        #expect(order.totalCents == 844)
    }

    @Test func ignoresScreensWithoutItems() {
        let order = OrderScreenshotParser.parse(
            text: """
                Order details
                Delivered on Sep 15
                Start a return
                Visa ending in 1234
                """)

        #expect(order.items.isEmpty)
        #expect(order.totalCents == nil)
    }

    @Test(arguments: [
        ("$4.98", OrderScreenshotParser.Token.price(498)),
        ("Was $5.48", .ignoredPrice),
        ("-$1.00", .ignoredPrice),
        ("$0.31/oz", .ignoredPrice),
        ("Qty 3", .quantity(3, price: nil)),
        ("3 x $1.25", .multiplied(quantity: 3, unitCents: 125)),
        ("Refunded", .marker(.refunded)),
        ("Unavailable items (2)", .section(.unavailable)),
        ("Service fee $3.99", .summary(.fee, amount: 399)),
        ("Estimated total", .summary(.total, amount: nil)),
        ("Sunflower Seeds, 16 oz", .name("Sunflower Seeds, 16 oz", trailingPrice: nil)),
        ("Marinara Sauce 24 oz", .name("Marinara Sauce 24 oz", trailingPrice: nil)),
    ])
    func classifiesLines(line: String, token: OrderScreenshotParser.Token) {
        #expect(OrderScreenshotParser.classify(line) == token)
    }
}

struct OrderPriceMatcherTests {
    private func lines() throws -> [PriceableLine] {
        let json = CostFixtures.handoffJSON(
            id: "handoff-1",
            lines: [
                CostFixtures.pricedLineJSON(
                    id: "l1", key: "i-sour-cream", name: "Sour Cream", displayName: "Great Value Sour Cream 16 oz",
                    priceCents: nil, pantry: "tracked"),
                CostFixtures.pricedLineJSON(
                    id: "l2", key: "i-soy", name: "Soy Sauce", displayName: "Soy sauce", priceCents: nil,
                    pantry: "tracked"),
                CostFixtures.pricedLineJSON(
                    id: "l3", key: "i-banana", name: "Bananas", displayName: "Bananas", priceCents: nil,
                    pantry: "not_tracked"),
                CostFixtures.pricedLineJSON(
                    id: "l4", key: "i-sugar", name: "Brown Sugar", displayName: "Light brown sugar", priceCents: nil,
                    pantry: "tracked"),
                CostFixtures.pricedLineJSON(
                    id: "l5", key: "i-beef", name: "Ground Beef", displayName: "Test Brand beef", priceCents: nil,
                    pantry: "not_tracked"),
                CostFixtures.pricedLineJSON(
                    id: "l6", key: "i-cilantro", name: "Cilantro", displayName: "Test cilantro", priceCents: nil,
                    pantry: "not_tracked"),
                CostFixtures.pricedLineJSON(
                    id: "l7", key: "i-milk", name: "Whole Milk", displayName: "Test whole milk", priceCents: 348,
                    pantry: "tracked"),
            ])
        let handoff = try JSONCoding.makeDecoder().decode(ShoppingHandoff.self, from: Data(json.utf8))
        return handoff.lines.map { PriceableLine(handoffID: handoff.id, line: $0) }
    }

    private func id(_ lineID: String) -> PriceableLine.ID {
        PriceableLine.ID(handoffID: "handoff-1", lineID: lineID)
    }

    @Test func matchesReceiptNamesToLinesWithConfidence() throws {
        let order = OrderScreenshotParser.parse(text: CostFixtures.orderDetailsText)

        let matching = OrderPriceMatcher.match(items: order.items, lines: try lines())

        #expect(matching.matches.map(\.itemID) == [0, 1, 2, 3, 4])
        #expect(matching.matches.map(\.lineID) == [id("l1"), id("l2"), id("l3"), id("l4"), id("l5")])
        #expect(matching.matches.allSatisfy { $0.confidence == .high })
        // The cilantro was unavailable, so it isn't matched even though the name fits.
        #expect(matching.unmatchedItemIDs == [5])
        #expect(matching.unmatchedLineIDs == [id("l6"), id("l7")])
    }

    @Test func eachLineTakesOnlyItsBestItem() throws {
        let items = [
            ParsedOrderItem(
                id: 0, name: "Sample Organic Grass Fed Whole Milk", priceCents: 599, quantity: 1,
                isWeightAdjusted: false, status: .ordered),
            ParsedOrderItem(
                id: 1, name: "Whole Milk, 1 gal", priceCents: 348, quantity: 1, isWeightAdjusted: false,
                status: .ordered),
        ]

        let matching = OrderPriceMatcher.match(items: items, lines: try lines())

        #expect(matching.matches.map(\.itemID) == [1])
        #expect(matching.matches.first?.lineID == id("l7"))
        #expect(matching.unmatchedItemIDs == [0])
    }

    @Test func weakNameOverlapStaysUnmatched() throws {
        let items = [
            ParsedOrderItem(
                id: 0, name: "Test Chicken Broth", priceCents: 250, quantity: 1, isWeightAdjusted: false,
                status: .ordered),
            ParsedOrderItem(
                id: 1, name: "Sample Dish Soap", priceCents: 199, quantity: 1, isWeightAdjusted: false,
                status: .ordered),
        ]

        let matching = OrderPriceMatcher.match(items: items, lines: try lines())

        #expect(matching.matches.isEmpty)
        #expect(matching.unmatchedItemIDs == [0, 1])
    }

    @Test func namesNormalizeWithoutSizesBrandsOrPlurals() {
        #expect(OrderPriceMatcher.normalizedName("Great Value Yellow Onions, 3 lb Bag") == "yellow onion")
        #expect(OrderPriceMatcher.normalizedName("Roma Tomatoes (each)") == "roma tomato")
        #expect(OrderPriceMatcher.normalizedName("Fresh Strawberries 16oz") == "fresh strawberry")
        #expect(OrderPriceMatcher.score(["tomato", "paste"], ["tomato", "sauce"]) == 0.5)
        #expect(OrderPriceMatcher.confidence(0.5) == .low)
        #expect(OrderPriceMatcher.confidence(0.7) == .medium)
        #expect(OrderPriceMatcher.confidence(0.9) == .high)
    }
}

struct OrderImportDraftTests {
    private func lines() throws -> [PriceableLine] {
        let json = CostFixtures.handoffJSON(
            id: "handoff-1",
            lines: [
                CostFixtures.pricedLineJSON(
                    id: "l1", key: "i-sour-cream", name: "Sour Cream", displayName: "Sour cream", priceCents: nil,
                    pantry: nil, status: "pending"),
                CostFixtures.pricedLineJSON(
                    id: "l2", key: "i-soy", name: "Soy Sauce", displayName: "Soy sauce", priceCents: nil, pantry: nil,
                    status: "pending"),
                CostFixtures.pricedLineJSON(
                    id: "l3", key: "i-rice", name: "Rice", displayName: "Test rice", priceCents: nil, pantry: nil,
                    status: "pending"),
            ])
        let handoff = try JSONCoding.makeDecoder().decode(ShoppingHandoff.self, from: Data(json.utf8))
        return handoff.lines.map { PriceableLine(handoffID: handoff.id, line: $0) }
    }

    private func id(_ lineID: String) -> PriceableLine.ID {
        PriceableLine.ID(handoffID: "handoff-1", lineID: lineID)
    }

    @Test func reviewStartsFromTheMatchesAndSavesWhatsAssigned() throws {
        let order = OrderScreenshotParser.parse(text: CostFixtures.sameRowText)
        var draft = OrderImportDraft(order: order, lines: try lines())

        #expect(draft.matchedRows.map(\.lineID) == [id("l1"), id("l2")])
        #expect(draft.unmatchedRows.isEmpty)
        #expect(draft.linesWithoutPrice.map(\.id) == [id("l3")])
        #expect(draft.detectedTotalCents == 844)
        #expect(draft.savesTotal)
        #expect(draft.prices == [id("l1"): .some(248), id("l2"): .some(596)])

        // Moving the soy sauce's price to the rice lets the rice go from "without a price".
        draft.assign(1, to: id("l3"))
        #expect(draft.prices == [id("l1"): .some(248), id("l3"): .some(596)])
        #expect(draft.matchedRows.last?.confidence == nil)
        #expect(draft.linesWithoutPrice.map(\.id) == [id("l2")])

        // A line prices one item: giving it to another item frees the first.
        draft.assign(0, to: id("l3"))
        #expect(draft.rows.first { $0.id == 1 }?.lineID == nil)
        #expect(draft.prices == [id("l3"): .some(248)])

        draft.setPriceText("oops", for: 0)
        #expect(draft.hasInvalidPrice)
        #expect(!draft.canSave)

        draft.assign(0, to: nil)
        draft.savesTotal = false
        #expect(draft.prices.isEmpty)
        #expect(!draft.canSave)
    }

    @Test func modelItemsAreKeptOnlyWhenTheirPricesAreInTheText() {
        let lines = CostFixtures.sameRowText.components(separatedBy: .newlines)
        let parsed = OrderScreenshotParser.parse(lines: lines)

        let merged = OrderImportMerge.merge(
            parsed: parsed,
            modelItems: [
                ModelOrderItem(name: "Great Value Sour Cream", price: "$2.48", quantity: 1),
                ModelOrderItem(name: "Harborline Soy Sauce", price: "5.96", quantity: 2),
                ModelOrderItem(name: "Invented Pickles", price: "$9.99", quantity: 1),
            ],
            modelTotal: "$99.00", lines: lines)

        #expect(merged.items.map(\.name) == ["Great Value Sour Cream", "Harborline Soy Sauce"])
        #expect(merged.items.map(\.priceCents) == [248, 596])
        #expect(merged.totalCents == 844)

        // Fewer checked model items than the parser found: the parser's reading stands.
        let fallback = OrderImportMerge.merge(
            parsed: parsed, modelItems: [ModelOrderItem(name: "Sour Cream", price: "$2.48", quantity: 1)],
            modelTotal: nil, lines: lines)
        #expect(fallback.items == parsed.items)
    }
}
