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
}
