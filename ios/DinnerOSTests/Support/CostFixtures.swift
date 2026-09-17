import CoreGraphics
import Foundation

@testable import DinnerOS

/// Synthetic prices, week cost, and order-screenshot text. Every product, brand, order number,
/// and price here is invented; none comes from a real receipt or screenshot.
nonisolated enum CostFixtures {
    static let weekCostJSON = Data(
        #"""
        {"week":"2026-W38","currency":"USD","orderTotalCents":13042,"spentCents":13042,
         "spentSource":"order_total","itemsBought":24,"itemsPriced":18,"usedCents":6120,"stockedCents":5400,
         "earlierStockUsedCents":310,"feesAndUnpricedCents":1200,"meals":5,"costPerMealCents":1224,
         "mealKit":{"weeklyCents":13000,"meals":5,"perMealCents":2600},"savedCents":6880,"partial":true,
         "summary":"Based on 18 of 24 items with prices.",
         "items":[
           {"handoffId":"handoff-1","lineId":"l1","ingredientKey":"i-sour-cream","name":"Sour Cream",
            "priceCents":248,"usedCents":31,"stockedCents":217,"pantry":"tracked","usage":"measured"},
           {"handoffId":"handoff-1","lineId":"l2","ingredientKey":"i-cilantro","name":"Cilantro",
            "priceCents":null,"usedCents":null,"stockedCents":null,"pantry":"not_tracked","usage":"whole_package"},
           {"handoffId":"handoff-1","lineId":"l3","ingredientKey":"i-rice","name":"Rice",
            "priceCents":399,"usedCents":null,"stockedCents":null,"pantry":"sealed_away","usage":"guessed"}
         ]}
        """#.utf8)

    /// A week with nothing bought and no baseline: every nullable field is null.
    static let emptyWeekCostJSON = Data(
        #"""
        {"week":"2026-W39","currency":"USD","orderTotalCents":null,"spentCents":null,"spentSource":null,
         "itemsBought":0,"itemsPriced":0,"usedCents":null,"stockedCents":null,"earlierStockUsedCents":0,
         "feesAndUnpricedCents":0,"meals":0,"costPerMealCents":null,"mealKit":null,"savedCents":null,
         "partial":false,"summary":"","items":[]}
        """#.utf8)

    static let savingsJSON = Data(
        #"""
        {"weeks":[
           {"week":"2026-W38","spentCents":13042,"usedCents":6120,"stockedCents":5400,"meals":5,
            "costPerMealCents":1224,"savedCents":6880,"itemsBought":24,"itemsPriced":18},
           {"week":"2026-W37","spentCents":null,"usedCents":null,"stockedCents":null,"meals":4,
            "costPerMealCents":null,"savedCents":null,"itemsBought":3,"itemsPriced":0}
         ],
         "totalSavedCents":6880,"weeksCounted":1,"mealKit":{"weeklyCents":13000,"meals":5,"perMealCents":2600}}
        """#.utf8)

    /// A stored handoff line with a price and pantry outcome, or without either (an older API).
    static func pricedLineJSON(
        id: String, key: String, name: String, displayName: String, priceCents: Int?, pantry: String?,
        status: String = "confirmed"
    ) -> String {
        let line = ShoppingFixtures.lineJSON(
            id: id, key: key, name: name, productID: "10000\(id.count)", displayName: displayName, size: nil,
            computed: 1, confirmation: ShoppingFixtures.confirmationJSON(status: status, packages: 1))
        guard priceCents != nil || pantry != nil else { return line }
        let extra =
            #","priceCents":\#(priceCents.map(String.init) ?? "null"),"pantry":\#(ShoppingFixtures.string(pantry))}"#
        return String(line.dropLast()) + extra
    }

    static func handoffJSON(id: String, lines: [String]) -> String {
        ShoppingFixtures.handoffJSON(
            id: id, status: "done", fields: ShoppingFixtures.proposalFields(lines: lines, excluded: [], links: []))
    }

    // MARK: - Order screenshot text

    /// Order details as text recognition reads them: a name, then quantity and price lines,
    /// with a was-price, a per-unit price, a wrapped name, a unit-price line, markers, and the
    /// order's totals at the end.
    static let orderDetailsText = """
        Order details
        Sep 14, 2026 order
        #2000123-45678901
        Delivered on Sep 15
        24 items
        Shopped items (6)
        Great Value Sour Cream, 16 oz
        Qty 1
        $2.48
        Was $2.98
        Harborline Soy Sauce, 10 fl oz
        Qty 2
        $5.96
        $0.30/fl oz
        Fresh Bananas, each
        Weight-adjusted
        Qty 6
        $1.52
        Great Value Light Brown Sugar,
        2 lb Bag
        2 x $2.12
        Testfield Farms Ground Beef 80/20, 1 lb
        Substituted
        $5.47
        You saved $0.50
        Sample Brand Cilantro Bunch
        Unavailable
        $0.78
        Subtotal (24 items) $118.42
        Savings -$3.00
        Delivery fee $9.95
        Bag fee $0.00
        Tax $4.05
        Driver tip $6.00
        Total $130.42
        Visa ending in 1234
        """

    /// The second screenshot overlaps the first: the soy sauce appears again.
    static let overlappingScreenshotText = """
        Harborline Soy Sauce, 10 fl oz
        Qty 2
        $5.96
        Great Value Sour Cream, 16 oz
        Qty 1
        $2.48
        """

    /// Prices above their names, and totals with their amounts on the next line.
    static let pricesFirstText = """
        Items (3)
        $3.97
        Testfield Farms Shredded Cheddar Cheese, 8 oz
        Qty 1
        $6.24
        Sample Brand Flour Tortillas 10 ct
        Qty 2
        $1.18
        Yellow Onions, 3 lb Bag
        Subtotal
        $11.39
        Tax
        $0.80
        Total
        $12.19
        """

    /// Rows as recognition joins them: each name with its price on the same line.
    static let sameRowText = """
        Great Value Sour Cream, 16 oz $2.48
        Qty 1
        Harborline Soy Sauce, 10 fl oz $5.96
        Qty 2 $5.96
        Order total $8.44
        """

    // MARK: - Cart screenshots with a layout

    /// Builds the recognized pieces of a Walmart-cart-style screenshot: a card per item with the
    /// price *above* the name, prices drawn as big dollars with small raised cents, and the app's
    /// chrome under the name. Coordinates are normalized with the origin at the top left, so `y`
    /// grows downward, and they run past 1 the way several stacked screenshots do.
    nonisolated struct CartScreen {
        private(set) var pieces: [RecognizedPiece] = []
        private var y: CGFloat = 0.01
        private let height: CGFloat = 0.008
        private let step: CGFloat = 0.011
        private let column: CGFloat = 0.28

        /// One row of text.
        mutating func text(_ value: String) {
            pieces.append(RecognizedPiece(value, CGRect(x: column, y: y, width: 0.6, height: height)))
            y += step
        }

        /// A word off the product's photo, in the photo's column to the left of the text.
        mutating func photoWord(_ value: String, x: CGFloat = 0.08) {
            pieces.append(RecognizedPiece(value, CGRect(x: x, y: y - step, width: 0.12, height: height)))
        }

        /// A price as recognition usually returns it from a real screenshot: the large dollars
        /// and the small raised cents read as one word, with nothing between them ("$244").
        mutating func joinedPrice(_ cents: Int) {
            text("$\(cents)")
        }

        /// A price the way the app draws it: "$1" in large type with "26" small and raised.
        mutating func price(_ cents: Int) {
            pieces.append(RecognizedPiece("$\(cents / 100)", CGRect(x: column, y: y, width: 0.06, height: height)))
            pieces.append(
                RecognizedPiece(
                    String(format: "%02d", cents % 100),
                    CGRect(x: column + 0.065, y: y - height * 0.25, width: 0.03, height: height * 0.7)))
            y += step
        }

        /// A whole card: the price, anything printed between it and the name, the name, and the
        /// boilerplate every card carries.
        mutating func card(price cents: Int, name: String, quantity: Int = 1, between: [String] = []) {
            price(cents)
            finishCard(name: name, quantity: quantity, between: between)
        }

        /// The same card, with the price recognized as one word the way a real screenshot gives
        /// it, and optional words read off the product's photo beside the name.
        mutating func joinedCard(
            price cents: Int, name: String, quantity: Int = 1, between: [String] = [], photo: [String] = []
        ) {
            joinedPrice(cents)
            finishCard(name: name, quantity: quantity, between: between, photo: photo)
        }

        mutating func finishCard(
            name: String, quantity: Int, between: [String] = [], photo: [String] = []
        ) {
            for line in between {
                text(line)
            }
            text(name)
            for (index, word) in photo.enumerated() {
                photoWord(word, x: 0.06 + CGFloat(index) * 0.07)
            }
            chrome(quantity: quantity)
        }

        /// What every card carries under its name.
        mutating func chrome(quantity: Int = 1) {
            text("Subscribe")
            text("SNAP EBT eligible")
            text("Free 90-day returns")
            text("Remove")
            text("Save for later")
            if quantity > 1 {
                text("- \(quantity) +")
            }
        }
    }

    /// A cart as it reads on an iPhone, prices above names.
    ///
    /// The first screenshot was taken mid-scroll, so it opens with a name whose price was cut off
    /// above it — which is what makes reading order alone bind every price to the wrong item. Two
    /// cards arrive with their chrome joined onto the name, as recognition returns them when the
    /// rows sit close together.
    static func cartPieces() -> [RecognizedPiece] {
        var screen = CartScreen()
        screen.text("Cart (12 items)")
        // The card at the top of the screenshot, its price scrolled off: no price to save.
        screen.text("Sample Brand Flour Tortillas, 10 ct")
        screen.text("Subscribe")
        screen.text("Save for later")
        screen.price(126)
        screen.text("avg $0.63 ea")
        screen.text("Fresh Zucchini, Each Subscribe - SNAP EBT eligible Free 90-day returns Remove Save for later")
        screen.text("- 2 +")
        screen.card(price: 268, name: "Sample Brand Fresh Shallots, 16 oz", between: ["Best seller"])
        screen.card(price: 50, name: "Fresh Lime, Each")
        screen.card(price: 137, name: "Testfield Farms Sour Cream, 8 oz", between: ["Was $1.62", "You save $0.25"])
        screen.card(price: 347, name: "Testfield Farms Cream Cheese, 8 oz", between: ["Rollback"])
        screen.card(price: 208, name: "Sample Brand Shredded Parmesan, 6 oz", between: ["10K+ bought since yesterday"])
        screen.card(price: 113, name: "Fresh Green Onion, Bunch")
        screen.price(132)
        screen.text("88¢/lb | Final cost by weight Fresh Whole Yellow Onion, Each Subscribe - SNAP EBT")
        screen.text("Remove")
        screen.card(price: 296, name: "Fresh Poblano Pepper, 16 oz", between: ["$2.96/lb"])
        screen.card(price: 93, name: "Fresh Cilantro, Bunch")
        screen.card(price: 312, name: "Sample Brand Garlic Breadsticks, 11.25 oz", between: ["Multipack Quantity: 1"])
        screen.card(price: 377, name: "Testfield Farms Ground Cayenne Pepper, 2.25 oz", quantity: 3)
        screen.text("Estimated total $24.59")
        screen.text("Continue to checkout")
        return screen.pieces
    }

    /// A cart the size of a real week's shop, as recognition actually returns one: every price is
    /// one word with no decimal point ("$244" for $2.44, "$64" for $0.64), unit prices and
    /// was-prices sit on the cards, two cards carry words read off the product photos, and the
    /// estimated total at the bottom is the one amount printed with a real decimal point.
    ///
    /// The prices and the layout are a real cart's; the brand names are the invented ones the
    /// other fixtures use.
    static func realCartPieces() -> [RecognizedPiece] {
        var screen = CartScreen()
        screen.text("Cart (24 items)")
        screen.joinedCard(price: 244, name: "Sample Brand Super Soft Flour Tortillas Street Tacos, 12 ct")
        screen.joinedCard(price: 198, name: "Great Value Light Brown Sugar, 32 oz")
        // A name that wraps, with the tub's own printing read off the photo beside it.
        screen.joinedPrice(137)
        screen.text("17.1¢/oz")
        screen.text("Great Value All Natural Sour")
        screen.photoWord("Sour Cream", x: 0.06)
        screen.photoWord("Original", x: 0.15)
        screen.text("Cream, 8 oz")
        screen.chrome()
        screen.joinedCard(price: 208, name: "Great Value Parmesan Finely Shredded, 6 oz Bag", between: ["34.7¢/oz"])
        screen.joinedCard(
            price: 268, name: "Harborline Fresh Whole Shallots, 16 oz Bag", between: ["16.8¢/oz"],
            photo: ["shallots"])
        screen.joinedCard(price: 64, name: "Garlic Bulb Fresh Whole, Each")
        screen.joinedCard(price: 258, name: "Marketside Fresh Green Beans, 12 oz", between: ["21.5¢/oz"])
        screen.joinedCard(price: 397, name: "Fresh Ginger Root, Each", between: ["$3.97 ea", "$3.97/lb"])
        screen.joinedCard(
            price: 126, name: "Fresh Zucchini, Each", quantity: 2,
            between: ["avg $0.63 ea", "$1.25/lb", "Was $0.71 ea", "You save $0.16"])
        screen.joinedCard(price: 50, name: "Fresh Lime, Each", quantity: 2, between: ["$0.25 ea"])
        screen.joinedCard(
            price: 347, name: "Testfield Farms Cream Cheese Spread, 8 oz",
            between: ["Was $3.97", "43.4¢/oz", "You save $0.50"])
        screen.joinedCard(price: 113, name: "Fresh Whole Green Onion, 1 Bunch")
        screen.joinedCard(
            price: 132, name: "Fresh Whole Yellow Onion, Each", quantity: 2, between: ["avg $0.66 ea", "88¢/lb"])
        screen.joinedCard(price: 296, name: "Fresh Poblano Peppers, 16 oz", between: ["18.5¢/oz"])
        screen.joinedCard(price: 93, name: "Fresh Whole Green Cilantro Bunch")
        screen.joinedCard(
            price: 312, name: "Sample Brand Real Garlic Breadsticks, 10.5 oz 6 ct", between: ["29.7¢/oz"])
        screen.joinedCard(price: 377, name: "Great Value Cayenne Pepper, 2.25 oz", between: ["$1.68/oz"])
        screen.text("Was $133.24")
        screen.text("Estimated total $131.27")
        screen.text("Continue to checkout")
        return screen.pieces
    }

    /// Every item in `realCartPieces`, in order, as the review screen should show it.
    static let realCartItems: [(name: String, cents: Int, quantity: Int)] = [
        ("Sample Brand Super Soft Flour Tortillas Street Tacos, 12 ct", 244, 1),
        ("Great Value Light Brown Sugar, 32 oz", 198, 1),
        ("Great Value All Natural Sour Cream, 8 oz", 137, 1),
        ("Great Value Parmesan Finely Shredded, 6 oz Bag", 208, 1),
        ("Harborline Fresh Whole Shallots, 16 oz Bag", 268, 1),
        ("Garlic Bulb Fresh Whole, Each", 64, 1),
        ("Marketside Fresh Green Beans, 12 oz", 258, 1),
        ("Fresh Ginger Root, Each", 397, 1),
        ("Fresh Zucchini, Each", 126, 2),
        ("Fresh Lime, Each", 50, 2),
        ("Testfield Farms Cream Cheese Spread, 8 oz", 347, 1),
        ("Fresh Whole Green Onion, 1 Bunch", 113, 1),
        ("Fresh Whole Yellow Onion, Each", 132, 2),
        ("Fresh Poblano Peppers, 16 oz", 296, 1),
        ("Fresh Whole Green Cilantro Bunch", 93, 1),
        ("Sample Brand Real Garlic Breadsticks, 10.5 oz 6 ct", 312, 1),
        ("Great Value Cayenne Pepper, 2.25 oz", 377, 1),
    ]

    /// Every item in `cartPieces`, in order, with the price printed on its own card.
    static let cartItems: [(name: String, cents: Int, quantity: Int)] = [
        ("Fresh Zucchini, Each", 126, 2),
        ("Sample Brand Fresh Shallots, 16 oz", 268, 1),
        ("Fresh Lime, Each", 50, 1),
        ("Testfield Farms Sour Cream, 8 oz", 137, 1),
        ("Testfield Farms Cream Cheese, 8 oz", 347, 1),
        ("Sample Brand Shredded Parmesan, 6 oz", 208, 1),
        ("Fresh Green Onion, Bunch", 113, 1),
        ("Fresh Whole Yellow Onion, Each", 132, 1),
        ("Fresh Poblano Pepper, 16 oz", 296, 1),
        ("Fresh Cilantro, Bunch", 93, 1),
        ("Sample Brand Garlic Breadsticks, 11.25 oz", 312, 1),
        ("Testfield Farms Ground Cayenne Pepper, 2.25 oz", 377, 3),
    ]
}
