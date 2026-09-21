import Foundation

/// "Try Something Similar": meals like a planned one, and the result of picking one
/// (`docs/autopilot.md#try-something-similar`).

/// One replacement offered for a planned meal.
nonisolated struct MealAlternative: Decodable, Hashable, Sendable, Identifiable {
    let recipe: AutopilotSlotRecipe
    /// What the meal would be cooked in: the entry's servings, or the nearest size this
    /// recipe is authored in.
    let servings: Int
    /// `nil` when the recipe doesn't say.
    let cookMinutes: Int?
    let timeBand: AutopilotTimeBand?
    /// 0–1: how much this resembles the meal being replaced.
    let similarity: Double
    let score: Double
    /// Why this one, most important first ("Also Thai", "30 min").
    let reasons: [AutopilotText]

    var id: String { recipe.id }

    /// The reasons joined for display, for example "Also Thai · 30 min".
    var reasonText: String {
        reasons.map(\.text).filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// What a planned meal could be swapped for.
nonisolated struct MealAlternatives: Decodable, Hashable, Sendable {
    let entryID: String
    /// `YYYY-Www`, as every other plan response carries it.
    let week: String
    let day: PlanDay?
    /// The meal being replaced.
    let recipe: AutopilotSlotRecipe
    let servings: Int
    /// Most like the planned meal first. Empty when nothing else fits the day.
    let alternatives: [MealAlternative]
    /// `no_similar`, `no_alternatives`, or `seen_all`.
    let messages: [AutopilotText]
    let modelVersion: String?

    /// The first note to show under the list, if any.
    var noticeText: String? {
        messages.first(where: { !$0.text.isEmpty })?.text
    }

    private enum CodingKeys: String, CodingKey {
        case entryID = "entryId"
        case week, day, recipe, servings, alternatives, messages, modelVersion
    }
}

nonisolated struct PlannedMealSwapRequest: Encodable, Sendable {
    let recipeID: String

    private enum CodingKeys: String, CodingKey {
        case recipeID = "recipeId"
    }
}

/// The week after a planned meal was replaced.
nonisolated struct PlannedMealSwapResult: Decodable, Sendable {
    let plan: Plan
    /// The swapped entry, with its new recipe.
    let entry: PlanEntry
    /// The meal it replaced.
    let previousRecipe: AutopilotSlotRecipe
}
