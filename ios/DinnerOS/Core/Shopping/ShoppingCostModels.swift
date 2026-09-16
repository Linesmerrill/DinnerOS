import Foundation

// Week cost and meal kit savings (docs/api.md#shopping). Money is integer cents, in US dollars.

/// Where a week's spend comes from. Unknown values decode as-is.
nonisolated struct WeekSpendSource: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// The order total a member entered, fees and tax included.
    static let orderTotal = WeekSpendSource(rawValue: "order_total")
    /// The sum of item prices.
    static let itemPrices = WeekSpendSource(rawValue: "item_prices")
}

/// How much of an item the week used. Unknown values decode as-is.
nonisolated struct WeekCostUsage: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Measured against the package size.
    static let measured = WeekCostUsage(rawValue: "measured")
    /// A fresh or per-week item counted as used in full.
    static let wholePackage = WeekCostUsage(rawValue: "whole_package")
    static let unknown = WeekCostUsage(rawValue: "unknown")
}

/// A household's meal kit spend to compare against (`MealKitBaseline`).
nonisolated struct MealKitBaseline: Decodable, Hashable, Sendable {
    let weeklyCents: Int
    let meals: Int
    /// Sent by cost responses; worked out when a household response leaves it out.
    let perMealCents: Int

    init(weeklyCents: Int, meals: Int, perMealCents: Int? = nil) {
        self.weeklyCents = weeklyCents
        self.meals = meals
        self.perMealCents = perMealCents ?? (meals > 0 ? Int((Double(weeklyCents) / Double(meals)).rounded()) : 0)
    }

    private enum CodingKeys: String, CodingKey {
        case weeklyCents, meals, perMealCents
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            weeklyCents: try container.decode(Int.self, forKey: .weeklyCents),
            meals: try container.decode(Int.self, forKey: .meals),
            perMealCents: try container.decodeIfPresent(Int.self, forKey: .perMealCents))
    }
}

/// One bought item in a week's cost (`WeekCostItem`).
nonisolated struct WeekCostItem: Decodable, Hashable, Sendable, Identifiable {
    let handoffID: String
    let lineID: String
    let ingredientKey: String
    let name: String
    let priceCents: Int?
    let usedCents: Int?
    let stockedCents: Int?
    let pantry: ShoppingLinePantry
    let usage: WeekCostUsage

    var id: String { "\(handoffID)|\(lineID)" }

    private enum CodingKeys: String, CodingKey {
        case handoffID = "handoffId"
        case lineID = "lineId"
        case ingredientKey, name, priceCents, usedCents, stockedCents, pantry, usage
    }

    init(
        handoffID: String, lineID: String, ingredientKey: String, name: String, priceCents: Int?, usedCents: Int?,
        stockedCents: Int?, pantry: ShoppingLinePantry, usage: WeekCostUsage
    ) {
        self.handoffID = handoffID
        self.lineID = lineID
        self.ingredientKey = ingredientKey
        self.name = name
        self.priceCents = priceCents
        self.usedCents = usedCents
        self.stockedCents = stockedCents
        self.pantry = pantry
        self.usage = usage
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        handoffID = try container.decode(String.self, forKey: .handoffID)
        lineID = try container.decode(String.self, forKey: .lineID)
        ingredientKey = try container.decodeIfPresent(String.self, forKey: .ingredientKey) ?? ""
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        priceCents = try container.decodeIfPresent(Int.self, forKey: .priceCents)
        usedCents = try container.decodeIfPresent(Int.self, forKey: .usedCents)
        stockedCents = try container.decodeIfPresent(Int.self, forKey: .stockedCents)
        pantry = try container.decodeIfPresent(ShoppingLinePantry.self, forKey: .pantry) ?? .tracked
        usage = try container.decodeIfPresent(WeekCostUsage.self, forKey: .usage) ?? .unknown
    }
}

/// What a week's groceries cost, and how that compares with a meal kit (`WeekCost`).
nonisolated struct WeekCost: Decodable, Hashable, Sendable {
    let week: String
    let currency: String
    let orderTotalCents: Int?
    let spentCents: Int?
    let spentSource: WeekSpendSource?
    let itemsBought: Int
    let itemsPriced: Int
    let usedCents: Int?
    let stockedCents: Int?
    let earlierStockUsedCents: Int
    let feesAndUnpricedCents: Int
    let meals: Int
    let costPerMealCents: Int?
    let mealKit: MealKitBaseline?
    /// Negative when groceries cost more than the meal kit.
    let savedCents: Int?
    let partial: Bool
    /// Ready to display, for example "Based on 18 of 24 items with prices." May be empty.
    let summary: String
    let items: [WeekCostItem]

    /// Something worth a card: an order, a total, or a price.
    var hasContent: Bool {
        itemsBought > 0 || orderTotalCents != nil || spentCents != nil
    }

    private enum CodingKeys: String, CodingKey {
        case week, currency, orderTotalCents, spentCents, spentSource, itemsBought, itemsPriced, usedCents,
            stockedCents, earlierStockUsedCents, feesAndUnpricedCents, meals, costPerMealCents, mealKit, savedCents,
            partial, summary, items
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        week = try container.decode(String.self, forKey: .week)
        currency = try container.decodeIfPresent(String.self, forKey: .currency) ?? "USD"
        orderTotalCents = try container.decodeIfPresent(Int.self, forKey: .orderTotalCents)
        spentCents = try container.decodeIfPresent(Int.self, forKey: .spentCents)
        spentSource = try container.decodeIfPresent(WeekSpendSource.self, forKey: .spentSource)
        itemsBought = try container.decodeIfPresent(Int.self, forKey: .itemsBought) ?? 0
        itemsPriced = try container.decodeIfPresent(Int.self, forKey: .itemsPriced) ?? 0
        usedCents = try container.decodeIfPresent(Int.self, forKey: .usedCents)
        stockedCents = try container.decodeIfPresent(Int.self, forKey: .stockedCents)
        earlierStockUsedCents = try container.decodeIfPresent(Int.self, forKey: .earlierStockUsedCents) ?? 0
        feesAndUnpricedCents = try container.decodeIfPresent(Int.self, forKey: .feesAndUnpricedCents) ?? 0
        meals = try container.decodeIfPresent(Int.self, forKey: .meals) ?? 0
        costPerMealCents = try container.decodeIfPresent(Int.self, forKey: .costPerMealCents)
        mealKit = try container.decodeIfPresent(MealKitBaseline.self, forKey: .mealKit)
        savedCents = try container.decodeIfPresent(Int.self, forKey: .savedCents)
        partial = try container.decodeIfPresent(Bool.self, forKey: .partial) ?? false
        summary = try container.decodeIfPresent(String.self, forKey: .summary) ?? ""
        items = try container.decodeIfPresent([WeekCostItem].self, forKey: .items) ?? []
    }
}

/// The body of `PUT .../shopping/weeks/{week}/spend`. A `nil` total is sent as `null`, which
/// clears it.
nonisolated struct SetWeekSpendRequest: Encodable, Equatable, Sendable {
    var orderTotalCents: Int?

    private enum CodingKeys: String, CodingKey {
        case orderTotalCents
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeNullable(orderTotalCents, forKey: .orderTotalCents)
    }
}

/// One week of `GET .../shopping/savings`.
nonisolated struct SavingsWeek: Decodable, Hashable, Sendable, Identifiable {
    let week: String
    let spentCents: Int?
    let usedCents: Int?
    let stockedCents: Int?
    let meals: Int
    let costPerMealCents: Int?
    let savedCents: Int?
    let itemsBought: Int
    let itemsPriced: Int

    var id: String { week }

    private enum CodingKeys: String, CodingKey {
        case week, spentCents, usedCents, stockedCents, meals, costPerMealCents, savedCents, itemsBought, itemsPriced
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        week = try container.decode(String.self, forKey: .week)
        spentCents = try container.decodeIfPresent(Int.self, forKey: .spentCents)
        usedCents = try container.decodeIfPresent(Int.self, forKey: .usedCents)
        stockedCents = try container.decodeIfPresent(Int.self, forKey: .stockedCents)
        meals = try container.decodeIfPresent(Int.self, forKey: .meals) ?? 0
        costPerMealCents = try container.decodeIfPresent(Int.self, forKey: .costPerMealCents)
        savedCents = try container.decodeIfPresent(Int.self, forKey: .savedCents)
        itemsBought = try container.decodeIfPresent(Int.self, forKey: .itemsBought) ?? 0
        itemsPriced = try container.decodeIfPresent(Int.self, forKey: .itemsPriced) ?? 0
    }
}

/// Response to `GET .../shopping/savings`: recent weeks, newest first.
nonisolated struct ShoppingSavings: Decodable, Hashable, Sendable {
    let weeks: [SavingsWeek]
    let totalSavedCents: Int?
    let weeksCounted: Int
    let mealKit: MealKitBaseline?

    private enum CodingKeys: String, CodingKey {
        case weeks, totalSavedCents, weeksCounted, mealKit
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        weeks = try container.decodeIfPresent([SavingsWeek].self, forKey: .weeks) ?? []
        totalSavedCents = try container.decodeIfPresent(Int.self, forKey: .totalSavedCents)
        weeksCounted = try container.decodeIfPresent(Int.self, forKey: .weeksCounted) ?? 0
        mealKit = try container.decodeIfPresent(MealKitBaseline.self, forKey: .mealKit)
    }
}

/// A handed-off line of the shown week that a price can be saved for.
nonisolated struct PriceableLine: Hashable, Sendable, Identifiable {
    nonisolated struct ID: Hashable, Sendable {
        let handoffID: String
        let lineID: String
    }

    let handoffID: String
    let line: ShoppingHandoffLine

    var id: ID { ID(handoffID: handoffID, lineID: line.id) }
}

// MARK: - Formatting

nonisolated enum WeekCostText {
    /// "Saved $68.80 vs meal kit", "$4.00 more than meal kit", or "Same as meal kit".
    static func comparison(savedCents: Int, locale: Locale = .autoupdatingCurrent) -> String {
        if savedCents > 0 {
            return String(localized: "Saved \(MoneyText.format(savedCents, locale: locale)) vs meal kit")
        }
        if savedCents < 0 {
            return String(localized: "\(MoneyText.format(-savedCents, locale: locale)) more than meal kit")
        }
        return String(localized: "Same as meal kit")
    }

    /// "$130.00 for 5 meals".
    static func mealKit(_ kit: MealKitBaseline, locale: Locale = .autoupdatingCurrent) -> String {
        kit.meals == 1
            ? String(localized: "\(MoneyText.format(kit.weeklyCents, locale: locale)) for 1 meal")
            : String(localized: "\(MoneyText.format(kit.weeklyCents, locale: locale)) for \(kit.meals) meals")
    }

    /// A missing amount reads as a dash.
    static func amount(_ cents: Int?, locale: Locale = .autoupdatingCurrent) -> String {
        cents.map { MoneyText.format($0, locale: locale) } ?? "—"
    }
}
