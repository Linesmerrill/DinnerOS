import Foundation
import Testing

@testable import DinnerOS

/// The three-question setup: which sections it covers, what skipping keeps, and how the
/// cuisine grid picks its tiles and their photos (#330, #331).
struct AutopilotOnboardingTests {
    private let limits = AutopilotLimits.defaults

    private func option(_ value: String, _ label: String, _ count: Int?) -> AutopilotOption {
        AutopilotOption(value: value, label: label, description: nil, recipeCount: count)
    }

    private func card(id: String, image: String?) -> MenuCard {
        MenuCard(recipe: .placeholder(id: id, name: "Example \(id)", imageURLString: image))
    }

    // MARK: Steps

    @Test func setupAsksThreeQuestionsAndLeavesTheRestToPreferences() {
        #expect(AutopilotOnboardingStep.allCases.map(\.section) == [.taste, .restrictions, .schedule])
        #expect(AutopilotOnboardingStep.taste.positionText == "1 of 3")
        #expect(AutopilotOnboardingStep.week.positionText == "3 of 3")

        // Everything setup no longer asks stays available in preferences.
        #expect(
            AutopilotSection.allCases.filter { !$0.isInSetup }
                == [.cookTime, .equipment, .weekdayRules, .novelty, .pairings])
        #expect(AutopilotSection.allCases.allSatisfy { !$0.detail.isEmpty })
    }

    @Test func skippingAStepKeepsTheServerDefaultsForThatSectionOnly() {
        var edited = AutopilotSettings.defaults
        edited.setPreference(.liked, for: "mexican", kind: .cuisine, limits: limits)
        edited.restrictions.allergens = ["peanuts"]
        edited.schedule.mealsPerWeek = 3

        // Skipping "Anything to avoid?" restores that section from the loaded profile.
        let skipped = edited.replacing(AutopilotOnboardingStep.avoid.section, from: .defaults)

        #expect(skipped.restrictions == AutopilotRestrictions())
        #expect(skipped.taste.likes.cuisines == ["mexican"])
        #expect(skipped.schedule.mealsPerWeek == 3)
        #expect(skipped.validationMessage == nil)
    }

    /// Tapping a tile cycles liked → no thanks → cleared, and a cuisine is never both.
    @Test func likingAndNoThanksAreExclusiveAsATileIsTapped() {
        var settings = AutopilotSettings.defaults

        func tap() {
            let next = settings.preference(for: "italian", kind: .cuisine).next
            settings.setPreference(next, for: "italian", kind: .cuisine, limits: limits)
        }

        #expect(settings.preference(for: "italian", kind: .cuisine) == .neutral)

        tap()
        #expect(settings.taste.likes.cuisines == ["italian"])
        #expect(settings.taste.dislikes.cuisines.isEmpty)

        tap()
        #expect(settings.taste.likes.cuisines.isEmpty)
        #expect(settings.taste.dislikes.cuisines == ["italian"])

        tap()
        #expect(settings.taste.likes.cuisines.isEmpty)
        #expect(settings.taste.dislikes.cuisines.isEmpty)
    }

    // MARK: Cuisine tiles

    @Test func tilesShowTheMostCommonCuisinesInAStableOrder() {
        let options = [
            option("thai", "Thai", 12), option("mexican", "Mexican", 30), option("italian", "Italian", 30),
            option("nordic", "Nordic", 0), option("basque", "Basque", nil),
        ]

        let tiles = CuisineTiles.top(options, limit: 3)

        // Most recipes first; equal counts keep one order, so the grid doesn't reshuffle.
        #expect(tiles.map(\.value) == ["italian", "mexican", "thai"])
        #expect(tiles.map(\.recipeCount) == [30, 30, 12])
        #expect(CuisineTiles.top(options, limit: 3) == tiles)
        // A cuisine with no recipes has no photo to show, so it isn't offered.
        #expect(!tiles.contains { $0.value == "nordic" || $0.value == "basque" })
        #expect(CuisineTiles.top(options).count == 3)
    }

    @Test func anUncountedVocabularyKeepsItsOwnOrderInsteadOfAnEmptyStep() {
        let options = [option("thai", "Thai", 0), option("italian", "Italian", nil)]

        #expect(CuisineTiles.top(options).map(\.value) == ["thai", "italian"])
        #expect(CuisineTiles.top(options).allSatisfy { $0.imageURL == nil })
        #expect(CuisineTiles.top([]).isEmpty)
        #expect(CuisineTiles.top(options, limit: 0).isEmpty)
    }

    @Test func aTileTakesTheFirstPhotoAndFallsBackWhenNoRecipeHasOne() {
        let url = "https://img.example.test/f_auto,q_auto,w_1200/recipe-2.jpg"
        let cards = [
            card(id: "recipe-1", image: nil), card(id: "recipe-2", image: url),
            card(id: "recipe-3", image: nil),
        ]

        #expect(CuisineTiles.photo(in: cards) == URL(string: url))
        // Nothing to show: the tile keeps the same fallback glyph as an imageless card.
        #expect(CuisineTiles.photo(in: [card(id: "recipe-1", image: nil)]) == nil)
        #expect(CuisineTiles.photo(in: []) == nil)
    }

    @Test func tilesReadTheirStateToVoiceOver() {
        #expect(CuisineTiles.accessibilityValue(.neutral) == "No preference")
        #expect(CuisineTiles.accessibilityValue(.liked) == "Liked")
        #expect(CuisineTiles.accessibilityValue(.disliked) == "No thanks")
    }
}
