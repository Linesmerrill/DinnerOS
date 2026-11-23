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

    /// The fade under a pinned bar (the recipe page's picker, the cooking screen's panes) starts
    /// at the content's inset and grows to a whole row as rows scroll under it, so the top row
    /// never sits half under a hard edge (Hark's review, build 122 items 1c and 1e).
    @Test func theFadeUnderAPinnedBarGrowsToARow() {
        #expect(DSEdgeFade.height(scrolledUnder: -20) == DS.Size.edgeFade)
        #expect(DSEdgeFade.height(scrolledUnder: 0) == DS.Size.edgeFade)
        #expect(DSEdgeFade.height(scrolledUnder: 10) == DS.Size.edgeFade + 10)
        #expect(DSEdgeFade.height(scrolledUnder: 500) == DS.Size.rowEdgeFade)
        #expect(DS.Size.rowEdgeFade > DS.Size.edgeFade)
    }

    /// The picker's fade: its usual height while the section sits where it lays out under the
    /// picker (a stack spacing below it), a row once the section has scrolled up under it.
    @Test func thePickerFadeGrowsOnceRowsScrollUnderIt() {
        let resting = 100 + 40 + DS.Space.xl
        #expect(
            RecipeDetailHeader.pickerFade(sectionTop: resting + 300, pinLine: 100, pickerHeight: 40) == DS.Size.edgeFade
        )
        #expect(RecipeDetailHeader.pickerFade(sectionTop: resting, pinLine: 100, pickerHeight: 40) == DS.Size.edgeFade)
        #expect(
            RecipeDetailHeader.pickerFade(sectionTop: resting - 10, pinLine: 100, pickerHeight: 40)
                == DS.Size.edgeFade + 10)
        #expect(
            RecipeDetailHeader.pickerFade(sectionTop: resting - 400, pinLine: 100, pickerHeight: 40)
                == DS.Size.rowEdgeFade)
    }

    // MARK: Rows fade out whole at a pinned bar (build 122 round 5)

    /// A row resting where a list's first row starts (a fade's height below the bar) is whole;
    /// it fades as it comes up and is gone by the time its top reaches the bar's edge, so it's
    /// never shown half under the bar (Grape Tomatoes on the Duo's recipe page, ½ cup Jasmine Rice
    /// on its cooking screen).
    @Test func aRowFadesOutWholeBeforeItReachesTheBar() {
        let edge: CGFloat = 120
        #expect(DSRowEdgeFade.opacity(top: edge + DS.Size.edgeFade, height: 50, barEdge: edge) == 1)
        #expect(DSRowEdgeFade.opacity(top: edge + 300, height: 50, barEdge: edge) == 1)
        #expect(DSRowEdgeFade.opacity(top: edge + DS.Size.edgeFade / 2, height: 50, barEdge: edge) == 0.5)
        #expect(DSRowEdgeFade.opacity(top: edge, height: 50, barEdge: edge) == 0)
        #expect(DSRowEdgeFade.opacity(top: edge - 25, height: 50, barEdge: edge) == 0)
    }

    @Test func noBarOrATallCardLeavesTheRowAlone() {
        #expect(DSRowEdgeFade.opacity(top: 0, height: 50, barEdge: nil) == 1)
        // A step card with its photo keeps the bar's gradient instead.
        #expect(DSRowEdgeFade.opacity(top: 120, height: DSRowEdgeFade.maxRowHeight + 1, barEdge: 120) == 1)
    }

    // MARK: Nothing fades at rest (build 122 round 5, items 1 and 3)

    /// The iPad cooking screen's panes sit under the navigation bar with an 86 pt top inset. At
    /// rest nothing fades: the inset used as the edge faded Step 1, the Steps heading and the
    /// photo out when the screen opened.
    @Test func aPaneAtRestHasNoFade() {
        #expect(DSRowEdgeFade.barEdge(contentOffset: -86, topInset: 86) == nil)
        #expect(DSRowEdgeFade.barEdge(contentOffset: 0, topInset: 0) == nil)
        // Rubber-banding past the top isn't scrolling under the bar either.
        #expect(DSRowEdgeFade.barEdge(contentOffset: -120, topInset: 86) == nil)
    }

    /// Once scrolled, the edge is the top of the scroll view's own space, which starts below
    /// its inset: the Steps heading (32 pt down) is whole the moment the pane starts to move.
    @Test func aScrolledPaneFadesAtTheTopOfItsSpace() throws {
        let edge = try #require(DSRowEdgeFade.barEdge(contentOffset: -80, topInset: 86))
        #expect(edge == 0)
        #expect(DSRowEdgeFade.opacity(top: DS.Size.edgeFade, height: 24, barEdge: edge) == 1)
        #expect(DSRowEdgeFade.opacity(top: 0, height: 24, barEdge: edge) == 0)
        #expect(DSRowEdgeFade.barEdge(contentOffset: 40, topInset: 0) == 0)
    }

    // MARK: A list never rests on a half-faded row (build 122 round 5, items 4 and 10b)

    /// The Allergens line stopped 8 pt under the picker, inside the fade (1.5:1 in dark): the
    /// page moves back 24 pt so it rests whole, where a list's first row rests.
    @Test func aRowRestingInTheFadeComesBackWhole() throws {
        let edge: CGFloat = 200
        let rows: [(top: CGFloat, height: CGFloat)] = [(edge + 8, 24), (edge + 40, 44), (edge + 84, 44)]
        let shift = try #require(DSRowEdgeFade.restingShift(rows: rows, barEdge: edge))
        #expect(shift == 8 - DS.Size.edgeFade)
    }

    /// A row mostly gone under the picker: the next one comes up to rest instead, the shorter
    /// move, so the list doesn't jump back a whole row.
    @Test func aRowMostlyGoneLetsTheNextOneRest() throws {
        let edge: CGFloat = 200
        let rows: [(top: CGFloat, height: CGFloat)] = [(edge - 30, 44), (edge + 14, 44), (edge + 58, 44)]
        let shift = try #require(DSRowEdgeFade.restingShift(rows: rows, barEdge: edge))
        // The second row (14 pt down) moves to 32 pt down.
        #expect(shift == 14 - DS.Size.edgeFade)
        // At the end of the page there's no going on: the row comes back instead.
        let rows2: [(top: CGFloat, height: CGFloat)] = [(edge + 2, 44), (edge + 46, 44)]
        #expect(DSRowEdgeFade.restingShift(rows: rows2, barEdge: edge, maxForward: 0) == 2 - DS.Size.edgeFade)
    }

    /// Rows that rest whole, a list at its top, and tall step cards are left alone.
    @Test func wholeRowsDontMove() {
        let edge: CGFloat = 200
        #expect(DSRowEdgeFade.restingShift(rows: [(edge + DS.Size.edgeFade, 44)], barEdge: edge) == nil)
        #expect(DSRowEdgeFade.restingShift(rows: [(edge + 4, 44)], barEdge: nil) == nil)
        #expect(
            DSRowEdgeFade.restingShift(rows: [(edge + 4, DSRowEdgeFade.maxRowHeight + 1)], barEdge: edge) == nil)
        // A row entirely under the bar is already gone; the one below it rests whole.
        #expect(DSRowEdgeFade.restingShift(rows: [(edge - 60, 40), (edge + 40, 44)], barEdge: edge) == nil)
    }

    // MARK: A short tab still lets the picker pin (build 122 round 5)

    /// The Duo's typed-by-hand recipe on Nutrition: the page ends inside the snap zone, so it
    /// came to rest with the large title half under the bar. The sections get room to pin.
    @Test func aShortTabGetsRoomForThePickerToPin() {
        let zone: ClosedRange<CGFloat> = 200...500
        // Sections from 600 down, 150 tall, a 40 pt pad: the page reaches 600+150+40-560 = 230.
        let height = RecipeDetailHeader.sectionsMinHeight(
            sectionsTop: 600, sectionsHeight: 150, bottomPadding: 40, visibleHeight: 560, pinLine: 100, zone: zone)
        #expect(height == CGFloat(560 - 100 - 40))
        // With that room the page reaches the pinned end: 600 + 420 + 40 - 560 = 500.
        #expect(600 + height + 40 - 560 >= zone.upperBound)
    }

    /// The snap counts the bottom bar's inset, so a page with room to pin can reach the pin.
    @Test func theScrollEndsPastTheBottomBar() {
        #expect(RecipeDetailHeader.reachable(contentHeight: 1200, containerHeight: 700, bottomInset: 150) == 650)
    }

    /// A drag let go inside the zone, with no flick, settles at the nearer end.
    @Test func aPageThatStopsInsideTheZoneSettlesAtAnEnd() {
        let zone: ClosedRange<CGFloat> = 164...351
        #expect(RecipeDetailHeader.settle(offset: 221, zone: zone, reachable: 397) == 164)
        #expect(RecipeDetailHeader.settle(offset: 300, zone: zone, reachable: 397) == 351)
        // Outside the zone, at an end, or no zone: left alone.
        #expect(RecipeDetailHeader.settle(offset: 100, zone: zone, reachable: 397) == nil)
        #expect(RecipeDetailHeader.settle(offset: 351, zone: zone, reachable: 397) == nil)
        #expect(RecipeDetailHeader.settle(offset: 221, zone: nil, reachable: 397) == nil)
    }

    @Test func aLongOrUnscrollablePageKeepsItsHeight() {
        let zone: ClosedRange<CGFloat> = 200...500
        // Long enough already.
        #expect(
            RecipeDetailHeader.sectionsMinHeight(
                sectionsTop: 600, sectionsHeight: 900, bottomPadding: 40, visibleHeight: 560, pinLine: 100, zone: zone)
                == 0)
        // Fits on the screen, or never scrolls the title to the bar (an iPad): nothing added.
        #expect(
            RecipeDetailHeader.sectionsMinHeight(
                sectionsTop: 600, sectionsHeight: 60, bottomPadding: 40, visibleHeight: 900, pinLine: 100, zone: zone)
                == 0)
        // No zone, or not measured yet.
        #expect(
            RecipeDetailHeader.sectionsMinHeight(
                sectionsTop: 600, sectionsHeight: 150, bottomPadding: 40, visibleHeight: 560, pinLine: 100, zone: nil)
                == 0)
        #expect(
            RecipeDetailHeader.sectionsMinHeight(
                sectionsTop: 600, sectionsHeight: 0, bottomPadding: 40, visibleHeight: 560, pinLine: 100, zone: zone)
                == 0)
    }

    // MARK: Each word once under the title (build 122 round 5)

    @Test func aTagTheBadgeAlreadySaysIsLeftOut() {
        #expect(RecipeTagChips.shown(["quick", "Pasta"], badge: "Quick") == ["Pasta"])
        #expect(RecipeTagChips.shown(["quick", "Pasta"]) == ["Quick", "Pasta"])
        #expect(RecipeTagChips.shown(["one-pot", "seo", "latin-american-faves", "One Pot"]) == ["One Pot"])
    }

    @Test func thePhotoNeverSaysQuick() {
        let quick = MenuBadge(code: .quick, text: "Quick")
        let again = MenuBadge(code: .makeAgain, text: "Make Again")
        #expect(RecipeTagChips.heroBadge([quick]) == nil)
        #expect(RecipeTagChips.heroBadge([quick, again]) == again)
    }

    @Test func theStepperInTheBarMeansNoSecondServingsControl() {
        let planned = RecipePlanBarLayout.make(
            isPlanned: true, canEdit: true, isDraft: true, isPastWeek: false, hasStartCooking: true,
            startCookingInBar: true, stacksStartCooking: true)
        #expect(planned?.row.contains(.stepper) == true)
        let notPlanned = RecipePlanBarLayout.make(
            isPlanned: false, canEdit: true, isDraft: true, isPastWeek: false, hasStartCooking: true,
            startCookingInBar: true, stacksStartCooking: true)
        #expect(notPlanned?.row.contains(.stepper) == false)
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
