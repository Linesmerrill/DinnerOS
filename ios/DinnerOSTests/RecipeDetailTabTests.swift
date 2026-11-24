import Testing

@testable import DinnerOS

/// Which tabs the recipe screen offers, and what a selection falls back to when the extras
/// load and the tabs change under it.
struct RecipeDetailTabTests {
    @Test func customizeIsOfferedWhenThereIsSomethingToSwap() {
        #expect(
            RecipeDetailTab.available(hasCustomizations: true, hasPairings: false) == [
                .customization, .description, .ingredients, .nutrition,
            ])
    }

    @Test func customizeIsOfferedWhenThereIsOnlySomethingToPair() {
        #expect(
            RecipeDetailTab.available(hasCustomizations: false, hasPairings: true) == [
                .customization, .description, .ingredients, .nutrition,
            ])
    }

    @Test func aRecipeWithNeitherDropsTheCustomizeTab() {
        #expect(
            RecipeDetailTab.available(hasCustomizations: false, hasPairings: false) == [
                .description, .ingredients, .nutrition,
            ])
    }

    @Test func aSelectionTheRecipeStillHasIsKept() {
        let tabs = RecipeDetailTab.available(hasCustomizations: true, hasPairings: true)

        #expect(RecipeDetailTab.resolve(.customization, in: tabs) == .customization)
        #expect(RecipeDetailTab.resolve(.nutrition, in: tabs) == .nutrition)
    }

    /// The customizations request can come back empty after the screen has already landed on
    /// Customize; the picker must not keep a tab it no longer draws.
    @Test func customizeFallsBackToDescriptionWhenItGoesAway() {
        let tabs = RecipeDetailTab.available(hasCustomizations: false, hasPairings: false)

        #expect(RecipeDetailTab.resolve(.customization, in: tabs) == .description)
    }

    @Test func anEmptyTabListStillResolvesToSomething() {
        #expect(RecipeDetailTab.resolve(.ingredients, in: []) == .description)
    }

    @Test func everyTabHasItsOwnIdentifier() {
        #expect(Set(RecipeDetailTab.allCases.map(\.id)).count == RecipeDetailTab.allCases.count)
    }

    // MARK: One size for every tab's name (Hark's review, build 121 round 2)

    /// A small phone: "Customize" and "Description" need shrinking and "Nutrition" doesn't on
    /// its own. Every name gets the one scale the widest needs, so none is drawn bigger.
    @Test func everyNameShrinksByTheScaleTheWidestNeeds() {
        // Tabs 80 points wide leave 64 for a name.
        let scale = RecipeDetailTabPicker.sharedScale(labelWidths: [76, 80, 72, 62], segmentWidth: 80)

        #expect(abs(scale - 0.8) < 0.0001, "scale \(scale)")
    }

    @Test func namesThatAllFitStayFullSize() {
        #expect(RecipeDetailTabPicker.sharedScale(labelWidths: [60, 70, 64], segmentWidth: 100) == 1)
    }

    @Test func theScaleNeverGoesBelowTheSmallest() {
        let scale = RecipeDetailTabPicker.sharedScale(labelWidths: [300], segmentWidth: 60)

        #expect(scale == RecipeDetailTabPicker.minimumLabelScale)
    }

    @Test func nothingMeasuredYetLeavesTheNamesFullSize() {
        #expect(RecipeDetailTabPicker.sharedScale(labelWidths: [], segmentWidth: 80) == 1)
        #expect(RecipeDetailTabPicker.sharedScale(labelWidths: [90], segmentWidth: 0) == 1)
    }
}
