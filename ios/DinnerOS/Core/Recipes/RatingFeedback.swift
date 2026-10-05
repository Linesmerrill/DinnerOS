import Foundation

/// The answers in "What could have been better?": the choices picked under each kind of problem,
/// and what was wrong with each ingredient. Starts from the member's saved rating, so reopening it
/// shows what they said before. Pure, so what gets saved is tested without the sheet.
nonisolated struct RatingFeedback: Equatable, Sendable {
    /// Tags picked from the questions.
    private(set) var tags: Set<String>
    /// Keyed by ingredient key and name; one answer per ingredient.
    private(set) var misses: [String: RatingMiss]
    /// The tags the questions offer; any other saved tag (Make Again, Kid Favorite) is kept as is.
    private let offered: Set<String>
    private let saved: RatingDraft

    init(rating: RecipeRating?, questions: RatingQuestions) {
        offered = Set(questions.categories.flatMap { $0.options.map(\.tag) })
        saved = RatingDraft(rating: rating)
        tags = Set((rating?.tags ?? []).map(\.rawValue)).intersection(offered)
        misses = Dictionary(
            (rating?.misses ?? []).map { (Self.key($0.ingredientKey, $0.name), $0) }, uniquingKeysWith: { a, _ in a })
    }

    static func key(_ ingredientKey: String, _ name: String) -> String { ingredientKey + "|" + name }

    func isPicked(_ tag: String) -> Bool { tags.contains(tag) }

    mutating func toggle(_ tag: String) {
        if tags.contains(tag) { tags.remove(tag) } else { tags.insert(tag) }
    }

    /// How many answers sit under a category, for its badge.
    func count(in category: RatingQuestions.Category, ingredients: [RatingQuestions.Ingredient]) -> Int {
        category.isIngredients ? misses.count : category.options.filter { tags.contains($0.tag) }.count
    }

    func miss(for ingredient: RatingQuestions.Ingredient) -> RatingMiss? {
        misses[Self.key(ingredient.ingredientKey, ingredient.name)]
    }

    /// Picks what was wrong with an ingredient; picking the same reason again clears it.
    mutating func setReason(_ reason: String, for ingredient: RatingQuestions.Ingredient) {
        let key = Self.key(ingredient.ingredientKey, ingredient.name)
        if misses[key]?.reason == reason {
            misses[key] = nil
        } else {
            misses[key] = RatingMiss(
                ingredientKey: ingredient.ingredientKey, name: ingredient.name, part: misses[key]?.part ?? "",
                reason: reason)
        }
    }

    /// Narrows an ingredient's answer to one part ("Onions"); the same part again widens it back.
    mutating func setPart(_ part: String, for ingredient: RatingQuestions.Ingredient) {
        let key = Self.key(ingredient.ingredientKey, ingredient.name)
        let current = misses[key]
        misses[key] = RatingMiss(
            ingredientKey: ingredient.ingredientKey, name: ingredient.name, part: current?.part == part ? "" : part,
            reason: current?.reason ?? RatingMiss.didntLike)
    }

    var isEmpty: Bool { tags.isEmpty && misses.isEmpty }

    /// Ingredients whose product should be re-chosen next time.
    var productKeys: [String] {
        misses.values.filter { $0.reason == RatingMiss.product }.map(\.ingredientKey).filter { !$0.isEmpty }.sorted()
    }

    /// The rating to save: the score kept, other saved tags kept, these answers in place of the old.
    func draft(score: Int) -> RatingDraft {
        var draft = RatingDraft(
            score: score, comment: saved.comment,
            tags: saved.tags.filter { !offered.contains($0.rawValue) } + tags.sorted().map(RatingTag.init(rawValue:)))
        draft.misses = misses.values.sorted { ($0.name, $0.part) < ($1.name, $1.part) }
        return draft
    }
}
