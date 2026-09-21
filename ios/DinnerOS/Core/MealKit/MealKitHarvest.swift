import Foundation

/// One recipe of a household's own meal-kit order history, as read in the member's browser
/// session and as sent to our API (`docs/meal-kit-import.md`).
nonisolated struct MealKitOrderedRecipe: Equatable, Sendable, Codable {
    /// The meal kit's id for the recipe.
    let sourceRecipeID: String
    /// A label to show before the recipe page itself is read.
    let name: String
    /// The public recipe page. The server checks it against its own allow-list again.
    let url: String
    /// The ISO weeks it was delivered, e.g. `2026-W38`.
    let weeks: [String]
    /// A side or extra rather than a main meal.
    let isAddon: Bool

    private enum CodingKeys: String, CodingKey {
        case sourceRecipeID = "sourceRecipeId"
        case name, url, weeks, isAddon
    }

    init(sourceRecipeID: String, name: String = "", url: String = "", weeks: [String] = [], isAddon: Bool = false) {
        self.sourceRecipeID = sourceRecipeID
        self.name = name
        self.url = url
        self.weeks = weeks
        self.isAddon = isAddon
    }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            sourceRecipeID: try c.decode(String.self, forKey: .sourceRecipeID),
            name: try c.decodeIfPresent(String.self, forKey: .name) ?? "",
            url: try c.decodeIfPresent(String.self, forKey: .url) ?? "",
            weeks: try c.decodeIfPresent([String].self, forKey: .weeks) ?? [],
            isAddon: try c.decodeIfPresent(Bool.self, forKey: .isAddon) ?? false)
    }
}

/// What reading a household's order history in the web view produced.
///
/// This is the whole of what leaves the device: recipe ids, their public page URLs, the weeks
/// they were delivered. No token, no cookie, no account profile — the sign-in stays in the web
/// view and is thrown away with it.
nonisolated struct MealKitHarvest: Equatable, Sendable {
    let recipes: [MealKitOrderedRecipe]
    /// How many pages of history were walked, and how many delivered weeks were seen. Shown as
    /// progress and useful when a member says "it only found some of them".
    let pages: Int
    let weeks: Int

    init(recipes: [MealKitOrderedRecipe], pages: Int = 0, weeks: Int = 0) {
        self.recipes = recipes
        self.pages = pages
        self.weeks = weeks
    }

    var isEmpty: Bool { recipes.isEmpty }
}

/// Why reading the order history stopped. Each case is something the sign-in screen can say
/// plainly; none of them ever carries page content.
nonisolated enum MealKitHarvestFailure: String, Equatable, Sendable {
    /// The session died mid-walk (a 403). Signing in again fixes it.
    case forbidden
    /// The account has no past deliveries to import.
    case empty
    /// The responses were not the shape this build reads: their API changed.
    case unreadable
    /// Something else went wrong in the page — a network drop, usually.
    case unavailable

    init(code: String) {
        self = MealKitHarvestFailure(rawValue: code) ?? .unavailable
    }
}

/// The result of running the harvest script: a history, or a reason there isn't one.
nonisolated enum MealKitHarvestResult: Equatable, Sendable {
    case harvested(MealKitHarvest)
    case failed(MealKitHarvestFailure)

    /// Reads the JSON the harvest script returns, keeping only entries this build would send.
    ///
    /// The script ran against someone else's page, so its output is untrusted too: an id that is
    /// not a recipe id, or a URL that does not point at the service's own recipe pages, is
    /// dropped here as well as on the server. Anything unreadable is `.unreadable` rather than a
    /// crash or a half-read list.
    init(json: String, service: MealKitService) {
        guard let data = json.data(using: .utf8) else {
            self = .failed(.unreadable)
            return
        }
        let decoder = JSONDecoder()
        if let failure = try? decoder.decode(ScriptFailure.self, from: data) {
            self = .failed(MealKitHarvestFailure(code: failure.error.code))
            return
        }
        guard let payload = try? decoder.decode(ScriptSuccess.self, from: data) else {
            self = .failed(.unreadable)
            return
        }
        let kept = payload.recipes.filter { service.canImport($0) }
        if kept.isEmpty {
            self = .failed(.empty)
            return
        }
        self = .harvested(MealKitHarvest(recipes: kept, pages: payload.pages, weeks: payload.weeks))
    }

    private struct ScriptSuccess: Decodable {
        let recipes: [MealKitOrderedRecipe]
        let pages: Int
        let weeks: Int
    }

    private struct ScriptFailure: Decodable {
        struct Detail: Decodable { let code: String }
        let error: Detail
    }
}

nonisolated extension MealKitService {
    /// Whether a harvested entry is one we would actually send: the service's own id shape, and
    /// a URL on the service's own recipe pages.
    func canImport(_ recipe: MealKitOrderedRecipe) -> Bool {
        guard recipe.sourceRecipeID.count == 24,
            recipe.sourceRecipeID.allSatisfy({ $0.isHexDigit && !$0.isUppercase })
        else { return false }
        return recipe.url.hasPrefix(recipePagePrefix)
    }

    /// The only prefix a recipe page URL may have.
    var recipePagePrefix: String {
        switch self {
        case .helloFresh: "https://www.hellofresh.com/recipes/"
        }
    }
}
