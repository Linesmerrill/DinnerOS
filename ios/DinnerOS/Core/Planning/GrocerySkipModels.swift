import Foundation

/// How far a skip reaches. Unknown values decode as-is, like `GroceryItemStatus`.
nonisolated struct GrocerySkipScope: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Off this week's list only; back next week with no action.
    static let week = GrocerySkipScope(rawValue: "week")
    /// Off every list, and out of every recipe, until someone resumes it.
    static let always = GrocerySkipScope(rawValue: "always")
    /// Out of one recipe ("just this dish") every time it's planned, while other recipes on the
    /// list still get it. On a grocery item it means the item is only the left-out meals' part.
    static let recipe = GrocerySkipScope(rawValue: "recipe")

    /// Which of two skips on one ingredient wins: always, then week, then recipe. The API
    /// resolves it the same way.
    var rank: Int {
        switch self {
        case .always: 3
        case .week: 2
        case .recipe: 1
        default: 0
        }
    }
}

/// An ingredient the household leaves off its grocery list on purpose.
///
/// This is a different thing from an item the pantry has ("already at home") and from a line
/// someone checked off ("bought"): an item you own, an item you bought, and an item you never
/// want are three states, and the app keeps them apart.
nonisolated struct GrocerySkip: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    /// The grocery line key the skip was made from.
    let ingredientKey: String
    /// The normalized name the skip also matches.
    let key: String
    let name: String
    let scope: GrocerySkipScope
    /// The week a `week` skip covers; `nil` when it is `always`.
    let week: String?
    /// What the skip does, in the server's words ("Never buying this").
    let text: String
    /// The recipe a `recipe` skip leaves the ingredient out of; `nil` otherwise, and from a server
    /// without recipe skips.
    var recipeID: String? = nil
    /// The recipe's name when the skip was made, so the dish can still be named after it's gone.
    var recipeName: String? = nil

    var isForever: Bool { scope == .always }

    /// Identifies the skip among the household's: one per ingredient household-wide, and one per
    /// ingredient and recipe.
    var slot: GrocerySkipSlot { GrocerySkipSlot(ingredientKey: ingredientKey, recipeID: recipeID) }

    /// Whether this skip is about a grocery line with `key`. A skip matches the key it was made
    /// from and its normalized name, the two spellings a line can carry.
    /// A `name:` key built on the phone from a display name ("name:Smoky Red Pepper Crema") is
    /// compared without case, as the API normalizes it.
    func matches(ingredientKey key: String) -> Bool {
        let lowered = key.lowercased()
        return ingredientKey == key || ingredientKey == lowered || "name:\(self.key)" == lowered
    }

    private enum CodingKeys: String, CodingKey {
        case id, ingredientKey, key, name, scope, week, text
        case recipeID = "recipeId"
        case recipeName
    }
}

/// One skip's place among a household's: an ingredient, household-wide or for one recipe.
nonisolated struct GrocerySkipSlot: Hashable, Sendable {
    let ingredientKey: String
    let recipeID: String?
}

/// The body of `POST .../grocery-skips`.
nonisolated struct GrocerySkipRequest: Encodable, Equatable, Sendable {
    let ingredientKey: String
    let name: String
    let scope: GrocerySkipScope
    let week: String?
    /// Required for `recipe`, and absent otherwise.
    var recipeID: String? = nil

    var slot: GrocerySkipSlot { GrocerySkipSlot(ingredientKey: ingredientKey, recipeID: recipeID) }

    private enum CodingKeys: String, CodingKey {
        case ingredientKey, name, scope, week
        case recipeID = "recipeId"
    }

    /// Leaves an ingredient out of one recipe, every time it's planned ("just this dish").
    static func leaveOut(ingredientKey: String, name: String, recipeID: String) -> GrocerySkipRequest {
        GrocerySkipRequest(ingredientKey: ingredientKey, name: name, scope: .recipe, week: nil, recipeID: recipeID)
    }

    /// Leaves an ingredient out of every recipe and off every list ("always").
    static func always(ingredientKey: String, name: String) -> GrocerySkipRequest {
        GrocerySkipRequest(ingredientKey: ingredientKey, name: name, scope: .always, week: nil)
    }

    /// Leaves the ingredient off one week's list.
    static func thisWeek(item: GroceryItem, week: ISOWeek) -> GrocerySkipRequest {
        GrocerySkipRequest(
            ingredientKey: item.ingredientKey, name: item.name, scope: .week, week: week.description)
    }

    /// Leaves the ingredient off every list until someone resumes it.
    static func forever(item: GroceryItem) -> GrocerySkipRequest {
        GrocerySkipRequest(ingredientKey: item.ingredientKey, name: item.name, scope: .always, week: nil)
    }

    /// Changes an existing household-wide skip's lifetime, keeping the ingredient it is about. The
    /// API replaces the stored skip rather than adding a second one.
    static func changing(_ skip: GrocerySkip, to scope: GrocerySkipScope, week: ISOWeek) -> GrocerySkipRequest {
        GrocerySkipRequest(
            ingredientKey: skip.ingredientKey, name: skip.name, scope: scope,
            week: scope == .week ? week.description : nil,
            recipeID: scope == .recipe ? skip.recipeID : nil)
    }
}

nonisolated struct GrocerySkipListResponse: Decodable, Sendable {
    let items: [GrocerySkip]
}
