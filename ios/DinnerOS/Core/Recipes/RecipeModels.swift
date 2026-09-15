import Foundation

/// A recipe in a list (`RecipeSummary` in `api/openapi.yaml`).
nonisolated struct RecipeSummary: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let name: String
    let headline: String?
    let imageURLString: String?
    let totalMinutes: Int?
    let timesOrdered: Int
    /// An ISO week such as `2026-W30`; `nil` when never ordered.
    let lastOrderedWeek: String?
    let isAddon: Bool
    let tags: [String]
    /// Updated in place after the user rates the recipe.
    var householdRating: HouseholdRating = .unrated
    /// The signed-in user's rating, or `nil`.
    var myRating: RecipeRating?

    /// `nil` when missing or not a valid URL. Imported data isn't trusted to be well formed.
    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    private enum CodingKeys: String, CodingKey {
        case id, name, headline
        case imageURLString = "imageUrl"
        case totalMinutes, timesOrdered, lastOrderedWeek, isAddon, tags, householdRating, myRating
    }
}

/// Response to `GET /api/v1/households/{householdId}/recipes`.
nonisolated struct RecipeListPage: Decodable, Equatable, Sendable {
    let items: [RecipeSummary]
    /// Send back as `cursor` for the next page; `nil` on the last page.
    let nextCursor: String?
}

/// Response to `GET /api/v1/households/{householdId}/recipes/{recipeId}`.
nonisolated struct Recipe: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    let householdID: String
    let source: String
    let name: String
    let headline: String?
    let description: String?
    let imageURLString: String?
    let isAddon: Bool
    /// Serving sizes with authored amounts, for example `[2, 4]`.
    let servings: [Int]
    let prepMinutes: Int?
    let totalMinutes: Int?
    /// On the source's own scale.
    let difficulty: Int?
    let cuisines: [String]
    let tags: [String]
    let utensils: [String]
    let allergens: [String]
    let nutritionPerServing: [RecipeNutrient]
    let ingredients: [RecipeIngredient]
    let steps: [RecipeStep]
    /// ISO weeks the household received this recipe, oldest first.
    let orderWeeks: [String]
    let timesOrdered: Int
    let lastOrderedWeek: String?
    let createdAt: Date
    let updatedAt: Date
    var householdRating: HouseholdRating = .unrated
    /// The signed-in user's rating, or `nil`.
    var myRating: RecipeRating?

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    private enum CodingKeys: String, CodingKey {
        case id
        case householdID = "householdId"
        case source, name, headline, description
        case imageURLString = "imageUrl"
        case isAddon, servings, prepMinutes, totalMinutes, difficulty, cuisines, tags, utensils, allergens,
            nutritionPerServing, ingredients, steps, orderWeeks, timesOrdered, lastOrderedWeek, createdAt, updatedAt,
            householdRating, myRating
    }
}

nonisolated struct RecipeNutrient: Decodable, Equatable, Sendable {
    let name: String
    let amount: Double
    let unit: String
}

nonisolated struct RecipeStep: Decodable, Equatable, Sendable, Identifiable {
    /// 1-based.
    let index: Int
    let text: String
    let imageURLString: String?

    var id: Int { index }
    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    private enum CodingKeys: String, CodingKey {
        case index, text
        case imageURLString = "imageUrl"
    }
}

/// An ingredient line with its catalog category.
nonisolated struct RecipeIngredient: Decodable, Equatable, Sendable {
    let ingredientID: String
    let name: String
    let category: String
    /// A source hint that the ingredient is expected at home (salt, oil).
    let pantryStaple: Bool
    /// One entry per serving size.
    let amounts: [RecipeAmount]

    private enum CodingKeys: String, CodingKey {
        case ingredientID = "ingredientId"
        case name, category, pantryStaple, amounts
    }
}

/// An ingredient amount authored for one serving size.
nonisolated struct RecipeAmount: Decodable, Equatable, Sendable {
    let servings: Int
    /// Exact, as `"n"` or `"n/d"`. `nil` when the source gave no amount.
    let quantity: String?
    /// The same amount as a number, for display only.
    let quantityValue: Double?
    /// A DinnerOS unit code (`tbsp`, `oz`, `count`), or empty.
    let unit: String
    /// The unit as the source wrote it.
    let sourceUnit: String
    let rawText: String
}

// MARK: - List parameters

/// Sort orders offered by the recipe list (`sort`).
nonisolated enum RecipeSort: String, CaseIterable, Identifiable, Sendable {
    case recent
    case popular
    case name

    var id: String { rawValue }

    var title: String {
        switch self {
        case .recent: String(localized: "Recent")
        case .popular: String(localized: "Most Ordered")
        case .name: String(localized: "A–Z")
        }
    }
}

/// Mains, add-ons, or both (`addons`).
nonisolated enum RecipeKind: String, CaseIterable, Identifiable, Sendable {
    case all
    case mains
    case addons

    var id: String { rawValue }

    var title: String {
        switch self {
        case .all: String(localized: "All")
        case .mains: String(localized: "Mains")
        case .addons: String(localized: "Add-ons")
        }
    }

    /// The `addons` parameter; `nil` omits it.
    var addonsParameter: Bool? {
        switch self {
        case .all: nil
        case .mains: false
        case .addons: true
        }
    }
}

/// Filters for one recipe list. Changing any of them starts a new cursor sequence.
nonisolated struct RecipeListFilters: Equatable, Sendable {
    /// The API accepts at most this many characters of search text.
    static let maxSearchLength = 100

    var search = ""
    var sort: RecipeSort = .recent
    var kind: RecipeKind = .all
    var tag: String?
    var cuisine: String?

    /// Whether anything narrows the list, so an empty result isn't "no recipes at all".
    var isNarrowed: Bool {
        !normalizedSearch.isEmpty || kind != .all || tag != nil || cuisine != nil
    }

    /// Trimmed and cut to the API's limit.
    var normalizedSearch: String {
        String(search.trimmingCharacters(in: .whitespacesAndNewlines).prefix(Self.maxSearchLength))
    }

    /// Whether both send the same request, for example "taco" and "taco ".
    func isSameQuery(as other: RecipeListFilters) -> Bool {
        normalizedSearch == other.normalizedSearch && sort == other.sort && kind == other.kind
            && tag == other.tag && cuisine == other.cuisine
    }
}
