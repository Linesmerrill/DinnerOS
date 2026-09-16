import Foundation

/// The three questions first run asks, in order. Everything else Autopilot knows is left to
/// Autopilot Preferences, so a new household reaches the app in well under a minute (#330).
nonisolated enum AutopilotOnboardingStep: String, CaseIterable, Hashable, Sendable, Identifiable {
    case taste, avoid, week

    var id: String { rawValue }

    /// The profile section the step edits, which is also what **Skip** restores.
    var section: AutopilotSection {
        switch self {
        case .taste: .taste
        case .avoid: .restrictions
        case .week: .schedule
        }
    }

    var question: String {
        switch self {
        case .taste: String(localized: "What do you like?")
        case .avoid: String(localized: "Anything to avoid?")
        case .week: String(localized: "How do your weeks look?")
        }
    }

    /// At most one line. Anything longer belongs behind the step's info button.
    var subtitle: String {
        switch self {
        case .taste: String(localized: "Tap what you love. Tap again for “no thanks.”")
        case .avoid: String(localized: "Autopilot never suggests a recipe that breaks these.")
        case .week: String(localized: "How many dinners, which nights, and for how many.")
        }
    }

    /// The longer explanation, shown only when the member taps the info button.
    var explanation: String {
        switch self {
        case .taste:
            String(
                localized:
                    "Autopilot leans toward what you like and away from what you don't. You can change any of this later in Autopilot Preferences."
            )
        case .avoid:
            String(
                localized:
                    "Allergens and diets are strict: a recipe that breaks one is never suggested. Ingredients match by name, so “mushroom” also rules out cremini mushrooms."
            )
        case .week:
            String(
                localized:
                    "Autopilot fills up to this many of the nights you choose. Days you've already planned count toward the total."
            )
        }
    }

    /// "2 of 3", for the progress line.
    var positionText: String {
        String(localized: "\(index + 1) of \(AutopilotOnboardingStep.allCases.count)")
    }

    var index: Int {
        AutopilotOnboardingStep.allCases.firstIndex(of: self) ?? 0
    }
}

/// One cuisine in the "What do you like?" grid: a real photo from the catalog with the
/// cuisine's name on it.
nonisolated struct CuisineTile: Hashable, Sendable, Identifiable {
    let value: String
    let label: String
    let recipeCount: Int
    /// A photo from a recipe of this cuisine; `nil` when none of them has one, which shows
    /// the same fallback glyph as an imageless card.
    var imageURL: URL?

    var id: String { value }
}

/// Picks the cuisines the first step offers and the photo each tile shows.
///
/// Both steps are deterministic so the grid doesn't reshuffle between launches: cuisines are
/// ordered by recipe count and then by name, and a tile takes the first photo in the server's
/// own order.
nonisolated enum CuisineTiles {
    /// The grid shows the 8-10 most common cuisines; fewer when the catalog has fewer.
    static let maxCount = 10

    /// The cuisines with the most recipes, most first, with ties broken by name so the order
    /// is stable across launches.
    ///
    /// Cuisines with no recipes can't show a photo, so they're left out — unless none of them
    /// has a count at all (a catalog the server hasn't counted), when the vocabulary's own
    /// order stands in rather than showing an empty step.
    static func top(_ options: [AutopilotOption], limit: Int = maxCount) -> [CuisineTile] {
        let counted = options.filter { ($0.recipeCount ?? 0) > 0 }
        // Without any counts the vocabulary's own order stands in, rather than an empty step.
        let ordered = counted.isEmpty ? options : counted.sorted(by: isBefore)
        return ordered.prefix(max(limit, 0)).map {
            CuisineTile(value: $0.value, label: $0.label, recipeCount: $0.recipeCount ?? 0)
        }
    }

    /// Most recipes first; then by label and value so equal counts keep one order.
    private static func isBefore(_ lhs: AutopilotOption, _ rhs: AutopilotOption) -> Bool {
        let left = lhs.recipeCount ?? 0
        let right = rhs.recipeCount ?? 0
        if left != right { return left > right }
        if lhs.label != rhs.label { return lhs.label < rhs.label }
        return lhs.value < rhs.value
    }

    /// The photo for a tile: the first card that has one, in the order the server returned.
    /// `nil` when no recipe of that cuisine has a photo.
    static func photo(in cards: [MenuCard]) -> URL? {
        cards.lazy.compactMap(\.recipe.imageURL).first
    }

    /// "Italian, liked" — what VoiceOver reads for a tile.
    static func accessibilityValue(_ preference: AutopilotTastePreference) -> String {
        switch preference {
        case .neutral: String(localized: "No preference")
        case .liked: String(localized: "Liked")
        case .disliked: String(localized: "No thanks")
        }
    }
}
