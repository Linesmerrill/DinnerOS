import Foundation

/// Response to `GET .../shopping/handoffs/{handoffId}/bulk-packs`: the lines whose packages
/// hold far more than the week needs (docs/shopping-providers.md#bulk-packs).
nonisolated struct ShoppingBulkPackList: Decodable, Equatable, Sendable {
    let week: String
    let provider: String
    let handoffID: String
    let packs: [ShoppingBulkPack]

    var isEmpty: Bool { packs.isEmpty }

    /// Packs still worth asking about: the remainder isn't already sealed.
    var open: [ShoppingBulkPack] { packs.filter { !$0.frozen } }

    private enum CodingKeys: String, CodingKey {
        case week, provider, packs
        case handoffID = "handoffId"
    }
}

/// One line the week over-bought, with what to do about it.
nonisolated struct ShoppingBulkPack: Decodable, Equatable, Sendable, Identifiable {
    let lineID: String
    let ingredientKey: String
    let ingredientID: String?
    let name: String
    let category: String
    let productID: String
    let productName: String
    let packages: Int
    /// The package size's unit; `bought`, `needed` and `surplus` are in it.
    let unit: String
    /// Exact, as `"n"` or `"n/d"`; the `*Value` fields are the same numbers for display.
    let bought: String
    let boughtValue: Double
    let needed: String
    let neededValue: Double
    let surplus: String
    let surplusValue: Double
    let surplusPercent: Int
    /// Ready to show: "This week uses 10 oz of 64 oz".
    let surplusText: String
    /// Whether to offer sealing it; produce and dairy never are.
    let freezable: Bool
    /// The remainder is already in the freezer.
    let frozen: Bool
    /// Second meals to plan this week, best first.
    let suggestions: [ShoppingBulkPackSuggestion]

    var id: String { lineID }

    private enum CodingKeys: String, CodingKey {
        case lineID = "lineId"
        case ingredientID = "ingredientId"
        case ingredientKey, name, category, productID = "productId", productName, packages, unit,
            bought, boughtValue, needed, neededValue, surplus, surplusValue, surplusPercent,
            surplusText, freezable, frozen, suggestions
    }
}

/// A recipe Autopilot suggests planning later the same week so the surplus gets cooked.
nonisolated struct ShoppingBulkPackSuggestion: Decodable, Equatable, Sendable, Identifiable {
    let recipeID: String
    let recipeName: String
    let imageURL: String?
    /// The open day it was ranked for, `"thu"`.
    let day: String
    let servings: Int
    let cookMinutes: Int?
    /// Autopilot's own reasons for the pick, most important first.
    let reasons: [String]

    var id: String { recipeID }

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case recipeName
        case imageURL = "imageUrl"
        case day, servings, cookMinutes, reasons
    }
}
