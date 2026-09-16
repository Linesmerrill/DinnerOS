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
        case .week: String(localized: "Tap the nights you cook dinner.")
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
                    "Autopilot plans a dinner for each night you pick. Nights you've already planned count toward the total, and Autopilot Preferences can plan fewer dinners than nights, set servings, and cap weeknight cooking."
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

/// The glyph for an allergen or diet on the "Anything to avoid?" step.
///
/// A photo would be noise here — there's nothing appetizing to show about peanuts — so the
/// step is symbol-led instead, which is also what keeps it from reading as a wall of chips
/// (#337). Values the server hasn't taught the app fall back to a neutral glyph rather than
/// a blank tile, so a newer vocabulary still looks right.
nonisolated enum AutopilotAvoidSymbol {
    static let allergenFallback = "exclamationmark.shield.fill"
    static let dietFallback = "checkmark.seal.fill"

    /// Canonical allergen value → SF Symbol.
    static let allergens: [String: String] = [
        "milk": "drop.fill", "dairy": "drop.fill", "eggs": "oval.fill", "fish": "fish.fill",
        "shellfish": "water.waves", "peanuts": "leaf.fill", "tree nuts": "leaf.fill",
        "wheat": "birthday.cake.fill", "gluten": "birthday.cake.fill", "soy": "leaf.circle.fill",
        "sesame": "circle.grid.3x3.fill",
    ]

    /// Canonical diet value → SF Symbol.
    static let diets: [String: String] = [
        "vegetarian": "carrot.fill", "vegan": "leaf.fill", "pescatarian": "fish.fill",
        "gluten-free": "birthday.cake.fill", "dairy-free": "drop.fill", "nut-free": "leaf.fill",
        "keto": "flame.fill", "paleo": "flame.fill", "low-carb": "flame.fill",
    ]

    static func allergen(_ value: String) -> String {
        allergens[value.lowercased()] ?? allergenFallback
    }

    static func diet(_ value: String) -> String {
        diets[value.lowercased()] ?? dietFallback
    }

    /// Every symbol the step can draw, so a test can check they're all real.
    static var allSymbols: [String] {
        Array(allergens.values) + Array(diets.values) + [allergenFallback, dietFallback]
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

/// The canonical cuisine hierarchy, mirroring the API's hand-kept table
/// (`api/internal/recommendations/canonical.go`).
///
/// The vocabulary sends a flat list of values, labels, and counts with no parent, so the app
/// keeps its own copy to tell a region from a cuisine under it. A value the table doesn't
/// know simply has no parent and counts as specific, which is how an unknown cuisine from a
/// newer server behaves (#334).
nonisolated enum CuisineHierarchy {
    /// Canonical value → its broader region. Values that span regions (`mediterranean`,
    /// `fusion`) and the top-level regions have none.
    static let parents: [String: String] = [
        "east asian": "asian", "southeast asian": "asian", "south asian": "asian",
        "southern european": "european", "western european": "european",
        "eastern european": "european", "northern european": "european",
        "caribbean": "latin american", "central american": "latin american",
        "south american": "latin american",
        "north african": "african", "west african": "african", "east african": "african",
        "chinese": "east asian", "japanese": "east asian", "korean": "east asian",
        "thai": "southeast asian", "vietnamese": "southeast asian", "filipino": "southeast asian",
        "indonesian": "southeast asian", "indian": "south asian",
        "italian": "southern european", "greek": "southern european", "spanish": "southern european",
        "portuguese": "southern european",
        "french": "western european", "german": "western european", "british": "western european",
        "irish": "western european",
        "hungarian": "eastern european", "polish": "eastern european", "russian": "eastern european",
        "southern": "north american", "southwestern": "north american", "tex-mex": "north american",
        "cajun": "north american",
        "hawaiian": "pacific islander", "mexican": "latin american",
        "cuban": "caribbean", "jamaican": "caribbean",
        "brazilian": "south american", "peruvian": "south american",
        "lebanese": "middle eastern", "turkish": "middle eastern", "persian": "middle eastern",
        "moroccan": "north african", "ethiopian": "east african",
    ]

    /// The broader regions of a cuisine, nearest first.
    static func ancestors(of value: String) -> [String] {
        var out: [String] = []
        var current = value
        while let parent = parents[current], !out.contains(parent), out.count < parents.count {
            out.append(parent)
            current = parent
        }
        return out
    }

    /// Whether `value` is a broader region of `other`, such as `asian` of `japanese`.
    static func isRegion(_ value: String, of other: String) -> Bool {
        ancestors(of: other).contains(value)
    }

    /// Whether the two would split one preference: the same cuisine, or a region and
    /// something under it.
    static func overlap(_ value: String, of other: String) -> Bool {
        value == other || isRegion(value, of: other) || isRegion(other, of: value)
    }
}

/// Picks the cuisines the first step offers and the photo each tile shows.
///
/// Both steps are deterministic so the grid doesn't reshuffle between launches: cuisines are
/// ordered by recipe count and then by name, and photos are handed out in tile order.
nonisolated enum CuisineTiles {
    /// The grid shows the 8-10 most common cuisines; fewer when the catalog has fewer.
    static let maxCount = 10

    /// A cuisine under a region needs at least this many recipes to stand in for it. Below
    /// that the region is the more useful tile, because picking it covers everything under it.
    static let minimumSpecificRecipes = 5

    /// The cuisines to offer, most recipes first, preferring the specific ones people
    /// recognize (Italian, Mexican, Thai) over the regions above them.
    ///
    /// A region is offered only when nothing under it has `minimumSpecificRecipes`, and a
    /// region never appears beside a cuisine of its own, which would split one preference
    /// across two tiles (#334). Counts roll up, so without this the regions win every time.
    ///
    /// Cuisines with no recipes can't show a photo, so they're left out — unless none of them
    /// has a count at all (a catalog the server hasn't counted), when the vocabulary's own
    /// order stands in rather than showing an empty step.
    static func top(
        _ options: [AutopilotOption], limit: Int = maxCount, minimumSpecific: Int = minimumSpecificRecipes
    ) -> [CuisineTile] {
        let counted = options.filter { ($0.recipeCount ?? 0) > 0 }
        // Without any counts the vocabulary's own order stands in, rather than an empty step.
        guard !counted.isEmpty else {
            return options.prefix(max(limit, 0)).map(tile)
        }
        let candidates = counted.sorted(by: isBefore)
        var chosen: [AutopilotOption] = []
        for candidate in candidates where chosen.count < max(limit, 0) {
            // Never beside a region of its own, or a cuisine of that region.
            guard !chosen.contains(where: { CuisineHierarchy.overlap($0.value, of: candidate.value) }) else {
                continue
            }
            // A region steps aside for the specific cuisines that will represent it.
            let coveredBySpecific = candidates.contains { other in
                CuisineHierarchy.isRegion(candidate.value, of: other.value)
                    && (other.recipeCount ?? 0) >= minimumSpecific
            }
            guard !coveredBySpecific else { continue }
            chosen.append(candidate)
        }
        return chosen.map(tile)
    }

    private static func tile(_ option: AutopilotOption) -> CuisineTile {
        CuisineTile(value: option.value, label: option.label, recipeCount: option.recipeCount ?? 0)
    }

    /// Most recipes first; then by label and value so equal counts keep one order.
    private static func isBefore(_ lhs: AutopilotOption, _ rhs: AutopilotOption) -> Bool {
        let left = lhs.recipeCount ?? 0
        let right = rhs.recipeCount ?? 0
        if left != right { return left > right }
        if lhs.label != rhs.label { return lhs.label < rhs.label }
        return lhs.value < rhs.value
    }

    /// Gives each tile a photo no other tile is using.
    ///
    /// Cuisines overlap — a recipe can be both Italian and Mediterranean — so the same recipe
    /// can be the best match for more than one tile. Tiles are served in order and each takes
    /// the first candidate nothing earlier has taken, which is deterministic; a tile with no
    /// unused candidate keeps no photo and shows a plain tinted tile instead of repeating one
    /// (#335).
    static func assignPhotos(_ tiles: [CuisineTile], candidates: [String: [MenuCard]]) -> [CuisineTile] {
        var usedRecipeIDs: Set<String> = []
        return tiles.map { tile in
            var tile = tile
            tile.imageURL = nil
            for card in candidates[tile.value] ?? [] where !usedRecipeIDs.contains(card.recipe.id) {
                guard let url = card.recipe.imageURL else { continue }
                usedRecipeIDs.insert(card.recipe.id)
                tile.imageURL = url
                break
            }
            return tile
        }
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
