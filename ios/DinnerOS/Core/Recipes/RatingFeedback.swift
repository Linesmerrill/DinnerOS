import Foundation

/// The answers in "What could have been better?": the choices picked under each kind of problem,
/// and everything that was wrong with each ingredient. Starts from the member's saved rating, so reopening it
/// shows what they said before. Pure, so what gets saved is tested without the sheet.
nonisolated struct RatingFeedback: Equatable, Sendable {
    /// Tags picked from the questions.
    private(set) var tags: Set<String>
    /// Every ingredient answer, several per ingredient ("Not enough" and "The product I bought"),
    /// one per reason.
    private(set) var misses: [RatingMiss]
    /// The tags the questions offer; any other saved tag (Make Again, Kid Favorite) is kept as is.
    private let offered: Set<String>
    private let saved: RatingDraft

    init(rating: RecipeRating?, questions: RatingQuestions) {
        offered = Set(questions.categories.flatMap { $0.options.map(\.tag) })
        saved = RatingDraft(rating: rating)
        tags = Set((rating?.tags ?? []).map(\.rawValue)).intersection(offered)
        var misses: [RatingMiss] = []
        for miss in rating?.misses ?? [] where !misses.contains(miss) { misses.append(miss) }
        self.misses = misses
    }

    /// "Too much" and "Not enough" can't both be true of one ingredient.
    private static let opposites: [String: String] = ["too-much": "too-little", "too-little": "too-much"]

    private static func isFor(_ ingredient: RatingQuestions.Ingredient) -> (RatingMiss) -> Bool {
        { $0.ingredientKey == ingredient.ingredientKey && $0.name == ingredient.name }
    }

    func isPicked(_ tag: String) -> Bool { tags.contains(tag) }

    mutating func toggle(_ tag: String) {
        if tags.contains(tag) { tags.remove(tag) } else { tags.insert(tag) }
    }

    /// How many answers sit under a category, for its badge: ingredients count once each.
    func count(in category: RatingQuestions.Category, ingredients: [RatingQuestions.Ingredient]) -> Int {
        category.isIngredients
            ? Set(misses.map { $0.ingredientKey + "|" + $0.name }).count
            : category.options.filter { tags.contains($0.tag) }.count
    }

    /// What was said about an ingredient, in the order it was picked.
    func misses(for ingredient: RatingQuestions.Ingredient) -> [RatingMiss] {
        misses.filter(Self.isFor(ingredient))
    }

    func reasons(for ingredient: RatingQuestions.Ingredient) -> Set<String> {
        Set(misses(for: ingredient).map(\.reason))
    }

    /// The part an ingredient's answers are about ("Onions"), or empty for all of it.
    func part(for ingredient: RatingQuestions.Ingredient) -> String {
        misses(for: ingredient).first?.part ?? ""
    }

    /// Adds a reason for an ingredient, or takes it away when it's already picked. Picking "Too
    /// much" takes away "Not enough", and the other way around.
    mutating func toggleReason(_ reason: String, for ingredient: RatingQuestions.Ingredient) {
        let mine = Self.isFor(ingredient)
        if misses.contains(where: { mine($0) && $0.reason == reason }) {
            misses.removeAll { mine($0) && $0.reason == reason }
            return
        }
        let part = part(for: ingredient)
        if let opposite = Self.opposites[reason] {
            misses.removeAll { mine($0) && $0.reason == opposite }
        }
        misses.append(
            RatingMiss(ingredientKey: ingredient.ingredientKey, name: ingredient.name, part: part, reason: reason))
    }

    /// Narrows an ingredient's answers to one part ("Onions"); the same part again widens them
    /// back. With nothing picked yet, it means the part wasn't liked.
    mutating func setPart(_ part: String, for ingredient: RatingQuestions.Ingredient) {
        let mine = Self.isFor(ingredient)
        let newPart = self.part(for: ingredient) == part ? "" : part
        if !misses.contains(where: mine) {
            misses.append(
                RatingMiss(
                    ingredientKey: ingredient.ingredientKey, name: ingredient.name, part: newPart,
                    reason: RatingMiss.didntLike))
            return
        }
        misses = misses.map { miss in
            guard mine(miss) else { return miss }
            return RatingMiss(ingredientKey: miss.ingredientKey, name: miss.name, part: newPart, reason: miss.reason)
        }
    }

    /// Takes away everything said about an ingredient.
    mutating func clear(_ ingredient: RatingQuestions.Ingredient) {
        misses.removeAll(where: Self.isFor(ingredient))
    }

    var isEmpty: Bool { tags.isEmpty && misses.isEmpty }

    /// Ingredients whose product should be re-chosen next time.
    var productKeys: [String] {
        Array(Set(misses.filter { $0.reason == RatingMiss.product }.map(\.ingredientKey).filter { !$0.isEmpty }))
            .sorted()
    }

    /// The rating to save: the score kept, other saved tags kept, these answers in place of the old.
    func draft(score: Int) -> RatingDraft {
        var draft = RatingDraft(
            score: score, comment: saved.comment,
            tags: saved.tags.filter { !offered.contains($0.rawValue) } + tags.sorted().map(RatingTag.init(rawValue:)))
        draft.misses = misses.sorted { ($0.name, $0.part, $0.reason) < ($1.name, $1.part, $1.reason) }
        return draft
    }
}
