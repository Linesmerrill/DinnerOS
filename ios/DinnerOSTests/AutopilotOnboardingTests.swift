import Foundation
import Testing
import UIKit

@testable import DinnerOS

/// The three-question setup: which sections it covers, what skipping keeps, which cuisines
/// the grid offers, and how it gives every tile a photo of its own (#330-#336).
struct AutopilotOnboardingTests {
    private let limits = AutopilotLimits.defaults

    private func option(_ value: String, _ label: String, _ count: Int?) -> AutopilotOption {
        AutopilotOption(value: value, label: label, description: nil, recipeCount: count)
    }

    private func card(id: String, image: String? = "https://img.example.test/\(UUID().uuidString).jpg") -> MenuCard {
        MenuCard(recipe: .placeholder(id: id, name: "Example \(id)", imageURLString: image))
    }

    private func photo(_ id: String) -> MenuCard {
        MenuCard(
            recipe: .placeholder(
                id: id, name: "Example \(id)", imageURLString: "https://img.example.test/\(id).jpg"))
    }

    /// The live catalog's shape: counts roll up, so every region outranks the cuisines under it.
    private var catalogCuisines: [AutopilotOption] {
        [
            option("asian", "Asian", 123), option("north american", "North American", 110),
            option("european", "European", 80), option("southern european", "Southern European", 74),
            option("latin american", "Latin American", 68), option("east asian", "East Asian", 60),
            option("italian", "Italian", 32), option("mexican", "Mexican", 26),
            option("southeast asian", "Southeast Asian", 24), option("caribbean", "Caribbean", 9),
            option("japanese", "Japanese", 3), option("southwestern", "Southwestern", 1),
        ]
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

    /// The nights you cook are the dinners you want, so setup asks once instead of twice.
    @Test func theNightsPickedAreTheDinnersAutopilotPlans() {
        var settings = AutopilotSettings.defaults
        #expect(settings.schedule.planDays.count == 5)

        settings.setPlanNight(.sat, included: true)
        #expect(settings.schedule.planDays == [.mon, .tue, .wed, .thu, .fri, .sat])
        #expect(settings.schedule.mealsPerWeek == 6)

        settings.setPlanNight(.mon, included: false)
        #expect(settings.schedule.planDays == [.tue, .wed, .thu, .fri, .sat])
        #expect(settings.schedule.mealsPerWeek == settings.schedule.planDays.count)
        #expect(settings.validationMessage == nil)
    }

    /// A week with no nights can't be planned, so the last one stays whatever is tapped.
    @Test func theLastNightStaysSoTheWeekIsAlwaysPlannable() {
        var settings = AutopilotSettings.defaults
        for day in PlanDay.allCases {
            settings.setPlanNight(day, included: false)
        }

        #expect(settings.schedule.planDays.count == 1)
        #expect(settings.schedule.mealsPerWeek == 1)
        #expect(settings.validationMessage == nil)
    }

    /// Setup no longer asks for servings: `nil` means the household's own default, which the
    /// API resolves as `effective.defaultServings`.
    @Test func setupLeavesServingsToTheHouseholdDefault() {
        #expect(AutopilotSettings.defaults.schedule.defaultServings == nil)
    }

    // MARK: Glyphs

    /// A misspelled SF Symbol draws nothing at all, so every one the avoid step can reach is
    /// checked here rather than discovered on the screen.
    @Test func everyAvoidGlyphIsARealSymbol() {
        for name in AutopilotAvoidSymbol.allSymbols {
            #expect(UIImage(systemName: name) != nil, "\(name) isn't an SF Symbol")
        }
    }

    @Test func glyphsAreCaseInsensitiveAndFallBackForAnUnknownValue() {
        #expect(AutopilotAvoidSymbol.allergen("peanuts") == "leaf.fill")
        #expect(AutopilotAvoidSymbol.allergen("Peanuts") == "leaf.fill")
        #expect(AutopilotAvoidSymbol.allergen("unobtainium") == AutopilotAvoidSymbol.allergenFallback)
        #expect(AutopilotAvoidSymbol.diet("vegetarian") == "carrot.fill")
        #expect(AutopilotAvoidSymbol.diet("carnivore") == AutopilotAvoidSymbol.dietFallback)
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

    // MARK: Hierarchy

    @Test func theHierarchyMirrorsTheAPIsRegions() {
        #expect(CuisineHierarchy.ancestors(of: "italian") == ["southern european", "european"])
        #expect(CuisineHierarchy.ancestors(of: "japanese") == ["east asian", "asian"])
        #expect(CuisineHierarchy.ancestors(of: "mexican") == ["latin american"])
        // Spans regions, so it has no parent — and neither does an unknown cuisine.
        #expect(CuisineHierarchy.ancestors(of: "mediterranean").isEmpty)
        #expect(CuisineHierarchy.ancestors(of: "klingon").isEmpty)

        #expect(CuisineHierarchy.isRegion("asian", of: "japanese"))
        #expect(!CuisineHierarchy.isRegion("japanese", of: "asian"))
        #expect(CuisineHierarchy.overlap("european", of: "italian"))
        #expect(CuisineHierarchy.overlap("italian", of: "european"))
        #expect(CuisineHierarchy.overlap("thai", of: "thai"))
        // Siblings under one region are separate choices, not an overlap.
        #expect(!CuisineHierarchy.overlap("mexican", of: "caribbean"))
        #expect(!CuisineHierarchy.overlap("italian", of: "klingon"))
    }

    // MARK: Which cuisines are offered

    /// A region and a cuisine under it split one preference, so the specific one wins.
    @Test func specificCuisinesAreOfferedInsteadOfTheRegionsAboveThem() {
        let tiles = CuisineTiles.top(catalogCuisines)

        #expect(
            tiles.map(\.value) == [
                "north american", "east asian", "italian", "mexican", "southeast asian", "caribbean",
            ])
        // No tile is a region of another, whatever the counts said.
        for (index, tile) in tiles.enumerated() {
            for other in tiles.dropFirst(index + 1) {
                #expect(!CuisineHierarchy.overlap(tile.value, of: other.value))
            }
        }
        #expect(!tiles.contains { $0.value == "asian" || $0.value == "european" })
        #expect(CuisineTiles.top(catalogCuisines) == tiles)
    }

    /// A region is still the better tile when nothing under it has enough recipes: North
    /// American keeps its 110 rather than handing them to Southwestern's 1.
    @Test func aRegionIsOfferedWhenNothingUnderItHasEnoughRecipes() {
        let tiles = CuisineTiles.top(catalogCuisines)

        #expect(tiles.first?.value == "north american")
        #expect(tiles.contains { $0.value == "east asian" })
        #expect(!tiles.contains { $0.value == "japanese" })

        // Lower the bar and Japanese's 3 recipes are enough to replace East Asian.
        let specific = CuisineTiles.top(catalogCuisines, minimumSpecific: 3)
        #expect(specific.contains { $0.value == "japanese" })
        #expect(!specific.contains { $0.value == "east asian" })
    }

    @Test func theGridIsCappedAndCountsDecideTheOrder() {
        let tiles = CuisineTiles.top(catalogCuisines, limit: 3)

        #expect(tiles.map(\.value) == ["north american", "east asian", "italian"])
        #expect(tiles.map(\.recipeCount) == [110, 60, 32])
        #expect(CuisineTiles.top(catalogCuisines, limit: 0).isEmpty)
    }

    @Test func anUncountedVocabularyKeepsItsOwnOrderInsteadOfAnEmptyStep() {
        let options = [option("thai", "Thai", 0), option("italian", "Italian", nil)]

        #expect(CuisineTiles.top(options).map(\.value) == ["thai", "italian"])
        #expect(CuisineTiles.top(options).allSatisfy { $0.imageURL == nil })
        #expect(CuisineTiles.top([]).isEmpty)
    }

    // MARK: Photos

    /// Cuisines overlap, so the same recipe can be the best match for two tiles. Each tile
    /// takes the first one nothing earlier has taken, rather than repeating a photo.
    @Test func everyTileGetsADistinctPhoto() {
        let tiles = CuisineTiles.top(catalogCuisines, limit: 3)
        // The same top recipe wins for all three, with different runners-up.
        let candidates = [
            "north american": [photo("shared"), photo("burger")],
            "east asian": [photo("shared"), photo("ramen")],
            "italian": [photo("shared"), photo("pasta")],
        ]

        let assigned = CuisineTiles.assignPhotos(tiles, candidates: candidates)

        #expect(assigned.compactMap(\.imageURL).count == 3)
        #expect(Set(assigned.compactMap(\.imageURL)).count == 3)
        #expect(assigned[0].imageURL == URL(string: "https://img.example.test/shared.jpg"))
        #expect(assigned[1].imageURL == URL(string: "https://img.example.test/ramen.jpg"))
        #expect(assigned[2].imageURL == URL(string: "https://img.example.test/pasta.jpg"))
        // Deterministic: the same candidates give the same grid every launch.
        #expect(CuisineTiles.assignPhotos(tiles, candidates: candidates) == assigned)
    }

    @Test func aTileWithNoUnusedPhotoShowsNoneRatherThanARepeat() {
        let tiles = CuisineTiles.top(catalogCuisines, limit: 3)
        let candidates = [
            "north american": [photo("shared")],
            // Only the recipe the first tile took, so this one has nothing left.
            "east asian": [photo("shared")],
            // A cuisine whose recipes have no photos at all.
            "italian": [card(id: "no-photo", image: nil)],
        ]

        let assigned = CuisineTiles.assignPhotos(tiles, candidates: candidates)

        #expect(assigned[0].imageURL == URL(string: "https://img.example.test/shared.jpg"))
        #expect(assigned[1].imageURL == nil)
        #expect(assigned[2].imageURL == nil)
        // A cuisine nothing was fetched for keeps no photo either.
        #expect(CuisineTiles.assignPhotos(tiles, candidates: [:]).allSatisfy { $0.imageURL == nil })
    }

    @Test func tilesReadTheirStateToVoiceOver() {
        #expect(CuisineTiles.accessibilityValue(.neutral) == "No preference")
        #expect(CuisineTiles.accessibilityValue(.liked) == "Liked")
        #expect(CuisineTiles.accessibilityValue(.disliked) == "No thanks")
    }
}
