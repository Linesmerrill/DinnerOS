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
}
