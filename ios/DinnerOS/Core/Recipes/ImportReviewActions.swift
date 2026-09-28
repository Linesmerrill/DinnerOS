import Foundation

/// What a member can do about one row of Import Review, and what the screen says about it.
///
/// Pure so the choices and the wording are tested rather than checked by eye. The copy rules
/// are the import screens' (`MealKitFormatting`): short sentences, one idea per line, no dash
/// asides.
nonisolated enum ImportReviewAction: Hashable, Sendable {
    /// The delivered variant is the stored recipe (`same_recipe`).
    case sameRecipe
    /// The delivered variant is its own dish: add it with Add Recipe, then link it
    /// (`different_recipe`). The import only has its name, so it can't be created for them.
    case addAsNewRecipe(deliveredName: String)
    /// Open the stored recipe, when there is one.
    case openRecipe
    /// Nothing to do (`dismissed`).
    case dismiss

    /// The resolution the action records, or `nil` for one that only navigates.
    var resolution: ImportReviewResolution? {
        switch self {
        case .sameRecipe: .sameRecipe
        case .addAsNewRecipe: .differentRecipe
        case .dismiss: .dismissed
        case .openRecipe: nil
        }
    }

    /// The choices for a recipe whose box looked different. A respelling is plainly the same
    /// dish, so it isn't offered as a new recipe.
    static func actions(for group: ImportVariantGroup) -> [ImportReviewAction] {
        var out: [ImportReviewAction] = [.sameRecipe]
        if !group.isSpellingOnly {
            for name in group.deliveredNames
            where !ImportVariantMatch.isSpellingOnly(stored: group.storedName, delivered: name) {
                out.append(.addAsNewRecipe(deliveredName: name))
            }
        }
        if group.recipeID != nil { out.append(.openRecipe) }
        out.append(.dismiss)
        return out
    }

    /// The choices for a missing-steps, cook-time, unit, or unknown item: look at the recipe,
    /// or take it off the list. Nothing in the app can supply what the source left out.
    static func actions(for item: ImportReview) -> [ImportReviewAction] {
        item.recipeID == nil ? [.dismiss] : [.openRecipe, .dismiss]
    }
}

/// The plain-words explanation for a row's detail screen.
nonisolated enum ImportReviewCopy {
    /// "HelloFresh" for the one source there is, and a neutral word for anything else.
    static func sourceName(_ source: String) -> String {
        source == MealKitService.helloFresh.rawValue
            ? MealKitService.helloFresh.displayName : String(localized: "the meal kit")
    }

    /// What happened to a recipe whose box looked different, one idea per line.
    static func explanation(for group: ImportVariantGroup) -> [String] {
        if group.isSpellingOnly {
            return [
                String(localized: "The box spelled this recipe a little differently."),
                String(localized: "It's almost certainly the same recipe."),
            ]
        }
        var lines = [
            group.recipeID == nil
                ? String(localized: "Your library had this as \(group.storedName).")
                : String(localized: "Your library has this as \(group.storedName)."),
            group.deliveredNames.count == 1
                ? String(localized: "At least one box said \(group.deliveredNames[0]).")
                : String(localized: "Some boxes said something else, listed below."),
            String(localized: "That's often a different protein."),
            String(localized: "Groceries and allergens come from the recipe in your library."),
        ]
        if group.recipeID == nil {
            lines.append(String(localized: "That recipe isn't in your library any more."))
        }
        return lines
    }

    /// What a missing-steps, cook-time, unit, or unknown item means.
    static func explanation(for item: ImportReview) -> [String] {
        let source = sourceName(item.source)
        var lines: [String]
        switch item.kind {
        case .steps:
            lines = [
                String(localized: "\(source) had no instructions for this recipe."),
                String(localized: "We didn't make any up."),
            ]
        case .cookTime:
            lines = [
                String(localized: "\(source) didn't say how long this recipe takes."),
                String(localized: "It has no cook time in your library."),
            ]
        case .ingredientUnit(let ingredient):
            let unit = item.value.isEmpty ? String(localized: "a unit") : "\u{201C}\(item.value)\u{201D}"
            lines = [
                String(localized: "\(ingredient) was measured in \(unit)."),
                String(localized: "That isn't a unit we know, so it has no unit here."),
                String(localized: "The line still reads as \(source) wrote it."),
            ]
        case .variant, .other:
            lines = [item.reason]
        }
        if item.recipeID == nil {
            lines.append(String(localized: "That recipe isn't in your library any more."))
        }
        return lines
    }

    /// The short line under a row in the list.
    static func summary(for item: ImportReview) -> String {
        switch item.kind {
        case .steps: String(localized: "No instructions")
        case .cookTime: String(localized: "No cook time")
        case .ingredientUnit(let ingredient): String(localized: "Unknown unit on \(ingredient)")
        case .variant, .other: item.reason
        }
    }
}
