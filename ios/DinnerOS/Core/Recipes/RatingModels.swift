import Foundation

/// A structured reaction to a recipe (`RatingTag` in `api/openapi.yaml`).
///
/// The API accepts only a fixed allowlist. A tag from a newer server decodes as-is and
/// is kept when the rating is saved again, but the app only offers the tags it knows.
nonisolated struct RatingTag: RawRepresentable, Codable, Hashable, Sendable, Identifiable {
    let rawValue: String

    init(rawValue: String) {
        self.rawValue = rawValue
    }

    static let makeAgain = RatingTag(rawValue: "make-again")
    static let neverAgain = RatingTag(rawValue: "never-again")
    static let kidFavorite = RatingTag(rawValue: "kid-favorite")
    static let kidsDisliked = RatingTag(rawValue: "kids-disliked")
    static let tooSpicy = RatingTag(rawValue: "too-spicy")
    static let tooBland = RatingTag(rawValue: "too-bland")
    static let tooMuchWork = RatingTag(rawValue: "too-much-work")
    static let greatLeftovers = RatingTag(rawValue: "great-leftovers")
    static let tooSalty = RatingTag(rawValue: "too-salty")
    static let tooSweet = RatingTag(rawValue: "too-sweet")
    static let tookTooLong = RatingTag(rawValue: "took-too-long")
    static let tooDry = RatingTag(rawValue: "too-dry")
    static let tooSoggy = RatingTag(rawValue: "too-soggy")
    static let portionTooSmall = RatingTag(rawValue: "portion-too-small")
    static let portionTooBig = RatingTag(rawValue: "portion-too-big")

    /// The allowlist in the API's canonical order.
    static let known: [RatingTag] = [
        .makeAgain, .neverAgain, .kidFavorite, .kidsDisliked, .tooSpicy, .tooBland, .tooMuchWork, .greatLeftovers,
    ]

    var id: String { rawValue }

    /// The tag that can't be combined with this one: `make-again` and `never-again`
    /// contradict each other, and the API rejects them together.
    var excluded: RatingTag? {
        switch self {
        case .makeAgain: .neverAgain
        case .neverAgain: .makeAgain
        default: nil
        }
    }

    var title: String {
        switch self {
        case .makeAgain: String(localized: "Make Again")
        case .neverAgain: String(localized: "Never Again")
        case .kidFavorite: String(localized: "Kid Favorite")
        case .kidsDisliked: String(localized: "Kids Disliked")
        case .tooSpicy: String(localized: "Too Spicy")
        case .tooBland: String(localized: "Too Bland")
        case .tooMuchWork: String(localized: "Too Much Work")
        case .greatLeftovers: String(localized: "Great Leftovers")
        default: rawValue.replacingOccurrences(of: "-", with: " ").capitalized
        }
    }
}

/// A recipe's aggregate rating in the household (`HouseholdRating`).
nonisolated struct HouseholdRating: Codable, Hashable, Sendable {
    /// Rounded to two decimals by the API; `nil` when nobody has rated the recipe.
    let average: Double?
    let count: Int

    static let unrated = HouseholdRating(average: nil, count: 0)
}

/// One member's rating of a recipe (`Rating`).
nonisolated struct RecipeRating: Codable, Hashable, Sendable {
    let recipeID: String
    let userID: String
    /// 1 to 5.
    let score: Int
    /// Empty when there is none.
    let comment: String
    /// Distinct, in canonical order.
    let tags: [RatingTag]
    let createdAt: Date
    let updatedAt: Date
    /// The ingredients that didn't work, from the follow-up questions; `nil` from an older server.
    var misses: [RatingMiss]? = nil

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
        case userID = "userId"
        case score, comment, tags, createdAt, updatedAt, misses
    }
}

/// One ingredient that didn't work, and what was wrong with it.
nonisolated struct RatingMiss: Codable, Hashable, Sendable {
    let ingredientKey: String
    let name: String
    /// The part of a made ingredient ("Onions"), or empty.
    var part: String = ""
    let reason: String

    static let didntLike = "didnt-like"
    static let product = "product"

    private enum CodingKeys: String, CodingKey {
        case ingredientKey, name, part, reason
    }

    init(ingredientKey: String, name: String, part: String = "", reason: String) {
        self.ingredientKey = ingredientKey
        self.name = name
        self.part = part
        self.reason = reason
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        ingredientKey = (try? container.decode(String.self, forKey: .ingredientKey)) ?? ""
        name = try container.decode(String.self, forKey: .name)
        part = (try? container.decode(String.self, forKey: .part)) ?? ""
        reason = try container.decode(String.self, forKey: .reason)
    }
}

/// A rating with the rater's display name (`MemberRating`).
nonisolated struct MemberRating: Decodable, Hashable, Sendable, Identifiable {
    let rating: RecipeRating
    /// May be empty when the member has no display name.
    let displayName: String

    var id: String { rating.userID }

    /// The display name, or a placeholder when none was provided.
    var name: String {
        displayName.isEmpty ? String(localized: "Unnamed member") : displayName
    }

    init(rating: RecipeRating, displayName: String) {
        self.rating = rating
        self.displayName = displayName
    }

    /// The API sends the rating's fields and `displayName` in one flat object.
    init(from decoder: any Decoder) throws {
        rating = try RecipeRating(from: decoder)
        displayName = try decoder.container(keyedBy: CodingKeys.self).decode(String.self, forKey: .displayName)
    }

    private enum CodingKeys: String, CodingKey {
        case displayName
    }
}

/// Response to `GET .../recipes/{recipeId}/ratings`.
nonisolated struct RatingListResponse: Decodable, Equatable, Sendable {
    let householdRating: HouseholdRating
    /// Most recently updated first.
    let items: [MemberRating]
}

/// Body of `PUT .../recipes/{recipeId}/rating`. An empty comment is omitted.
nonisolated struct RateRecipeRequest: Encodable, Equatable, Sendable {
    let score: Int
    let comment: String
    let tags: [RatingTag]
    /// `nil` keeps the saved ones; empty clears them.
    var misses: [RatingMiss]? = nil

    private enum CodingKeys: String, CodingKey {
        case score, comment, tags, misses
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(score, forKey: .score)
        if !comment.isEmpty {
            try container.encode(comment, forKey: .comment)
        }
        try container.encode(tags, forKey: .tags)
        try container.encodeIfPresent(misses, forKey: .misses)
    }
}

/// The API's rating limits, mirrored for input validation.
nonisolated enum RatingLimits {
    static let scores = 1...5
    /// Counted in Unicode scalars after trimming, like the API.
    static let maxCommentLength = 500
}

/// The rating being edited on a recipe screen.
///
/// Enforces the API's rules as the user edits, so Save is only offered for a rating the
/// server will accept: a score from 1 to 5, a comment within the limit, and never both
/// `make-again` and `never-again`.
nonisolated struct RatingDraft: Equatable, Sendable {
    /// 0 until the user picks a score.
    var score: Int
    var comment: String
    /// Distinct, in canonical order, never containing a tag and its exclusion.
    private(set) var tags: [RatingTag]
    /// What didn't work; `nil` leaves the saved answers as they are.
    var misses: [RatingMiss]? = nil

    init(score: Int = 0, comment: String = "", tags: [RatingTag] = []) {
        self.score = score
        self.comment = comment
        self.tags = []
        for tag in tags where !contains(tag) {
            insert(tag)
        }
    }

    /// Starts from the user's saved rating, or an empty draft.
    init(rating: RecipeRating?) {
        self.init(score: rating?.score ?? 0, comment: rating?.comment ?? "", tags: rating?.tags ?? [])
    }

    func contains(_ tag: RatingTag) -> Bool {
        tags.contains(tag)
    }

    /// Selects or deselects `tag`. Selecting one of `make-again` and `never-again`
    /// deselects the other.
    mutating func toggle(_ tag: RatingTag) {
        if contains(tag) {
            tags.removeAll { $0 == tag }
        } else {
            insert(tag)
        }
    }

    private mutating func insert(_ tag: RatingTag) {
        if let excluded = tag.excluded {
            tags.removeAll { $0 == excluded }
        }
        tags.append(tag)
        // Known tags in canonical order; unknown ones keep their place at the end.
        tags.sort { Self.order(of: $0) < Self.order(of: $1) }
    }

    private static func order(of tag: RatingTag) -> Int {
        RatingTag.known.firstIndex(of: tag) ?? RatingTag.known.count
    }

    var trimmedComment: String {
        comment.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var commentLength: Int {
        trimmedComment.unicodeScalars.count
    }

    var isCommentTooLong: Bool {
        commentLength > RatingLimits.maxCommentLength
    }

    var canSave: Bool {
        RatingLimits.scores.contains(score) && !isCommentTooLong
    }

    /// Whether saving would change `rating` (or create one).
    func differs(from rating: RecipeRating?) -> Bool {
        guard let rating else { return true }
        return score != rating.score || trimmedComment != rating.comment || tags != rating.tags
    }

    var request: RateRecipeRequest {
        RateRecipeRequest(score: score, comment: trimmedComment, tags: tags, misses: misses)
    }
}

/// Display text for ratings.
nonisolated enum RatingFormat {
    /// One decimal, for example "4.5".
    static func average(_ value: Double, locale: Locale = .autoupdatingCurrent) -> String {
        value.formatted(.number.precision(.fractionLength(1)).locale(locale))
    }

    /// For example "4.5 ★ from 2", or `nil` when nobody has rated the recipe.
    static func summary(_ rating: HouseholdRating, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let average = rating.average, rating.count > 0 else { return nil }
        return String(localized: "\(self.average(average, locale: locale)) ★ from \(rating.count)")
    }

    /// For VoiceOver, for example "Rated 4.5 out of 5 by 2 members".
    static func accessibilitySummary(_ rating: HouseholdRating, locale: Locale = .autoupdatingCurrent) -> String {
        guard let average = rating.average, rating.count > 0 else { return String(localized: "Not rated yet") }
        let value = self.average(average, locale: locale)
        return rating.count == 1
            ? String(localized: "Rated \(value) out of 5 by 1 member")
            : String(localized: "Rated \(value) out of 5 by \(rating.count) members")
    }
}

/// "What could have been better?", from the server (`GET .../rating-questions`): kinds of problem
/// with their choices, and the meal's ingredients as cooked.
nonisolated struct RatingQuestions: Decodable, Equatable, Sendable {
    struct Category: Decodable, Equatable, Sendable, Identifiable {
        let code: String
        let title: String
        let symbol: String
        let options: [Option]
        var id: String { code }
        /// The category that asks about one ingredient instead of offering options.
        var isIngredients: Bool { code == "ingredients" }
    }

    struct Option: Decodable, Equatable, Sendable, Identifiable {
        let tag: String
        let title: String
        var id: String { tag }
    }

    struct Ingredient: Decodable, Equatable, Sendable, Identifiable {
        let ingredientKey: String
        let name: String
        let parts: [String]
        var id: String { ingredientKey + name }
    }

    struct Reason: Decodable, Equatable, Sendable, Identifiable {
        let reason: String
        let title: String
        let detail: String
        var id: String { reason }
    }

    let title: String
    let categories: [Category]
    let ingredients: [Ingredient]
    let reasons: [Reason]
}
