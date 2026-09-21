import Foundation

/// A recipe parsed from pasted text or a web page, before anyone saves it
/// (`RecipeDraft` in `api/openapi.yaml`).
///
/// The server parses and the member reviews: a draft is never stored until the
/// edited version comes back to `POST .../recipes`. Everything here came from
/// text nobody vouched for, so it is shown as text and never as markup.
nonisolated struct RecipeDraft: Codable, Equatable, Sendable {
    var name: String = ""
    var headline: String?
    var description: String?
    var sourceURL: String?
    var imageURL: String?
    /// What the amounts are for; 0 when the source didn't say.
    var servings: Int = 0
    var prepMinutes: Int?
    var totalMinutes: Int?
    var cuisines: [String] = []
    var tags: [String] = []
    var ingredients: [RecipeDraftIngredient] = []
    var steps: [String] = []
    /// What the parser could not read. Advice for the person reviewing, not errors.
    var warnings: [String] = []
    /// True when the draft came from a web page. Sent back unchanged.
    var fromURL: Bool = false

    enum CodingKeys: String, CodingKey {
        case name, headline, description
        case sourceURL = "sourceUrl"
        case imageURL = "imageUrl"
        case servings, prepMinutes, totalMinutes, cuisines, tags, ingredients, steps, warnings
        case fromURL = "fromUrl"
    }

    /// Whether the draft has enough to save.
    var isSaveable: Bool {
        !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

/// One ingredient line of a draft.
nonisolated struct RecipeDraftIngredient: Codable, Equatable, Sendable, Identifiable, Hashable {
    var name: String = ""
    /// The amount as text (`"1 1/2"`); empty when the line gave none.
    var quantity: String?
    /// A DinnerOS unit code, empty when counted or unrecognized.
    var unit: String?
    /// The original line, kept so nothing the parser misread is lost.
    var rawText: String?
    var pantryStaple: Bool = false

    /// Stable within one draft: the list is edited in place, so identity is by
    /// position rather than by a name the member is still typing.
    var id: String { (rawText ?? "") + "|" + name + "|" + (quantity ?? "") + "|" + (unit ?? "") }
}

/// The body of `POST .../recipes/parse`. Exactly one field is filled.
nonisolated struct ParseRecipeRequest: Encodable, Sendable {
    var text: String?
    var url: String?
}

/// The body of `PUT .../recipes/{recipeId}/sharing`.
nonisolated struct RecipeSharingRequest: Encodable, Sendable {
    let sharedToCatalog: Bool
}

/// A recipe's global-catalog opt-in (`PUT .../recipes/{recipeId}/sharing`).
nonisolated struct RecipeSharing: Decodable, Equatable, Sendable {
    let recipeID: String
    let sharedToCatalog: Bool
    /// True when the recipe is in the global catalog, either because it is
    /// shared or because it came from a public source.
    let inCatalog: Bool

    fileprivate enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case sharedToCatalog, inCatalog
    }
}

/// The units a member can pick for a typed ingredient. They mirror the server's
/// unit codes (`ingredients.UnitCodes`); anything else is refused on save.
nonisolated enum RecipeUnit {
    static let codes: [String] = [
        "count", "clove", "can", "package", "slice", "bunch", "pinch", "thumb",
        "tsp", "tbsp", "floz", "cup", "ml", "l",
        "oz", "lb", "g", "kg",
    ]

    /// What to show for a code. `count` means "no unit".
    static func label(_ code: String) -> String {
        switch code {
        case "count": String(localized: "No unit")
        case "floz": "fl oz"
        case "package": String(localized: "package")
        default: code
        }
    }
}
