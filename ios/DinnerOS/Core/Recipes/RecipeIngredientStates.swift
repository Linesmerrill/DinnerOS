import Foundation

/// One ingredient on a recipe's ingredient list as the household cooks it: the line to show,
/// whether it's left out and by which skip, and — for a specialty ingredient the household makes
/// from store ingredients — what that component is made of.
nonisolated struct RecipeIngredientState: Equatable, Sendable, Identifiable {
    let line: IngredientLine
    /// The key a skip for it is made with; `nil` when the server didn't say (an older server),
    /// in which case the ingredient can't be left out from here.
    let ingredientKey: String?
    let leftOut: InstructionLeftOut?
    let component: InstructionComponent?

    var id: String { line.id }
    var isLeftOut: Bool { leftOut != nil }
    var canLeaveOut: Bool { ingredientKey != nil }
    var isComponent: Bool { component != nil }

    /// What the row says about a left-out ingredient.
    var leftOutText: String? {
        switch leftOut?.scope {
        case .recipe?:
            isComponent
                ? String(localized: "Not making this for this dish") : String(localized: "Left out of this dish")
        case .always?:
            isComponent ? String(localized: "Never making this") : String(localized: "Left out of every dish")
        case nil: nil
        default: String(localized: "Left out")
        }
    }

    /// The request that leaves it out: of this recipe ("just this dish"), or of every recipe
    /// ("always"). `nil` when it can't be left out from here.
    func leaveOutRequest(scope: GrocerySkipScope, recipeID: String) -> GrocerySkipRequest? {
        guard let ingredientKey else { return nil }
        switch scope {
        case .recipe: return .leaveOut(ingredientKey: ingredientKey, name: line.name, recipeID: recipeID)
        case .always: return .always(ingredientKey: ingredientKey, name: line.name)
        default: return nil
        }
    }
}

nonisolated enum RecipeIngredientStates {
    /// Joins the recipe's ingredient lines with the server's rendered state for them.
    ///
    /// They're matched by position — the server lists the recipe's ingredients in the same
    /// order — and only when the names agree, so instructions loaded for an older copy of the
    /// recipe can't cross out the wrong line.
    static func make(lines: [IngredientLine], instructions: RecipeInstructions?) -> [RecipeIngredientState] {
        lines.enumerated().map { index, line in
            guard let state = instructions?.ingredient(at: index), state.name == line.name else {
                return RecipeIngredientState(line: line, ingredientKey: nil, leftOut: nil, component: nil)
            }
            return RecipeIngredientState(
                line: line, ingredientKey: state.ingredientKey, leftOut: state.leftOut, component: state.component)
        }
    }
}
