import Foundation

/// An option a grocery list suggests for an unchosen specialty ingredient.
nonisolated struct GrocerySpecialtyOption: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    let type: SpecialtyOptionType
    let name: String
    let isDefault: Bool
}

/// A grocery item that is a specialty ingredient by its own name (`GrocerySpecialty`).
nonisolated struct GrocerySpecialty: Decodable, Equatable, Sendable, Identifiable {
    /// The specialty ingredient's slug.
    let id: String
    let key: String
    let name: String
    /// `nil` when the household hasn't chosen; `as_is`, or `house_made_batch` when a pantry batch
    /// covers the week.
    let choiceType: SpecialtyChoiceType?
    let optionID: String?
    /// Covered by a house-made batch in the pantry ("In pantry (house-made)").
    let houseMade: Bool
    /// The default first; empty once chosen.
    let suggestedOptions: [GrocerySpecialtyOption]
    /// English text, for example "Specialty ingredient: choose a store alternative or a house-made batch".
    let text: String

    private enum CodingKeys: String, CodingKey {
        case id, key, name, choiceType
        case optionID = "optionId"
        case houseMade, suggestedOptions, text
    }
}

/// A specialty ingredient a grocery item stands in for (`GroceryVia`).
nonisolated struct GroceryVia: Decodable, Equatable, Sendable {
    let kind: SpecialtyOptionType
    let specialtyID: String
    let specialtyKey: String
    let specialtyName: String
    let optionID: String
    let optionName: String
    /// The strategy that picked the option, as sent: empty when a member chose it explicitly.
    /// Read `strategy`, which reads an empty string as "nobody chose".
    private let strategyRawValue: String?
    /// What one batch makes; `nil` for a store alternative.
    let batchYield: GroceryAmount?
    /// Batches the list asks for; `nil` for a store alternative.
    let batches: Int?
    let recipes: [GroceryRecipe]
    /// For example "for Tex-Mex Paste in Smoky Pork Tacos". The server already ends it with
    /// "(your default)" when a strategy picked the option.
    let text: String

    init(
        kind: SpecialtyOptionType, specialtyID: String, specialtyKey: String, specialtyName: String,
        optionID: String, optionName: String, batchYield: GroceryAmount?, batches: Int?,
        recipes: [GroceryRecipe], text: String, strategy: SpecialtyStrategy? = nil
    ) {
        self.kind = kind
        self.specialtyID = specialtyID
        self.specialtyKey = specialtyKey
        self.specialtyName = specialtyName
        self.optionID = optionID
        self.optionName = optionName
        self.strategyRawValue = strategy?.rawValue
        self.batchYield = batchYield
        self.batches = batches
        self.recipes = recipes
        self.text = text
    }

    /// The household's standing strategy that put this line on the list; `nil` when a member
    /// chose the option themselves.
    var strategy: SpecialtyStrategy? {
        guard let raw = strategyRawValue, !raw.isEmpty else { return nil }
        return SpecialtyStrategy(rawValue: raw)
    }

    /// Nobody chose this option: the household's standing strategy picked it.
    var isFromStrategy: Bool { strategy != nil }

    private enum CodingKeys: String, CodingKey {
        case kind
        case specialtyID = "specialtyId"
        case specialtyKey, specialtyName
        case optionID = "optionId"
        case optionName
        case strategyRawValue = "strategy"
        case batchYield = "yield"
        case batches, recipes, text
    }
}

/// Whether a week's house-made batch is in the pantry or needs making. Unknown values decode as-is.
nonisolated struct GroceryBatchStatus: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let inPantry = GroceryBatchStatus(rawValue: "inPantry")
    static let make = GroceryBatchStatus(rawValue: "make")
}

/// Why a batch is in the pantry or needs making. Unknown values decode as-is.
nonisolated struct GroceryBatchReason: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// In stock, and the estimate covers the week.
    static let enough = GroceryBatchReason(rawValue: "enough")
    /// In stock, but the amounts can't be compared.
    static let inStock = GroceryBatchReason(rawValue: "inStock")
    static let notEnough = GroceryBatchReason(rawValue: "notEnough")
    static let low = GroceryBatchReason(rawValue: "low")
    static let out = GroceryBatchReason(rawValue: "out")
    static let missing = GroceryBatchReason(rawValue: "missing")
}

/// A house-made specialty ingredient the week uses (`GroceryBatch`).
nonisolated struct GroceryBatch: Decodable, Equatable, Sendable, Identifiable {
    let specialtyID: String
    let specialtyKey: String
    let specialtyName: String
    let optionID: String
    let optionName: String
    let batchYield: GroceryAmount
    let status: GroceryBatchStatus
    let reason: GroceryBatchReason
    /// Batches to make; 0 when in the pantry.
    let batches: Int
    let pantryItemID: String?
    /// The pantry estimate; `nil` when untracked or missing.
    let remaining: GroceryAmount?
    /// The week's total in the yield's unit; `nil` when it can't be totaled.
    let needed: GroceryAmount?
    let recipes: [GroceryRecipe]
    /// For example "Make a batch (makes about 12 tbsp)".
    let text: String

    var id: String { specialtyID }

    private enum CodingKeys: String, CodingKey {
        case specialtyID = "specialtyId"
        case specialtyKey, specialtyName
        case optionID = "optionId"
        case optionName
        case batchYield = "yield"
        case status, reason, batches
        case pantryItemID = "pantryItemId"
        case remaining, needed, recipes, text
    }
}

/// How the grocery list screen arranges a list: house-made batches to make, each with the raw
/// ingredients bought only for it, then batches already made, then the aisles.
///
/// Grouping is conservative. An item moves under a batch only when every `via` is that one batch
/// and every recipe the item lists needs the batch, so its whole amount is for the batch. An item
/// also bought for another batch, a store alternative, or a recipe that uses it directly stays in
/// its aisle, where its `via` text explains it.
nonisolated struct GroceryListLayout: Equatable, Sendable {
    nonisolated struct BatchGroup: Equatable, Sendable, Identifiable {
        let batch: GroceryBatch
        /// Items bought only to make this batch, in aisle order.
        let ingredients: [GroceryItem]

        var id: String { batch.id }
    }

    let toMake: [BatchGroup]
    let alreadyMade: [GroceryBatch]
    /// The server's categories without grouped items; categories left empty are omitted.
    let categories: [GroceryCategory]
    /// Specialty ingredients the household hasn't chosen an option for, in aisle order.
    let needsChoice: [GroceryItem]

    init(_ list: GroceryList) {
        let making = list.batches.filter { $0.status == .make }
        let makingIDs = Set(making.map(\.specialtyID))
        var grouped: [String: [GroceryItem]] = [:]
        var categories: [GroceryCategory] = []
        for category in list.categories {
            var remaining: [GroceryItem] = []
            for item in category.items {
                if let specialtyID = Self.batchSpecialtyID(for: item), makingIDs.contains(specialtyID) {
                    grouped[specialtyID, default: []].append(item)
                } else {
                    remaining.append(item)
                }
            }
            if !remaining.isEmpty {
                categories.append(GroceryCategory(category: category.category, items: remaining))
            }
        }
        toMake = making.map { BatchGroup(batch: $0, ingredients: grouped[$0.specialtyID] ?? []) }
        alreadyMade = list.batches.filter { $0.status == .inPantry }
        self.categories = categories
        needsChoice = list.allItems.filter(\.needsSpecialtyChoice)
    }

    /// The batch `item` is bought only for, or `nil`.
    static func batchSpecialtyID(for item: GroceryItem) -> String? {
        guard let first = item.via.first else { return nil }
        let isOneBatch = item.via.allSatisfy { $0.kind == .houseMadeBatch && $0.specialtyID == first.specialtyID }
        guard isOneBatch else { return nil }
        let batchRecipes = Set(item.via.flatMap(\.recipes).map(\.id))
        guard item.recipes.allSatisfy({ batchRecipes.contains($0.id) }) else { return nil }
        return first.specialtyID
    }
}
