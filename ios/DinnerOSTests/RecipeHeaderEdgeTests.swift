import CoreGraphics
import Foundation
import Testing

@testable import DinnerOS

/// Build 122 (Hark's review items 5, 6 and 12): the recipe page never rests on a sliver of its
/// large title, the fade under the bar never draws over the picker, and a recipe nobody has
/// cooked doesn't ask for a rating.
struct RecipeHeaderEdgeTests {
    // MARK: Snapping between the title and the pinned picker

    @Test func theZoneRunsFromAWholeTitleToAPinnedPicker() throws {
        let zone = try #require(
            RecipeDetailHeader.snapZone(titleTop: 400, pickerTop: 700, pinLine: 100, fade: DS.Size.edgeFade))
        #expect(zone.lowerBound == 400 - 100 - DS.Size.edgeFade)
        #expect(zone.upperBound == 600)
    }

    @Test func noZoneWithoutRoomForOne() {
        // Not measured yet.
        #expect(RecipeDetailHeader.snapZone(titleTop: 0, pickerTop: 0, pinLine: 100, fade: 32) == nil)
        // The picker would pin before the title reaches the bar.
        #expect(RecipeDetailHeader.snapZone(titleTop: 400, pickerTop: 300, pinLine: 100, fade: 32) == nil)
    }

    @Test func aScrollEndingInsideSettlesAtTheEndItWasHeadingFor() {
        let zone: ClosedRange<CGFloat> = 300...600
        #expect(RecipeDetailHeader.snapped(350, zone: zone, velocity: 2) == 600)
        #expect(RecipeDetailHeader.snapped(550, zone: zone, velocity: -2) == 300)
        // Let go without a flick: the nearer end.
        #expect(RecipeDetailHeader.snapped(420, zone: zone, velocity: 0) == 300)
        #expect(RecipeDetailHeader.snapped(500, zone: zone, velocity: 0) == 600)
    }

    @Test func aScrollEndingOutsideIsLeftAlone() {
        let zone: ClosedRange<CGFloat> = 300...600
        #expect(RecipeDetailHeader.snapped(0, zone: zone, velocity: 3) == 0)
        #expect(RecipeDetailHeader.snapped(300, zone: zone, velocity: 3) == 300)
        #expect(RecipeDetailHeader.snapped(900, zone: zone, velocity: -3) == 900)
    }

    // MARK: The photo on a short screen

    /// The Duo's cover screen (644 pt) and an iPhone SE (667 pt): the photo leaves room for the
    /// name above the bottom bar. A tall phone keeps its photo.
    @Test func thePhotoStaysWithinAboutHalfOfAShortScreen() {
        #expect(RecipeDetailHeader.heroHeight(width: 298, isWide: false, pageHeight: 644) == 644 * 0.45)
        #expect(RecipeDetailHeader.heroHeight(width: 375, isWide: false, pageHeight: 667) == 667 * 0.45)
        #expect(RecipeDetailHeader.heroHeight(width: 440, isWide: false, pageHeight: 956) == 420)
        // Never shorter than the floor.
        #expect(RecipeDetailHeader.heroHeight(width: 375, isWide: false, pageHeight: 300) == 240)
    }

    // MARK: The fade under the bar

    @Test func theBarFadeShrinksAsThePickerComesUpToPin() {
        #expect(RecipeDetailHeader.barFade(pickerTop: 500, pinLine: 100) == DS.Size.edgeFade)
        #expect(RecipeDetailHeader.barFade(pickerTop: 110, pinLine: 100) == 10)
        #expect(RecipeDetailHeader.barFade(pickerTop: 100, pinLine: 100) == 0)
        #expect(RecipeDetailHeader.barFade(pickerTop: -40, pinLine: 100) == 0)
    }

    // MARK: Ratings only once it's been cooked

    private let thisWeek = ISOWeek("2026-W47")!

    private func cooked(
        rated: Bool = false, householdCount: Int = 0, weeks: [String] = [], nightPassed: Bool = false
    ) -> Bool {
        let mine =
            rated
            ? RecipeRating(
                recipeID: "r", userID: "u", score: 4, comment: "", tags: [], createdAt: .now, updatedAt: .now) : nil
        return RecipeRatingVisibility.hasBeenCooked(
            myRating: mine,
            householdRating: householdCount > 0 ? HouseholdRating(average: 4, count: householdCount) : .unrated,
            orderWeeks: weeks, currentWeek: thisWeek, nightHasPassed: nightPassed)
    }

    @Test func aRecipeNobodyHasMadeHasNothingToRate() {
        #expect(!cooked())
        // Planned this week or later, its night still ahead: not yet.
        #expect(!cooked(weeks: ["2026-W47", "2026-W48"]))
    }

    @Test func aMadeOrRatedRecipeAsksForARating() {
        #expect(cooked(weeks: ["2026-W40"]))
        #expect(cooked(nightPassed: true))
        #expect(cooked(rated: true))
        #expect(cooked(householdCount: 2))
    }
}
