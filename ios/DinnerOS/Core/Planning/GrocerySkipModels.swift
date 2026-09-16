import Foundation

/// How long a skip lasts. Unknown values decode as-is, like `GroceryItemStatus`.
nonisolated struct GrocerySkipScope: RawRepresentable, Codable, Hashable, Sendable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    /// Off this week's list only; back next week with no action.
    static let week = GrocerySkipScope(rawValue: "week")
    /// Off every list until someone resumes it.
    static let always = GrocerySkipScope(rawValue: "always")
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

    var isForever: Bool { scope == .always }
}

/// The body of `POST .../grocery-skips`.
nonisolated struct GrocerySkipRequest: Encodable, Sendable {
    let ingredientKey: String
    let name: String
    let scope: GrocerySkipScope
    let week: String?

    /// Leaves the ingredient off one week's list.
    static func thisWeek(item: GroceryItem, week: ISOWeek) -> GrocerySkipRequest {
        GrocerySkipRequest(
            ingredientKey: item.ingredientKey, name: item.name, scope: .week, week: week.description)
    }

    /// Leaves the ingredient off every list until someone resumes it.
    static func forever(item: GroceryItem) -> GrocerySkipRequest {
        GrocerySkipRequest(ingredientKey: item.ingredientKey, name: item.name, scope: .always, week: nil)
    }

    /// Changes an existing skip's lifetime, keeping the ingredient it is about. The API replaces
    /// the stored skip rather than adding a second one.
    static func changing(_ skip: GrocerySkip, to scope: GrocerySkipScope, week: ISOWeek) -> GrocerySkipRequest {
        GrocerySkipRequest(
            ingredientKey: skip.ingredientKey, name: skip.name, scope: scope,
            week: scope == .week ? week.description : nil)
    }
}

nonisolated struct GrocerySkipListResponse: Decodable, Sendable {
    let items: [GrocerySkip]
}
