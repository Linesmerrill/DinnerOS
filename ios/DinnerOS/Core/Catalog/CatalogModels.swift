import Foundation

/// A recipe in the global catalog, as a browse or search result
/// (`CatalogSummary` in `api/openapi.yaml`).
///
/// The catalog belongs to no household, so a summary carries no rating, no
/// order history, and no notes — only the recipe itself, plus whether this
/// household already has it.
nonisolated struct CatalogSummary: Decodable, Hashable, Sendable, Identifiable {
    let id: String
    /// The catalog's identity for this recipe (`source:sourceRecipeId`, or a
    /// fingerprint). Two households' copies of one meal-kit recipe share it.
    let catalogKey: String
    /// Where the recipe came from, for example `hellofresh`.
    let source: String
    let name: String
    let headline: String?
    let imageURLString: String?
    let isAddon: Bool
    let totalMinutes: Int?
    let cookMinutes: Int?
    let timeBand: AutopilotTimeBand?
    let calories: Int?
    let proteinGrams: Int?
    let cuisines: [String]
    let tags: [String]
    /// True when the household already has this recipe.
    let inLibrary: Bool
    /// The household's own copy, when it has one.
    let libraryRecipeID: String?
    /// Why discovery ranked it here ("You like Thai"). Empty for search.
    let reasons: [String]

    /// `nil` when missing or not a valid URL; catalog data isn't trusted to be well formed.
    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }

    /// The time to show, falling back to the source's total for an older server.
    var displayMinutes: Int? {
        RecipeFormat.displayMinutes(cook: cookMinutes, prep: nil, total: totalMinutes)
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case id, catalogKey, source, name, headline
        case imageURLString = "imageUrl"
        case isAddon, totalMinutes, cookMinutes, timeBand, calories, proteinGrams, cuisines, tags, inLibrary
        case libraryRecipeID = "libraryRecipeId"
        case reasons
    }
}

nonisolated extension CatalogSummary {
    /// Fields added after this client shipped decode leniently, as recipe summaries do.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try container.decode(String.self, forKey: .id),
            catalogKey: try container.decodeIfPresent(String.self, forKey: .catalogKey) ?? "",
            source: try container.decodeIfPresent(String.self, forKey: .source) ?? "",
            name: try container.decode(String.self, forKey: .name),
            headline: try container.decodeIfPresent(String.self, forKey: .headline),
            imageURLString: try container.decodeIfPresent(String.self, forKey: .imageURLString),
            isAddon: try container.decodeIfPresent(Bool.self, forKey: .isAddon) ?? false,
            totalMinutes: container.decodeLenientInt(forKey: .totalMinutes),
            cookMinutes: container.decodeLenientInt(forKey: .cookMinutes),
            timeBand: container.decodeLenient(AutopilotTimeBand.self, forKey: .timeBand),
            calories: container.decodeLenientInt(forKey: .calories),
            proteinGrams: container.decodeLenientInt(forKey: .proteinGrams),
            cuisines: try container.decodeIfPresent([String].self, forKey: .cuisines) ?? [],
            tags: try container.decodeIfPresent([String].self, forKey: .tags) ?? [],
            inLibrary: try container.decodeIfPresent(Bool.self, forKey: .inLibrary) ?? false,
            libraryRecipeID: try container.decodeIfPresent(String.self, forKey: .libraryRecipeID),
            reasons: try container.decodeIfPresent([String].self, forKey: .reasons) ?? [])
    }
}

/// One page of catalog results.
nonisolated struct CatalogListPage: Decodable, Equatable, Sendable {
    let items: [CatalogSummary]
    /// Send back as `cursor` for the next page; `nil` on the last page.
    let nextCursor: String?
    /// How many entries matched, capped by the server's paging limit.
    var total: Int = 0

    fileprivate enum CodingKeys: String, CodingKey {
        case items, nextCursor, total
    }
}

/// A catalog recipe in full (`GET .../catalog/recipes/{id}`).
nonisolated struct CatalogRecipe: Decodable, Equatable, Sendable, Identifiable {
    let id: String
    let catalogKey: String
    let source: String
    let name: String
    let headline: String?
    let description: String?
    let imageURLString: String?
    let sourceURLString: String?
    let isAddon: Bool
    let servings: [Int]
    let prepMinutes: Int?
    let totalMinutes: Int?
    let cookMinutes: Int?
    let cuisines: [String]
    let tags: [String]
    let allergens: [String]
    let ingredients: [CatalogRecipeIngredient]
    let steps: [RecipeStep]
    let inLibrary: Bool
    let libraryRecipeID: String?

    var imageURL: URL? { imageURLString.flatMap { URL(string: $0) } }
    var sourceURL: URL? { sourceURLString.flatMap { URL(string: $0) } }

    var displayMinutes: Int? {
        RecipeFormat.displayMinutes(cook: cookMinutes, prep: prepMinutes, total: totalMinutes)
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case id, catalogKey, source, name, headline, description
        case imageURLString = "imageUrl"
        case sourceURLString = "sourceUrl"
        case isAddon, servings, prepMinutes, totalMinutes, cookMinutes, cuisines, tags, allergens
        case ingredients, steps, inLibrary
        case libraryRecipeID = "libraryRecipeId"
    }
}

/// An ingredient line on a catalog recipe. It carries no grocery category: the
/// category is read from the ingredient catalog once the recipe is in a library.
nonisolated struct CatalogRecipeIngredient: Decodable, Equatable, Sendable, Identifiable {
    let ingredientID: String
    let name: String
    let pantryStaple: Bool
    let amounts: [RecipeAmount]

    var id: String { ingredientID + "/" + name }

    fileprivate enum CodingKeys: String, CodingKey {
        case ingredientID = "ingredientId"
        case name, pantryStaple, amounts
    }
}

/// Result of adding a catalog recipe to the household's library.
nonisolated struct AddToLibraryResult: Decodable, Equatable, Sendable {
    /// The household's own copy.
    let recipeID: String
    /// False when the household already had it, which is not an error.
    let created: Bool

    fileprivate enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case created
    }
}

/// What the catalog browse and search screens ask for.
nonisolated struct CatalogQuery: Equatable, Sendable {
    var search: String = ""
    var cuisine: String?
    var tag: String?

    static let maxSearchLength = 100

    /// The search text as the server sees it: trimmed and length-capped.
    var normalizedSearch: String {
        String(search.trimmingCharacters(in: .whitespacesAndNewlines).prefix(Self.maxSearchLength))
    }

    /// Whether two queries would produce the same request.
    func isSameRequest(as other: CatalogQuery) -> Bool {
        normalizedSearch == other.normalizedSearch && cuisine == other.cuisine && tag == other.tag
    }

    var isSearching: Bool { !normalizedSearch.isEmpty }
}
