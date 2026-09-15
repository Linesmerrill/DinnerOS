import Foundation

/// A recipe in a list (`RecipeSummary` in `api/openapi.yaml`).
nonisolated struct RecipeSummary: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    let name: String
    let headline: String?
    let imageURLString: String?
    /// The source's total time. Unreliable (often below prep); show `displayMinutes`.
    let totalMinutes: Int?
    /// The effective cook time the API derives from prep and total time; `nil` when
    /// unknown or from a server that doesn't send it.
    var cookMinutes: Int? = nil
    let timesOrdered: Int
    /// An ISO week such as `2026-W30`; `nil` when never ordered.
    let lastOrderedWeek: String?
    let isAddon: Bool
    let tags: [String]
    /// Updated in place after the user rates the recipe.
    var householdRating: HouseholdRating = .unrated
    /// The signed-in user's rating, or `nil`.
    var myRating: RecipeRating?
    /// Per serving; `nil` when unknown or from a server without the menu fields.
    var calories: Int? = nil
    /// Grams per serving; `nil` when unknown.
    var proteinGrams: Int? = nil
    /// Quick, medium, or long; `nil` when unknown.
    var timeBand: AutopilotTimeBand? = nil

    /// `nil` when missing or not a valid URL. Imported data isn't trusted to be well formed.
    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    fileprivate enum CodingKeys: String, CodingKey {
        case id, name, headline
        case imageURLString = "imageUrl"
        case totalMinutes, cookMinutes, timesOrdered, lastOrderedWeek, isAddon, tags, householdRating, myRating
        case calories, proteinGrams, timeBand
    }

    /// The time to show: `cookMinutes`, falling back to `totalMinutes` for an older server.
    var displayMinutes: Int? {
        RecipeFormat.displayMinutes(cook: cookMinutes, prep: nil, total: totalMinutes)
    }

    /// A summary with only what a plan entry knows, for opening a recipe from the plan.
    static func placeholder(id: String, name: String, imageURLString: String?) -> RecipeSummary {
        RecipeSummary(
            id: id, name: name, headline: nil, imageURLString: imageURLString, totalMinutes: nil, timesOrdered: 0,
            lastOrderedWeek: nil, isAddon: false, tags: [])
    }
}

nonisolated extension RecipeSummary {
    /// The documented fields decode as before. The menu fields (`calories`, `proteinGrams`,
    /// `timeBand`) are additive, so they're read leniently.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            name: try container.decode(String.self, forKey: .name),
            headline: try container.decodeIfPresent(String.self, forKey: .headline),
            imageURLString: try container.decodeIfPresent(String.self, forKey: .imageURLString),
            totalMinutes: try container.decodeIfPresent(Int.self, forKey: .totalMinutes),
            cookMinutes: try container.decodeIfPresent(Int.self, forKey: .cookMinutes),
            timesOrdered: try container.decode(Int.self, forKey: .timesOrdered),
            lastOrderedWeek: try container.decodeIfPresent(String.self, forKey: .lastOrderedWeek),
            isAddon: try container.decode(Bool.self, forKey: .isAddon),
            tags: try container.decode([String].self, forKey: .tags),
            householdRating: try container.decode(HouseholdRating.self, forKey: .householdRating),
            myRating: try container.decodeIfPresent(RecipeRating.self, forKey: .myRating),
            calories: container.decodeLenientInt(forKey: .calories),
            proteinGrams: container.decodeLenientInt(forKey: .proteinGrams),
            timeBand: container.decodeLenient(AutopilotTimeBand.self, forKey: .timeBand))
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
    /// Unreliable in imported data; show `displayMinutes`.
    let totalMinutes: Int?
    /// The effective cook time; `nil` when unknown or from an older server.
    var cookMinutes: Int? = nil
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
    /// The recipe's page at its source, for sharing; `nil` when unknown.
    var sourceURLString: String? = nil
    /// Per-serving nutrition from a server that sends `nutrition`; empty otherwise.
    var nutrition: [RecipeNutrient] = []

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }
    var sourceURL: URL? { sourceURLString.flatMap { URL(string: $0) } }

    fileprivate enum CodingKeys: String, CodingKey {
        case id
        case householdID = "householdId"
        case source, name, headline, description
        case imageURLString = "imageUrl"
        case sourceURLString = "sourceUrl"
        case isAddon, servings, prepMinutes, totalMinutes, cookMinutes, difficulty, cuisines, tags, utensils,
            allergens, nutritionPerServing, nutrition, ingredients, steps, orderWeeks, timesOrdered, lastOrderedWeek,
            createdAt, updatedAt, householdRating, myRating
    }

    /// The time to show: `cookMinutes`, else the larger of prep and total time.
    var displayMinutes: Int? {
        RecipeFormat.displayMinutes(cook: cookMinutes, prep: prepMinutes, total: totalMinutes)
    }

    /// Per-serving nutrition: `nutrition` when the server sends it, else `nutritionPerServing`.
    var nutritionValues: [RecipeNutrient] {
        nutrition.isEmpty ? nutritionPerServing : nutrition
    }
}

nonisolated extension Recipe {
    /// Identity, names, servings, ingredients, and history decode strictly as before. Display
    /// details the recipe screen only shows when present (allergens, nutrition, difficulty,
    /// utensils, the source link) are read leniently.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            householdID: try container.decode(String.self, forKey: .householdID),
            source: try container.decode(String.self, forKey: .source),
            name: try container.decode(String.self, forKey: .name),
            headline: try container.decodeIfPresent(String.self, forKey: .headline),
            description: try container.decodeIfPresent(String.self, forKey: .description),
            imageURLString: try container.decodeIfPresent(String.self, forKey: .imageURLString),
            isAddon: try container.decode(Bool.self, forKey: .isAddon),
            servings: try container.decode([Int].self, forKey: .servings),
            prepMinutes: try container.decodeIfPresent(Int.self, forKey: .prepMinutes),
            totalMinutes: try container.decodeIfPresent(Int.self, forKey: .totalMinutes),
            cookMinutes: try container.decodeIfPresent(Int.self, forKey: .cookMinutes),
            difficulty: container.decodeLenientInt(forKey: .difficulty),
            cuisines: try container.decode([String].self, forKey: .cuisines),
            tags: try container.decode([String].self, forKey: .tags),
            utensils: container.decodeLossyArray(String.self, forKey: .utensils),
            allergens: container.decodeLossyArray(String.self, forKey: .allergens),
            nutritionPerServing: container.decodeLossyArray(RecipeNutrient.self, forKey: .nutritionPerServing),
            ingredients: try container.decode([RecipeIngredient].self, forKey: .ingredients),
            steps: try container.decode([RecipeStep].self, forKey: .steps),
            orderWeeks: try container.decode([String].self, forKey: .orderWeeks),
            timesOrdered: try container.decode(Int.self, forKey: .timesOrdered),
            lastOrderedWeek: try container.decodeIfPresent(String.self, forKey: .lastOrderedWeek),
            createdAt: try container.decode(Date.self, forKey: .createdAt),
            updatedAt: try container.decode(Date.self, forKey: .updatedAt),
            householdRating: try container.decode(HouseholdRating.self, forKey: .householdRating),
            myRating: try container.decodeIfPresent(RecipeRating.self, forKey: .myRating),
            sourceURLString: container.decodeLenient(String.self, forKey: .sourceURLString),
            nutrition: container.decodeLossyArray(RecipeNutrient.self, forKey: .nutrition))
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

    init(index: Int, text: String, imageURLString: String?) {
        self.index = index
        self.text = text
        self.imageURLString = imageURLString
    }

    /// A step image this build can't read is left out rather than failing the recipe.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        index = try container.decode(Int.self, forKey: .index)
        text = try container.decode(String.self, forKey: .text)
        imageURLString = container.decodeLenient(String.self, forKey: .imageURLString)
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
    /// A photo of the ingredient; `nil` from a server that doesn't send one.
    var imageURLString: String? = nil
    /// Allergens this ingredient contains, such as "Soy"; empty when unknown.
    var allergens: [String] = []

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    private enum CodingKeys: String, CodingKey {
        case ingredientID = "ingredientId"
        case name, category, pantryStaple, amounts
        case imageURLString = "imageUrl"
        case allergens
    }
}

nonisolated extension RecipeIngredient {
    /// `imageUrl` and `allergens` are additive and read leniently.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            ingredientID: try container.decode(String.self, forKey: .ingredientID),
            name: try container.decode(String.self, forKey: .name),
            category: try container.decode(String.self, forKey: .category),
            pantryStaple: try container.decode(Bool.self, forKey: .pantryStaple),
            amounts: try container.decode([RecipeAmount].self, forKey: .amounts),
            imageURLString: container.decodeLenient(String.self, forKey: .imageURLString),
            allergens: container.decodeLossyArray(String.self, forKey: .allergens))
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
