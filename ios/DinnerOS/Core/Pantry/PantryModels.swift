import Foundation

/// How much of an item the household has (`PantryStatus` in `api/openapi.yaml`).
///
/// Grocery lists treat `inStock` as at home, and `low` and `out` as to buy, even for a staple.
nonisolated enum PantryStatus: String, Codable, CaseIterable, Hashable, Sendable, Identifiable {
    case inStock = "in_stock"
    case low
    case out

    var id: String { rawValue }

    var title: String {
        switch self {
        case .inStock: String(localized: "In Stock")
        case .low: String(localized: "Low")
        case .out: String(localized: "Out")
        }
    }

    /// For sentences such as "Marked 3 items as low."
    var phrase: String {
        switch self {
        case .inStock: String(localized: "in stock")
        case .low: String(localized: "low")
        case .out: String(localized: "out")
        }
    }
}

/// An item in a household's pantry (`PantryItem`).
nonisolated struct PantryItem: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let householdID: String
    /// The catalog ingredient; `nil` for free text that matched no catalog ingredient.
    let ingredientID: String?
    /// The normalized ingredient name, unique within the household.
    let key: String
    let displayName: String
    /// A grocery category name such as `produce` (see `PantryCategory`).
    let category: String
    /// Exact, as `"n"` or `"n/d"`. `nil`, with `quantityValue` and `unit`, when no amount
    /// was recorded ("have some").
    let quantity: String?
    /// The same amount as a number, for display only.
    let quantityValue: Double?
    /// A DinnerOS unit code.
    let unit: String?
    let status: PantryStatus
    /// An always-have item such as salt or oil.
    let isStaple: Bool
    /// A calendar date, `YYYY-MM-DD`.
    let expiresOn: String?
    let note: String
    /// The user ID of the last change by a person.
    let updatedBy: String
    let createdAt: Date
    let updatedAt: Date
    /// Who set `status`. Responses from before usage tracking read as `person`.
    let statusSource: PantryStatusSource
    /// The item's own threshold, as percent used; `nil` uses the household's.
    let lowThresholdPercent: Int?
    /// How much one discrete unit holds, remembered from a purchase.
    let unitSize: PantryUnitSize?
    /// What's estimated to be left; `nil` when no amount is recorded.
    let estimate: PantryEstimate?

    init(
        id: String, householdID: String, ingredientID: String?, key: String, displayName: String, category: String,
        quantity: String?, quantityValue: Double?, unit: String?, status: PantryStatus, isStaple: Bool,
        expiresOn: String?, note: String, updatedBy: String, createdAt: Date, updatedAt: Date,
        statusSource: PantryStatusSource = .person, lowThresholdPercent: Int? = nil, unitSize: PantryUnitSize? = nil,
        estimate: PantryEstimate? = nil
    ) {
        self.id = id
        self.householdID = householdID
        self.ingredientID = ingredientID
        self.key = key
        self.displayName = displayName
        self.category = category
        self.quantity = quantity
        self.quantityValue = quantityValue
        self.unit = unit
        self.status = status
        self.isStaple = isStaple
        self.expiresOn = expiresOn
        self.note = note
        self.updatedBy = updatedBy
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.statusSource = statusSource
        self.lowThresholdPercent = lowThresholdPercent
        self.unitSize = unitSize
        self.estimate = estimate
    }

    /// The usage estimate, not a person, marked the item low.
    var isEstimatedLow: Bool {
        status == .low && statusSource == .estimate
    }

    private enum CodingKeys: String, CodingKey {
        case id
        case householdID = "householdId"
        case ingredientID = "ingredientId"
        case key, displayName, category, quantity, quantityValue, unit, status, isStaple, expiresOn, note, updatedBy,
            createdAt, updatedAt, statusSource, lowThresholdPercent, unitSize, estimate
    }

    /// The usage fields are additive, so they're read leniently: a response without them, or
    /// with an estimate this build can't read, still shows the item.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            householdID: try container.decode(String.self, forKey: .householdID),
            ingredientID: try container.decodeIfPresent(String.self, forKey: .ingredientID),
            key: try container.decode(String.self, forKey: .key),
            displayName: try container.decode(String.self, forKey: .displayName),
            category: try container.decode(String.self, forKey: .category),
            quantity: try container.decodeIfPresent(String.self, forKey: .quantity),
            quantityValue: try container.decodeIfPresent(Double.self, forKey: .quantityValue),
            unit: try container.decodeIfPresent(String.self, forKey: .unit),
            status: try container.decode(PantryStatus.self, forKey: .status),
            isStaple: try container.decode(Bool.self, forKey: .isStaple),
            expiresOn: try container.decodeIfPresent(String.self, forKey: .expiresOn),
            note: try container.decode(String.self, forKey: .note),
            updatedBy: try container.decode(String.self, forKey: .updatedBy),
            createdAt: try container.decode(Date.self, forKey: .createdAt),
            updatedAt: try container.decode(Date.self, forKey: .updatedAt),
            statusSource: (try? container.decodeIfPresent(PantryStatusSource.self, forKey: .statusSource)) ?? .person,
            lowThresholdPercent: try? container.decodeIfPresent(Int.self, forKey: .lowThresholdPercent),
            unitSize: try? container.decodeIfPresent(PantryUnitSize.self, forKey: .unitSize),
            estimate: try? container.decodeIfPresent(PantryEstimate.self, forKey: .estimate))
    }
}

/// Response to `GET /api/v1/households/{householdId}/pantry`.
nonisolated struct PantryItemListResponse: Decodable, Equatable, Sendable {
    let items: [PantryItem]
}

/// The body of `POST .../pantry`. `nil` fields are omitted. When the pantry already has the
/// ingredient, the API merges: omitted fields keep their values.
nonisolated struct NewPantryItem: Encodable, Equatable, Sendable {
    /// A catalog ingredient from `GET /api/v1/ingredients`. Either this or `name` is required.
    var ingredientID: String?
    var name: String?
    var category: String?
    /// Exact, as produced by `PantryQuantity.parse`.
    var quantity: String?
    var unit: String?
    var status: PantryStatus?
    var isStaple: Bool?
    var expiresOn: String?
    var note: String?

    private enum CodingKeys: String, CodingKey {
        case ingredientID = "ingredientId"
        case name, category, quantity, unit, status, isStaple, expiresOn, note
    }
}

/// The body of `PATCH .../pantry/{itemId}`. `nil` fields are omitted and unchanged. An
/// empty string clears `quantity` (and its unit), `expiresOn`, or `note`.
nonisolated struct PantryItemChanges: Encodable, Equatable, Sendable {
    var displayName: String?
    var category: String?
    var quantity: String?
    var unit: String?
    var status: PantryStatus?
    var isStaple: Bool?
    var expiresOn: String?
    var note: String?
    /// `.household` sends `null`, which returns the item to the household's threshold.
    var lowThresholdPercent: PantryThresholdChange?

    /// The API rejects a PATCH that changes nothing.
    var isEmpty: Bool { self == PantryItemChanges() }

    private enum CodingKeys: String, CodingKey {
        case displayName, category, quantity, unit, status, isStaple, expiresOn, note, lowThresholdPercent
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(displayName, forKey: .displayName)
        try container.encodeIfPresent(category, forKey: .category)
        try container.encodeIfPresent(quantity, forKey: .quantity)
        try container.encodeIfPresent(unit, forKey: .unit)
        try container.encodeIfPresent(status, forKey: .status)
        try container.encodeIfPresent(isStaple, forKey: .isStaple)
        try container.encodeIfPresent(expiresOn, forKey: .expiresOn)
        try container.encodeIfPresent(note, forKey: .note)
        switch lowThresholdPercent {
        case .household: try container.encodeNil(forKey: .lowThresholdPercent)
        case .percent(let percent): try container.encode(percent, forKey: .lowThresholdPercent)
        case nil: break
        }
    }
}

/// One entry of `POST .../pantry/bulk`.
nonisolated struct PantryStatusUpdate: Codable, Equatable, Sendable {
    let id: String
    let status: PantryStatus
}

nonisolated struct PantryBulkStatusRequest: Encodable, Equatable, Sendable {
    let items: [PantryStatusUpdate]
}

/// Response to `POST .../pantry/bulk`.
nonisolated struct PantryBulkStatusResponse: Decodable, Equatable, Sendable {
    /// The updated items, in request order.
    let items: [PantryItem]
    /// Requested IDs that aren't in the pantry, for example because another member deleted them.
    let missing: [String]
}

/// Response to `POST .../pantry/staples/defaults`.
nonisolated struct PantryDefaultStaplesResponse: Decodable, Equatable, Sendable {
    /// The staples added.
    let items: [PantryItem]
    /// Default staples the pantry already had.
    let skipped: Int
}

/// What a multi-select status change did, for the summary shown afterwards.
nonisolated struct PantryBulkOutcome: Equatable, Sendable {
    let status: PantryStatus
    let updatedCount: Int
    /// Requested items that were no longer in the pantry.
    let missingCount: Int
    /// Display names of the missing items that were known locally, in request order.
    let missingNames: [String]

    var summary: String {
        let updated =
            updatedCount == 1
            ? String(localized: "Marked 1 item as \(status.phrase).")
            : String(localized: "Marked \(updatedCount) items as \(status.phrase).")
        guard missingCount > 0 else { return updated }
        let names = missingNames.count == missingCount ? missingNames.formatted(.list(type: .and)) : nil
        let missing: String
        switch (names, missingCount) {
        case (let names?, 1):
            missing = String(localized: "\(names) was no longer in the pantry. Someone may have removed it.")
        case (let names?, _):
            missing = String(localized: "\(names) were no longer in the pantry. Someone may have removed them.")
        case (nil, 1):
            missing = String(localized: "1 item was no longer in the pantry. Someone may have removed it.")
        case (nil, let count):
            missing = String(localized: "\(count) items were no longer in the pantry. Someone may have removed them.")
        }
        return updated + " " + missing
    }
}

// MARK: - Ingredient catalog

/// A global catalog ingredient (`CatalogIngredient`).
nonisolated struct CatalogIngredient: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let key: String
    let name: String
    let category: String
    /// `false` when no category rule matched and `category` is a placeholder.
    let categoryConfident: Bool
    let imageURLString: String?

    private enum CodingKeys: String, CodingKey {
        case id, key, name, category, categoryConfident
        case imageURLString = "imageUrl"
    }
}

/// Response to `GET /api/v1/ingredients`.
nonisolated struct IngredientSearchResponse: Decodable, Equatable, Sendable {
    let items: [CatalogIngredient]
}

// MARK: - Categories and units

/// Grocery categories (`GroceryCategoryName`), in the aisle order the API uses.
nonisolated enum PantryCategory {
    static let aisleOrder = [
        "produce", "meat-seafood", "dairy-eggs", "bakery", "deli", "pantry", "spices", "condiments", "frozen",
        "beverages", "other",
    ]

    /// Position in aisle order. A category from a newer server sorts after every known one.
    static func rank(_ category: String) -> Int {
        aisleOrder.firstIndex(of: category) ?? aisleOrder.count
    }

    static func title(_ category: String) -> String {
        switch category {
        case "produce": String(localized: "Produce")
        case "meat-seafood": String(localized: "Meat & Seafood")
        case "dairy-eggs": String(localized: "Dairy & Eggs")
        case "bakery": String(localized: "Bakery")
        case "deli": String(localized: "Deli")
        case "pantry": String(localized: "Pantry")
        case "spices": String(localized: "Spices")
        case "condiments": String(localized: "Condiments")
        case "frozen": String(localized: "Frozen")
        case "beverages": String(localized: "Beverages")
        case "other": String(localized: "Other")
        default: category.capitalized
        }
    }
}

/// DinnerOS unit codes (`ingredients.UnitCodes` in the API; docs/grocery-engine.md#units).
nonisolated enum PantryUnit {
    static let defaultCode = "count"

    /// Counts, then volume, then mass, each smallest first.
    static let codes = [
        "count", "clove", "can", "package", "slice", "bunch", "pinch", "thumb",
        "tsp", "tbsp", "floz", "cup", "ml", "l",
        "oz", "lb", "g", "kg",
    ]

    /// The picker's choices, keeping a code this build doesn't know so editing doesn't drop it.
    static func options(including code: String) -> [String] {
        codes.contains(code) ? codes : codes + [code]
    }

    static func pickerLabel(_ code: String) -> String {
        code == defaultCode
            ? String(localized: "Count") : RecipeFormat.unitLabel(code, sourceUnit: code, plural: true)
    }
}
