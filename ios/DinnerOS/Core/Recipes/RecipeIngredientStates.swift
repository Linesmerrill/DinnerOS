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
        case .recipe? where leftOut?.isOnce == true:
            isComponent ? String(localized: "Not making this this time") : String(localized: "Skipped this time")
        case .recipe?:
            isComponent
                ? String(localized: "Not making this for this dish") : String(localized: "Skipped in this dish")
        case .always?:
            isComponent ? String(localized: "Never making this") : String(localized: "Skipped in every dish")
        case nil: nil
        default: String(localized: "Skipped")
        }
    }

    /// The request that leaves it out: of this week's meal only, of this recipe every time, or
    /// of every recipe. `nil` when it can't be left out from here, or just this once without a
    /// week.
    func leaveOutRequest(_ choice: IngredientLeaveOut, recipeID: String, week: ISOWeek? = nil)
        -> GrocerySkipRequest?
    {
        guard let ingredientKey else { return nil }
        switch choice {
        case .thisOnce:
            guard let week else { return nil }
            return .leaveOut(ingredientKey: ingredientKey, name: line.name, recipeID: recipeID, week: week)
        case .thisDish: return .leaveOut(ingredientKey: ingredientKey, name: line.name, recipeID: recipeID)
        case .everyDish: return .always(ingredientKey: ingredientKey, name: line.name)
        }
    }
}

/// How far to leave an ingredient out of a recipe.
nonisolated enum IngredientLeaveOut: Equatable, Sendable {
    /// Out of this week's meal only; back next time.
    case thisOnce
    /// Out of this dish every time it's planned.
    case thisDish
    /// Out of every dish, and off every list.
    case everyDish
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
            // The server's amount reads a packet as a kitchen measure ("2 Tbsp" of tomato paste),
            // and a swapped protein reads as the one being cooked.
            var shown = line
            let swapName = state.swapName.flatMap { $0.isEmpty ? nil : $0 }
            if state.amountText?.isEmpty == false || swapName != nil {
                shown = IngredientLine(
                    id: line.id, name: swapName ?? line.name, amount: state.amountText ?? line.amount,
                    isPantryStaple: line.isPantryStaple, category: line.category, imageURL: line.imageURL,
                    allergens: line.allergens)
            }
            return RecipeIngredientState(
                line: shown, ingredientKey: state.ingredientKey, leftOut: state.leftOut, component: state.component)
        }
    }
}
