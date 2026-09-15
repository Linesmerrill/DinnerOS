import Foundation

/// Who set an item's status (`PantryStatusSource` in `api/openapi.yaml`). A value from a newer
/// server keeps its raw value and reads as neither case.
nonisolated struct PantryStatusSource: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let person = PantryStatusSource(rawValue: "person")
    /// The usage estimate marked the item low.
    static let estimate = PantryStatusSource(rawValue: "estimate")
}

/// Whether the threshold in effect is the item's own or the household's.
nonisolated struct PantryThresholdSource: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let item = PantryThresholdSource(rawValue: "item")
    static let household = PantryThresholdSource(rawValue: "household")
}

/// An exact amount with a number for display (`PantryAmount`).
nonisolated struct PantryAmount: Decodable, Hashable, Sendable {
    /// Exact, as `"n"` or `"n/d"`.
    let quantity: String
    let quantityValue: Double
}

/// One discrete unit's size: one `per` holds `quantity` `unit` ("1 package = 8 oz").
nonisolated struct PantryUnitSize: Decodable, Hashable, Sendable {
    let per: String
    let quantity: String
    let quantityValue: Double
    let unit: String
}

/// Cooked recipes deducted in the current cycle.
nonisolated struct PantryRecipeUse: Decodable, Hashable, Sendable {
    let count: Int
    let quantity: String
    let quantityValue: Double
}

/// Learned non-recipe use per day.
nonisolated struct PantryDailyRate: Decodable, Hashable, Sendable {
    let quantity: String
    let quantityValue: Double
    /// How many earlier usage periods the rate is based on.
    let basedOnSegments: Int
}

/// What's estimated to be left of an item (`PantryEstimate`; docs/pantry-usage.md). Every
/// amount is in `unit`.
nonisolated struct PantryEstimate: Decodable, Hashable, Sendable {
    let cycleID: String
    /// `grocery_list`, `manual`, `provider`, or `edit`.
    let cycleSource: String
    let cycleStartedAt: Date
    /// When a person last corrected the amount in this cycle.
    let adjustedAt: Date?
    let unit: String
    let startAmount: PantryAmount
    let remaining: PantryAmount
    let percentRemaining: Int
    let percentUsed: Int
    let recipeUse: PantryRecipeUse
    let otherUse: PantryAmount
    /// `nil` until at least two usage periods are known.
    let dailyRate: PantryDailyRate?
    /// Cooked recipes that used the item but couldn't be deducted.
    let skippedRecipes: Int
    /// The threshold in effect, as percent used.
    let lowThresholdPercent: Int
    let thresholdSource: PantryThresholdSource
    let belowThreshold: Bool
    /// English text from the API, for example "About 31% left: 2 recipes used 6 tbsp."
    let summary: String
    let estimatedAt: Date

    private enum CodingKeys: String, CodingKey {
        case cycleID = "cycleId"
        case cycleSource, cycleStartedAt, adjustedAt, unit, startAmount, remaining, percentRemaining, percentUsed,
            recipeUse, otherUse, dailyRate, skippedRecipes, lowThresholdPercent, thresholdSource, belowThreshold,
            summary, estimatedAt
    }
}

/// How a `PATCH` changes an item's own threshold.
nonisolated enum PantryThresholdChange: Equatable, Sendable {
    /// Sends `null`: the item uses the household's threshold again.
    case household
    /// The item's own threshold, as percent used (1–100).
    case percent(Int)
}

// MARK: - Purchases

/// Where a purchase came from. Apps send `groceryList` or `manual`.
nonisolated struct PantryPurchaseSource: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let groceryList = PantryPurchaseSource(rawValue: "grocery_list")
    static let manual = PantryPurchaseSource(rawValue: "manual")
    static let provider = PantryPurchaseSource(rawValue: "provider")
}

/// How much one purchased discrete unit holds, in a volume or weight unit.
nonisolated struct PantryUnitSizeInput: Encodable, Equatable, Sendable {
    var quantity: String
    var unit: String
}

/// The body of `POST .../pantry/purchases`. `nil` fields are omitted. Send `itemID`, or
/// `ingredientID` or `name`, never both.
nonisolated struct NewPantryPurchase: Encodable, Equatable, Sendable {
    var itemID: String?
    var ingredientID: String?
    var name: String?
    var source: PantryPurchaseSource
    /// Exact, as produced by `PantryQuantity.parse`. Without it the item isn't tracked.
    var quantity: String?
    var unit: String?
    var unitSize: PantryUnitSizeInput?
    /// The grocery list's ISO week, for `groceryList` purchases.
    var week: String?
    /// The same ID on every attempt, so a retry after a lost response records one purchase.
    var clientPurchaseID: String

    private enum CodingKeys: String, CodingKey {
        case itemID = "itemId"
        case ingredientID = "ingredientId"
        case name, source, quantity, unit, unitSize, week
        case clientPurchaseID = "clientPurchaseId"
    }
}

/// A recorded purchase (`PantryPurchase`).
nonisolated struct PantryPurchase: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let householdID: String
    let itemID: String
    let source: PantryPurchaseSource
    let quantity: String?
    let quantityValue: Double?
    let unit: String?
    let unitSize: PantryUnitSize?
    let week: String?
    let clientPurchaseID: String?
    let recordedBy: String
    let purchasedAt: Date

    private enum CodingKeys: String, CodingKey {
        case id
        case householdID = "householdId"
        case itemID = "itemId"
        case source, quantity, quantityValue, unit, unitSize, week
        case clientPurchaseID = "clientPurchaseId"
        case recordedBy, purchasedAt
    }
}

/// Response to `POST .../pantry/purchases`.
nonisolated struct PantryPurchaseResponse: Decodable, Hashable, Sendable {
    let purchase: PantryPurchase
    /// The item as it is after the purchase.
    let item: PantryItem
}

/// Response to `GET .../pantry/{itemId}/purchases`.
nonisolated struct PantryPurchaseListResponse: Decodable, Equatable, Sendable {
    /// Newest first, at most 20.
    let items: [PantryPurchase]
}

// MARK: - Settings

/// The household's pantry settings (`PantrySettings`).
nonisolated struct PantrySettings: Decodable, Hashable, Sendable {
    static let defaultLowThresholdPercent = 80
    static let thresholdRange = 1...100

    /// Percent of an item's starting amount used before the estimate marks it low.
    let lowThresholdPercent: Int
    let defaultLowThresholdPercent: Int
    /// `nil` until someone changes the defaults.
    let updatedBy: String?
    let updatedAt: Date?

    init(
        lowThresholdPercent: Int, defaultLowThresholdPercent: Int = Self.defaultLowThresholdPercent,
        updatedBy: String? = nil, updatedAt: Date? = nil
    ) {
        self.lowThresholdPercent = lowThresholdPercent
        self.defaultLowThresholdPercent = defaultLowThresholdPercent
        self.updatedBy = updatedBy
        self.updatedAt = updatedAt
    }

    private enum CodingKeys: String, CodingKey {
        case lowThresholdPercent, defaultLowThresholdPercent, updatedBy, updatedAt
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let fallback =
            try container.decodeIfPresent(Int.self, forKey: .defaultLowThresholdPercent)
            ?? Self.defaultLowThresholdPercent
        self.init(
            lowThresholdPercent: try container.decodeIfPresent(Int.self, forKey: .lowThresholdPercent) ?? fallback,
            defaultLowThresholdPercent: fallback,
            updatedBy: try container.decodeIfPresent(String.self, forKey: .updatedBy),
            updatedAt: try container.decodeIfPresent(Date.self, forKey: .updatedAt))
    }
}

/// The body of `PUT .../pantry/settings`.
nonisolated struct PantrySettingsUpdate: Encodable, Equatable, Sendable {
    let lowThresholdPercent: Int
}
