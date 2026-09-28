import Foundation

/// One step's share of an ingredient the recipe uses in more than one step: "2 tbsp" of the
/// butter, for step 1.
nonisolated struct CookPart: Equatable, Sendable, Identifiable {
    let id: String
    let amountText: String
    let stepIndex: Int
}

/// An ingredient on the cooking checklist: the whole amount, and its per-step shares when the
/// recipe splits it across steps (butter melted in step 1 and stirred in at step 3).
nonisolated struct CookIngredient: Equatable, Sendable, Identifiable {
    let id: String
    let name: String
    let amountText: String?
    let imageURL: URL?
    let isLeftOut: Bool
    /// Empty unless two or more steps each say how much of it they use.
    let parts: [CookPart]
}

/// Builds the cooking checklist from the recipe and its rendered steps.
nonisolated enum CookChecklist {
    static func make(recipe: Recipe, servings: Int, instructions: RecipeInstructions?) -> [CookIngredient] {
        let lines = recipe.ingredientLines(servings: servings)
        let states = RecipeIngredientStates.make(lines: lines, instructions: instructions)
        return zip(recipe.ingredients, states).map { ingredient, state in
            var parts: [CookPart] = []
            for step in instructions?.steps ?? [] {
                for segment in step.segments where segment.isIngredient && segment.part && !segment.leftOut {
                    guard let amount = segment.amount, matches(segment, ingredient) else { continue }
                    let id = "\(state.id)#\(step.index)-\(parts.count)"
                    parts.append(CookPart(id: id, amountText: amount.text, stepIndex: step.index))
                }
            }
            return CookIngredient(
                id: state.id, name: state.line.name, amountText: state.line.amount, imageURL: state.line.imageURL,
                isLeftOut: state.isLeftOut, parts: parts.count >= 2 ? parts : [])
        }
    }

    private static func matches(_ segment: InstructionSegment, _ ingredient: RecipeIngredient) -> Bool {
        if let id = segment.ingredientID, !id.isEmpty, !ingredient.ingredientID.isEmpty {
            return id == ingredient.ingredientID
        }
        return segment.name?.caseInsensitiveCompare(ingredient.name) == .orderedSame
    }
}

/// What the cook has ticked off and which step they're on, per recipe, for as long as the
/// cooking screen is open. Switching dishes keeps each dish's progress.
@MainActor @Observable
final class CookSession {
    private(set) var checked: [String: Set<String>] = [:]
    private(set) var currentStep: [String: Int] = [:]

    func isChecked(_ id: String, recipe: String) -> Bool { checked[recipe, default: []].contains(id) }

    /// Ticks an ingredient, and with it every share of it; unticks the same way.
    func toggle(_ ingredient: CookIngredient, recipe: String) {
        var set = checked[recipe, default: []]
        let ids = [ingredient.id] + ingredient.parts.map(\.id)
        if set.contains(ingredient.id) {
            set.subtract(ids)
        } else {
            set.formUnion(ids)
        }
        checked[recipe] = set
    }

    /// Ticks one share; the ingredient is ticked once every share is.
    func toggle(_ part: CookPart, of ingredient: CookIngredient, recipe: String) {
        var set = checked[recipe, default: []]
        if set.contains(part.id) {
            set.remove(part.id)
        } else {
            set.insert(part.id)
        }
        if ingredient.parts.allSatisfy({ set.contains($0.id) }) {
            set.insert(ingredient.id)
        } else {
            set.remove(ingredient.id)
        }
        checked[recipe] = set
    }

    func current(recipe: String) -> Int? { currentStep[recipe] }

    /// Tapping the step you're on again clears it.
    func tapStep(_ index: Int, recipe: String) {
        currentStep[recipe] = currentStep[recipe] == index ? nil : index
    }
}
