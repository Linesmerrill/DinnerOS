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

    /// The warnings to list, without the imported-recipe note, which has its own place.
    var listedWarnings: [String] { warnings.filter { $0 != ImportedRecipeText.note } }

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
    /// The rest of the line: "diced", "14.5 oz", "to taste".
    var note: String?
    /// The heading the line came under on the page ("Sauce").
    var group: String?
    /// The row's identity on screen. It never changes while the member types, so the
    /// field keeps its keyboard; it is not sent to the server.
    var key = UUID()

    var id: UUID { key }

    enum CodingKeys: String, CodingKey {
        case name, quantity, unit, rawText, pantryStaple, note, group
    }

    /// Two lines are the same when what's saved is the same, whatever their row identity.
    static func == (a: Self, b: Self) -> Bool {
        a.name == b.name && a.quantity == b.quantity && a.unit == b.unit && a.rawText == b.rawText
            && a.pantryStaple == b.pantryStaple && a.note == b.note && a.group == b.group
    }

    func hash(into hasher: inout Hasher) {
        hasher.combine(name)
        hasher.combine(quantity)
        hasher.combine(unit)
        hasher.combine(rawText)
    }
}

/// The body of `POST .../recipes/parse`. Exactly one field is filled.
nonisolated struct ParseRecipeRequest: Encodable, Sendable {
    var text: String?
    var url: String?
    /// The page as this phone loaded it, for a site that turned the server away.
    var html: String?
}

/// Recipe sources the app treats differently (`Recipe.source`).
nonisolated enum RecipeSourceKey {
    /// Read from a web page the member linked: only its title, servings, and ingredients
    /// are kept, with a link back for the rest.
    static let fromPage = "user"
    /// From a meal kit's order history: never shared to the catalog.
    static let mealKit = "hellofresh"
    /// Typed or pasted by the member.
    static let typed = "manual"
}

/// The note shown wherever a recipe read from a web page appears.
nonisolated enum ImportedRecipeText {
    static let note = String(
        localized: "Imported recipes can have mistakes. Please check the ingredients before you shop.")
}

/// The answer to `POST .../recipe-photos`: where the photo now lives.
nonisolated struct RecipePhotoUpload: Decodable, Equatable, Sendable {
    let imageURL: String

    enum CodingKeys: String, CodingKey {
        case imageURL = "imageUrl"
    }
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
        "count", "clove", "can", "bag", "package", "slice", "bunch", "pinch", "thumb",
        "tsp", "tbsp", "floz", "cup", "ml", "l",
        "oz", "lb", "g", "kg",
    ]

    /// The units most recipes use, shown as one-tap chips above the amount keypad.
    static let quick: [String] = ["oz", "g", "lb", "cup", "tbsp", "tsp", "can", "bag", "clove", "count"]

    /// What to show for a code. `count` is a plain number of things: "2 each".
    static func label(_ code: String) -> String {
        switch code {
        case "count": String(localized: "each")
        case "floz": "fl oz"
        case "tbsp": "Tbsp"
        case "package": String(localized: "package")
        default: code
        }
    }
}

/// Typing an amount on the amount keypad: digits, a decimal point, and fraction keys that
/// make "1 1/2" without hunting for a slash or a space. Pure, so it's tested without the screen.
nonisolated enum AmountEntry {
    enum Key: Equatable, Sendable {
        case digit(Int)
        case decimal
        /// A fraction key: ½ is `fraction(1, 2)`.
        case fraction(Int, Int)
        case delete
    }

    /// The fraction keys, in the order they're shown.
    static let fractions: [(Int, Int)] = [(1, 4), (1, 3), (1, 2), (2, 3), (3, 4)]

    /// What a fraction key shows: "½".
    static func symbol(_ n: Int, _ d: Int) -> String {
        switch (n, d) {
        case (1, 4): "¼"
        case (1, 3): "⅓"
        case (1, 2): "½"
        case (2, 3): "⅔"
        case (3, 4): "¾"
        default: "\(n)/\(d)"
        }
    }

    /// The text after pressing `key`.
    static func apply(_ key: Key, to text: String) -> String {
        switch key {
        case .digit(let digit):
            guard (0...9).contains(digit) else { return text }
            // A digit after a fraction starts nothing sensible: "1/2" then "3" isn't an amount.
            if lastWord(text).contains("/") { return text }
            return text + String(digit)
        case .decimal:
            let word = lastWord(text)
            if word.contains(".") || word.contains("/") { return text }
            return text + (word.isEmpty ? "0." : ".")
        case .fraction(let n, let d):
            let fraction = "\(n)/\(d)"
            let trimmed = text.trimmingCharacters(in: .whitespaces)
            if trimmed.isEmpty { return fraction }
            let word = lastWord(trimmed)
            // Pressing another fraction swaps the one that's there.
            if word.contains("/") { return String(trimmed.dropLast(word.count)) + fraction }
            // "1." then ½ means "1 1/2".
            let whole = word.hasSuffix(".") ? String(trimmed.dropLast()) : trimmed
            if whole.contains(".") { return text }
            return whole + " " + fraction
        case .delete:
            guard !text.isEmpty else { return text }
            let word = lastWord(text)
            // A fraction goes in one press, as it came in.
            if word.contains("/") {
                return String(text.dropLast(word.count)).trimmingCharacters(in: .whitespaces)
            }
            return String(text.dropLast())
        }
    }

    /// How the amount reads on screen: "1 1/2" as "1 ½".
    static func display(_ text: String) -> String {
        var out = text
        for (n, d) in fractions {
            out = out.replacingOccurrences(of: "\(n)/\(d)", with: symbol(n, d))
        }
        return out
    }

    /// The amount as saved, from what the field shows: "1 ½" as "1 1/2".
    static func raw(_ shown: String) -> String {
        var out = shown
        for (n, d) in fractions {
            let symbol = symbol(n, d)
            // "1½" typed on a hardware keyboard still means "1 1/2".
            out = out.replacingOccurrences(
                of: #"(\d)"# + symbol, with: "$1 \(n)/\(d)", options: .regularExpression)
            out = out.replacingOccurrences(of: symbol, with: "\(n)/\(d)")
        }
        return out
    }

    private static func lastWord(_ text: String) -> String {
        String(text.split(separator: " ", omittingEmptySubsequences: false).last ?? "")
    }
}
