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

/// Why a walk of the order history stopped.
///
/// It is the difference between "that is all there is" and "there is more, come back": a
/// household with four years of history cannot be read in one sitting, so the walk says where
/// it got to and the server remembers it (`docs/meal-kit-import.md`).
nonisolated enum MealKitHarvestStop: String, Equatable, Sendable {
    /// The walk hit its own page cap. There is more history behind it.
    case cap
    /// A page came back with no delivered weeks: the start of the account's history.
    case empty
    /// The walk stopped moving backwards — the end of the history, as far as it can tell.
    case end
    /// It reached weeks already imported. A catch-up pass for new deliveries ends this way, and
    /// it says nothing about whether the oldest history is finished.
    case caughtUp = "caught_up"
    /// A reason from a newer script this build doesn't know. Nothing is concluded from it, and
    /// it is not sent to the server.
    case unknown = ""

    init(code: String) {
        self = MealKitHarvestStop(rawValue: code) ?? .unknown
    }

    /// Whether the member should be told there is history this harvest did not reach.
    var moreToFetch: Bool { self == .cap }
}

/// What reading a household's order history in the web view produced.
///
/// This is the whole of what leaves the device: recipe ids, their public page URLs, the weeks
/// they were delivered, and where the walk stopped. No token, no cookie, no account profile —
/// the sign-in stays in the web view and is thrown away with it.
nonisolated struct MealKitHarvest: Equatable, Sendable {
    let recipes: [MealKitOrderedRecipe]
    /// How many pages of history were walked, and how many delivered weeks were seen. Shown as
    /// progress and useful when a member says "it only found some of them".
    let pages: Int
    let weeks: Int
    /// The oldest ISO week this walk reached. The server stores it, and the next harvest picks
    /// up the week before it instead of walking back from today again.
    let earliestWeek: String
    /// The newest ISO week it saw, or empty when the walk could not reach today's end of the
    /// history — which is why the server must not move its ceiling on this pass.
    let latestWeek: String
    /// Why it stopped.
    let stopped: MealKitHarvestStop

    init(
        recipes: [MealKitOrderedRecipe], pages: Int = 0, weeks: Int = 0,
        earliestWeek: String = "", latestWeek: String = "", stopped: MealKitHarvestStop = .unknown
    ) {
        self.recipes = recipes
        self.pages = pages
        self.weeks = weeks
        self.earliestWeek = earliestWeek
        self.latestWeek = latestWeek
        self.stopped = stopped
    }

    var isEmpty: Bool { recipes.isEmpty }

    /// Whether there is history this walk did not reach.
    var moreToFetch: Bool { stopped.moreToFetch }
}

/// Why reading the order history stopped. Each case is something the sign-in screen can say
/// plainly; none of them ever carries page content.
nonisolated enum MealKitHarvestFailure: String, Equatable, Sendable {
    /// The session died mid-walk (a 403). Signing in again fixes it.
    case forbidden
    /// The account has no past deliveries to import.
    case empty
    /// There was nothing new: every delivery this walk reached is already in the library. Not a
    /// failure at all — it is what a catch-up pass usually finds — but it is not a harvest to
    /// send either, because there would be nothing in it.
    case nothingNew
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
    ///
    /// `catchingUp` says this walk was only looking for new deliveries on a history already
    /// read, so finding nothing means "up to date" rather than "you have never ordered".
    init(json: String, service: MealKitService, catchingUp: Bool = false) {
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
            self = .failed(catchingUp ? .nothingNew : .empty)
            return
        }
        self = .harvested(
            MealKitHarvest(
                recipes: kept, pages: payload.pages, weeks: payload.weeks,
                earliestWeek: payload.earliestWeek ?? "", latestWeek: payload.latestWeek ?? "",
                stopped: MealKitHarvestStop(code: payload.stopped ?? "")))
    }

    private struct ScriptSuccess: Decodable {
        let recipes: [MealKitOrderedRecipe]
        let pages: Int
        let weeks: Int
        /// Absent in a payload from an older script; it simply teaches the server nothing.
        let earliestWeek: String?
        let latestWeek: String?
        let stopped: String?
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
