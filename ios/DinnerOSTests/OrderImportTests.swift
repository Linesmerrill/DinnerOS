import CoreGraphics
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

    @Test func readsAWalmartCartWithPricesAboveNames() {
        let rows = OrderTextLayout.rows(CostFixtures.cartPieces())

        let order = OrderScreenshotParser.parse(rows: rows)

        #expect(order.items.map(\.name) == CostFixtures.cartItems.map(\.name))
        #expect(order.items.map(\.priceCents) == CostFixtures.cartItems.map(\.cents))
        #expect(order.items.map(\.quantity) == CostFixtures.cartItems.map(\.quantity))
        #expect(order.totalCents == 2_459)
        // The card whose price scrolled off the top of the screenshot has no price to save.
        #expect(!order.items.contains { $0.name.contains("Tortillas") })
    }

    /// Build 31 bound the price that *followed* a name to it, so every line took the next item's
    /// price and the last had none. The layout, not the reading order, says which side to look.
    @Test func cartPricesDontShiftOntoTheNextItem() {
        let rows = OrderTextLayout.rows(CostFixtures.cartPieces())

        let order = OrderScreenshotParser.parse(rows: rows)

        let byName = Dictionary(order.items.map { ($0.name, $0.priceCents) }, uniquingKeysWith: { first, _ in first })
        #expect(byName["Fresh Zucchini, Each"] == 126)
        #expect(byName["Fresh Whole Yellow Onion, Each"] == 132)
        #expect(byName["Sample Brand Garlic Breadsticks, 11.25 oz"] == 312)
        // The prices each of those took in build 31: the item below's.
        #expect(byName["Fresh Zucchini, Each"] != 268)
        #expect(byName["Fresh Whole Yellow Onion, Each"] != 296)
        #expect(order.items.count == CostFixtures.cartItems.count)
        let tokens = rows.map { OrderScreenshotParser.classify($0.text) }
        #expect(OrderScreenshotParser.pricesComeFirst(tokens: tokens, rows: rows))
    }

    @Test func cartPricesAreCentsNotWholeDollars() {
        let order = OrderScreenshotParser.parse(rows: OrderTextLayout.rows(CostFixtures.cartPieces()))
        let locale = Locale(identifier: "en_US")

        // "$1" with a small raised "26" is $1.26, not $126.00.
        #expect(order.items.map(\.priceCents).allSatisfy { $0 < 1_000 })
        #expect(order.items.map { MoneyText.editingText($0.priceCents) }.first == "1.26")
        #expect(order.items.map { MoneyText.format($0.priceCents, locale: locale) }.first == "$1.26")
        #expect(MoneyText.format(order.totalCents ?? 0, locale: locale) == "$24.59")
    }

    /// Build 33 read a real cart's "$244" as $244.00, so every row showed cents times a hundred.
    @Test func joinedRaisedCentsAreReadAsCents() {
        let rows = OrderTextLayout.rows(CostFixtures.realCartPieces())

        let order = OrderScreenshotParser.parse(rows: rows)

        #expect(OrderScreenshotParser.priceStyle(rows: rows) == .raisedCents)
        #expect(order.items.map(\.name) == CostFixtures.realCartItems.map(\.name))
        #expect(order.items.map(\.priceCents) == CostFixtures.realCartItems.map(\.cents))
        #expect(order.items.map(\.quantity) == CostFixtures.realCartItems.map(\.quantity))
        #expect(order.totalCents == 13_127)
        // What build 33 showed instead: the cents read as whole dollars.
        #expect(!order.items.map(\.priceCents).contains { $0 % 100 == 0 && $0 >= 5_000 })
        let locale = Locale(identifier: "en_US")
        #expect(
            order.items.map { MoneyText.format($0.priceCents, locale: locale) }.prefix(3) == [
                "$2.44", "$1.98", "$1.37",
            ])
        #expect(MoneyText.format(order.totalCents ?? 0, locale: locale) == "$131.27")
    }

    @Test func itemPricesHaveToFitInsideTheOrderTotal() {
        let rows = OrderTextLayout.rows(CostFixtures.realCartPieces())

        let order = OrderScreenshotParser.parse(rows: rows)

        // The screenshots show 17 of 24 items, so they come to part of the total, never more.
        let spent = order.items.reduce(0) { $0 + $1.priceCents }
        #expect(spent == 3_620)
        #expect(order.itemsFitTotal == true)
        // Read as whole dollars the same screen is a hundred times over the total.
        #expect(OrderScreenshotParser.parse(rows: rows, style: .decimal).itemsFitTotal == false)
        #expect(ParsedOrder().itemsFitTotal == nil)
    }

    /// One bare amount isn't a pattern, so the order's own total is what settles it.
    @Test func aLoneBareAmountIsSettledByTheTotal() {
        let rows = [
            RecognizedRow(text: "$298", rect: CGRect(x: 0.3, y: 0.1, width: 0.1, height: 0.02)),
            RecognizedRow(
                text: "Sample Brand Oat Milk, 64 fl oz", rect: CGRect(x: 0.3, y: 0.13, width: 0.5, height: 0.02)),
            RecognizedRow(text: "Estimated total $3.50", rect: CGRect(x: 0.3, y: 0.2, width: 0.5, height: 0.02)),
        ]

        #expect(OrderScreenshotParser.priceStyle(rows: rows) == .raisedCents)
        #expect(OrderScreenshotParser.parse(rows: rows).items.map(\.priceCents) == [298])
        // An order screen that writes its prices in full keeps reading them in full.
        #expect(OrderScreenshotParser.priceStyle(rows: [RecognizedRow(text: "Total $130.42")]) == .decimal)
        #expect(OrderScreenshotParser.parse(text: CostFixtures.orderDetailsText).items.map(\.priceCents).first == 248)
    }

    @Test func wordsReadOffTheProductPhotoArentPartOfTheName() {
        let pieces = CostFixtures.realCartPieces()

        let names = OrderScreenshotParser.parse(rows: OrderTextLayout.rows(pieces)).items.map(\.name)

        // The tub's own printing and the bag's label sit in the photo column, left of the text.
        #expect(names.contains("Great Value All Natural Sour Cream, 8 oz"))
        #expect(names.contains("Harborline Fresh Whole Shallots, 16 oz Bag"))
        #expect(!names.contains { $0.contains("Original") || $0.lowercased().hasSuffix("shallots") })
        #expect(OrderTextLayout.withoutProductPhotos(pieces).count < pieces.count)
        // A screen whose text is one column, with no room for photos beside it, is left alone.
        let column = (0..<6).map {
            RecognizedPiece("row \($0)", CGRect(x: 0.04, y: 0.1 * Double($0), width: 0.5, height: 0.02))
        }
        #expect(OrderTextLayout.withoutProductPhotos(column).count == column.count)
    }

    /// A real order split "Philadelphia Cream Cheese Spread, 1 Tub, for" from the rest of its
    /// title: the first line had no price and was dropped, and "Smooth, Spreadable Mornings,
    /// Original, No Arti…" took the $3.97 and matched nothing. The wrapped lines are one title.
    @Test(arguments: [CostFixtures.creamCheeseTwoLines, CostFixtures.creamCheeseThreeLines])
    func wrappedTitlesAreOneName(creamCheese: [(text: String, width: CGFloat)]) {
        let rows = OrderTextLayout.rows(CostFixtures.orderDetailsPieces(creamCheese: creamCheese))

        let order = OrderScreenshotParser.parse(rows: rows)

        let expected = CostFixtures.orderDetailsItems(creamCheese: creamCheese)
        #expect(order.items.map(\.name) == expected.map(\.name))
        #expect(order.items.map(\.priceCents) == expected.map(\.cents))
        #expect(order.items.map(\.quantity) == expected.map(\.quantity))
        #expect(order.totalCents == 1_021)
        // No half of the title stands on its own.
        #expect(!order.items.contains { $0.name.hasPrefix("Smooth") || $0.name.hasPrefix("Mornings") })
        // The whole title matches the week's cream cheese line.
        let items = order.items.filter { $0.priceCents == 397 }
        #expect(OrderPriceMatcher.score(OrderPriceMatcher.tokens(items.first?.name ?? ""), ["cream", "cheese"]) >= 0.5)
    }

    /// The looser line spacing mustn't glue the next product's title onto a short one.
    @Test func shortTitlesNextToEachOtherStayApart() {
        let line = CGRect(x: 0.27, y: 0.1, width: 0.64, height: 0.009)
        let below = CGRect(x: 0.27, y: 0.122, width: 0.6, height: 0.009)
        let short = CGRect(x: 0.27, y: 0.1, width: 0.24, height: 0.009)
        let nextCard = CGRect(x: 0.27, y: 0.23, width: 0.3, height: 0.009)
        let continues = OrderScreenshotParser.continuesName

        // A full line with the next right under it, even with a gap wider than the boxes.
        #expect(continues("Testfield Farms Cream Cheese Spread, 1 Tub, for", line, below, 0.66))
        // A short title ended where it stopped, however close the next title is.
        #expect(!continues("Fresh Lime, Each", short, below, 0.66))
        // The next card's title is a photo's height away.
        #expect(!continues("Testfield Farms Cream Cheese Spread, 1 Tub, for", line, nextCard, 0.66))
        // Walmart's "…" ends a title.
        #expect(!continues("Smooth, Spreadable Mornings, Original, No Arti…", line, below, 0.66))
        #expect(!continues("Smooth, Spreadable Mornings, Original, No Arti...", .null, .null, 0))
        // Starting at another edge, it's not the same title.
        #expect(
            !continues("Testfield Farms Cream Cheese Spread, 1 Tub, for", line, below.offsetBy(dx: 0.1, dy: 0), 0.66))
        // Without a layout, reading order is all there is.
        #expect(continues("Great Value Light Brown Sugar,", .null, .null, 0))

        // Two short products on neighbouring cards keep their own names and prices.
        let order = OrderScreenshotParser.parse(
            rows: OrderTextLayout.rows(CostFixtures.orderDetailsPieces(creamCheese: CostFixtures.creamCheeseTwoLines)))
        let byName = Dictionary(order.items.map { ($0.name, $0.priceCents) }, uniquingKeysWith: { first, _ in first })
        #expect(byName["Fresh Lime, Each"] == 50)
        #expect(byName["Fresh Cilantro, Bunch"] == 93)
    }

    /// Each item knows which screenshot it came from and where, so review can show the member
    /// their own photo and title for a name the app cut off.
    @Test func itemsKnowWhereOnTheScreenshotTheyWereRead() throws {
        let first = OrderTextLayout.rows(CostFixtures.orderDetailsPieces(creamCheese: CostFixtures.creamCheeseTwoLines))
        // The second screenshot repeats the cream cheese, wrapped over three lines this time, and
        // goes on to an item of its own.
        var next = CostFixtures.OrderDetailsScreen()
        next.card(title: CostFixtures.creamCheeseThreeLines, price: 397)
        next.card(title: [("Fresh Ginger Root, Each", 0.3)], price: 197)
        let second = OrderTextLayout.rows(next.pieces).map { $0.movedDown(byScreens: 1) }
        let order = OrderScreenshotParser.parse(rows: first + second)

        // The repeat is read once, from where it was first seen.
        #expect(order.items.count(where: { $0.priceCents == 397 }) == 1)
        let creamCheese = try #require(order.items.first { $0.priceCents == 397 })
        let source = try #require(creamCheese.source)
        #expect(source.screen == 0)
        // Both title lines and the price under them.
        let column = CostFixtures.OrderDetailsScreen.column
        #expect(abs(source.rect.minX - column) < 0.001)
        #expect(source.rect.height > CostFixtures.OrderDetailsScreen.linePitch * 2)
        let ginger = try #require(order.items.first { $0.name == "Fresh Ginger Root, Each" })
        #expect(ginger.source?.screen == 1)
        #expect((ginger.source?.rect.maxY ?? 2) < 1)

        // The crop is the card's full width, the rows padded, and at least as tall as the photo.
        let size = CGSize(width: 1179, height: 2556)
        let crop = source.cropRect(in: size)
        #expect(crop.minX == 0 && crop.width == size.width)
        #expect(crop.minY <= source.rect.minY * size.height)
        #expect(crop.maxY >= source.rect.maxY * size.height)
        #expect(crop.height >= column * size.width)
        #expect(crop.height < size.height / 4)
        // Near the top of the screenshot it stays inside the image.
        let top = OrderItemSource(screen: 0, rect: CGRect(x: 0.27, y: 0, width: 0.6, height: 0.02))
        #expect(top.cropRect(in: size).minY == 0)
        #expect(OrderItemSource.stacked(.null, anchor: .null) == nil)
        // Plain text has nowhere to point.
        #expect(OrderScreenshotParser.parse(text: CostFixtures.orderDetailsText).items.allSatisfy { $0.source == nil })
    }

    @Test func raisedCentsJoinOntoTheirDollars() {
        let dollars = RecognizedPiece("$2", CGRect(x: 0.2, y: 0.1, width: 0.05, height: 0.02))
        let cents = RecognizedPiece("68", CGRect(x: 0.255, y: 0.095, width: 0.03, height: 0.012))
        let unitPrice = RecognizedPiece("16.8¢/oz", CGRect(x: 0.2, y: 0.14, width: 0.1, height: 0.012))

        #expect(OrderTextLayout.isRaisedCents(after: dollars, cents))
        #expect(OrderTextLayout.rows([cents, dollars, unitPrice]).map(\.text) == ["$2.68", "16.8¢/oz"])
        #expect(OrderScreenshotParser.classify("$2.68") == .price(268))
        // Same size and on the same baseline: two separate readings, not one price.
        let sameSize = RecognizedPiece("68", CGRect(x: 0.255, y: 0.1, width: 0.03, height: 0.02))
        #expect(!OrderTextLayout.isRaisedCents(after: dollars, sameSize))
        // And when they arrive as one string anyway, the space still reads as a decimal point.
        #expect(OrderScreenshotParser.classify("$2 68") == .price(268))
    }

    @Test(arguments: [
        (
            "Fresh Zucchini, Each Subscribe - SNAP EBT eligible Free 90-day returns Remove Save for later 2 +",
            "Fresh Zucchini, Each"
        ),
        (
            "88¢/lb | Final cost by weight Fresh Whole Yellow Onion, Each Subscribe - SNAP EBT",
            "Fresh Whole Yellow Onion, Each"
        ),
        ("Sample Brand Fresh Shallots, 16 oz Best seller", "Sample Brand Fresh Shallots, 16 oz"),
        ("$1.12/lb Testfield Farms Ground Beef 80/20", "Testfield Farms Ground Beef 80/20"),
        ("Great Value Sour Cream, 16 oz", "Great Value Sour Cream, 16 oz"),
    ])
    func stripsTheAppsChromeFromTitles(line: String, title: String) {
        #expect(OrderTitleCleaner.clean(line) == title)
    }

    @Test(arguments: [
        "Subscribe", "SNAP EBT eligible", "Free 90-day returns", "Remove", "Save for later", "Best seller",
        "10K+ bought since yesterday", "Bought 5 times", "Multipack Quantity", "Count Per Pack", "88¢/lb",
        "88¢/lb | Final cost by weight", "Gift eligible", "Sponsored",
    ])
    func rowsThatAreOnlyChromeArentTitles(line: String) {
        #expect(OrderTitleCleaner.clean(line) == nil)
        #expect(OrderScreenshotParser.classify(line) != .name(line, trailingPrice: nil))
    }

    @Test(arguments: [
        ("$4.98", OrderScreenshotParser.Token.price(498)),
        ("avg $0.63 ea", .ignoredPrice),
        ("You save $0.25", .ignoredPrice),
        ("- 2 +", .quantity(2, price: nil)),
        ("Estimated total $24.59", .summary(.total, amount: 2_459)),
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
