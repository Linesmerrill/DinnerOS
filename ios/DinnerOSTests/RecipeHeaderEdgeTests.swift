import CoreGraphics
import Foundation
import SwiftUI
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

    /// Under the Duo cover screen's two-row bottom bar the photo takes less of the page, so the
    /// open tab's first lines show above the bar's fade (Hark's review, build 123 item 4).
    @Test func thePhotoIsShorterAboveATwoRowBar() {
        let usual = RecipeDetailHeader.heroHeight(width: 375, isWide: false, pageHeight: 520)
        let stacked = RecipeDetailHeader.heroHeight(width: 375, isWide: false, pageHeight: 520, stackedBar: true)
        #expect(usual == 240)
        #expect(stacked == 520 * RecipeDetailHeader.stackedBarPageShare)
        #expect(usual - stacked >= 50)
        #expect(
            RecipeDetailHeader.heroHeight(width: 375, isWide: false, pageHeight: 200, stackedBar: true)
                == RecipeDetailHeader.stackedBarMinHero)
    }

    // MARK: The fade under the bar

    @Test func theBarFadeShrinksAsThePickerComesUpToPin() {
        #expect(RecipeDetailHeader.barFade(pickerTop: 500, pinLine: 100) == DS.Size.edgeFade)
        #expect(RecipeDetailHeader.barFade(pickerTop: 110, pinLine: 100) == 10)
        #expect(RecipeDetailHeader.barFade(pickerTop: 100, pinLine: 100) == 0)
        #expect(RecipeDetailHeader.barFade(pickerTop: -40, pinLine: 100) == 0)
    }

    // MARK: Build 121 round 2 (Hark's items 3 and 2)

    /// Resting just under the bar, the large title keeps only the photo's gap above it, so the
    /// photo ends at the bar's edge and no strip of it shows through the fade (the Duo).
    @Test func theZoneStartsWithThePhotoWhollyUnderTheBar() throws {
        let zone = try #require(
            RecipeDetailHeader.snapZone(
                titleTop: 400, pickerTop: 700, pinLine: 100, fade: RecipeDetailHeader.titleGap))
        // The title rests the gap below the bar's edge: the photo ends right at it.
        #expect(400 - zone.lowerBound == 100 + RecipeDetailHeader.titleGap)
    }

    @Test func theBarFadeNeverReachesTheLargeTitle() {
        // The title resting the gap below the bar: the fade covers the gap only.
        #expect(
            RecipeDetailHeader.barFade(pickerTop: 500, pinLine: 100, titleTop: 100 + RecipeDetailHeader.titleGap)
                == RecipeDetailHeader.titleGap)
        // Far below, or long gone under the bar: the usual fade.
        #expect(RecipeDetailHeader.barFade(pickerTop: 500, pinLine: 100, titleTop: 600) == DS.Size.edgeFade)
        #expect(RecipeDetailHeader.barFade(pickerTop: 500, pinLine: 100, titleTop: -300) == DS.Size.edgeFade)
        // Going under: the fade grows back with it.
        #expect(RecipeDetailHeader.barFade(pickerTop: 500, pinLine: 100, titleTop: 80) == 20)
    }

    /// The page let go past the pin moves back to show its first row whole, but never so far
    /// that the picker comes loose and the title's last line rests under the bar (large iPhone).
    @Test func aRestingMoveNeverUnpinsThePicker() {
        let zone: ClosedRange<CGFloat> = 135...459
        #expect(RecipeDetailHeader.keepingPinned(-34, offset: 459, zone: zone) == nil)
        #expect(RecipeDetailHeader.keepingPinned(-40, offset: 480, zone: zone) == -21)
        #expect(RecipeDetailHeader.keepingPinned(60, offset: 459, zone: zone) == 60)
        // Above the zone, or with none, the move is left alone.
        #expect(RecipeDetailHeader.keepingPinned(-20, offset: 50, zone: zone) == -20)
        #expect(RecipeDetailHeader.keepingPinned(-20, offset: 500, zone: nil) == -20)
    }

    /// A tab picked with the picker pinned and the last tab scrolled on starts at its top, right
    /// under the picker; one picked with the picker still loose leaves the page where it is.
    @Test func aNewTabStartsUnderThePinnedPicker() {
        let zone: ClosedRange<CGFloat> = 91...439
        #expect(RecipeDetailHeader.tabStart(offset: 499, zone: zone) == 439)
        #expect(RecipeDetailHeader.tabStart(offset: 439, zone: zone) == nil)
        #expect(RecipeDetailHeader.tabStart(offset: 200, zone: zone) == nil)
        #expect(RecipeDetailHeader.tabStart(offset: 900, zone: nil) == nil)
    }

    /// Asking a scroll position for the point it was last sent to again still moves the page:
    /// a page let go short of the pin twice stayed short the second time.
    @Test func theSamePointTwiceIsStillAMove() {
        #expect(ScrollPosition.target(459, last: nil) == 459)
        #expect(ScrollPosition.target(459, last: 300) == 459)
        #expect(ScrollPosition.target(459, last: 459) == 459.01)
        #expect(ScrollPosition.target(459, last: 459.01) == 459)
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

    /// The picker's fade: the gap to the section while the section sits where it lays out under
    /// the picker, growing to a row once the section has scrolled up under it.
    @Test func thePickerFadeGrowsOnceRowsScrollUnderIt() {
        let gap = RecipeDetailHeader.sectionGap
        let resting = 100 + 40 + gap
        #expect(RecipeDetailHeader.pickerFade(sectionTop: resting + 300, pinLine: 100, pickerHeight: 40) == gap)
        #expect(RecipeDetailHeader.pickerFade(sectionTop: resting, pinLine: 100, pickerHeight: 40) == gap)
        #expect(RecipeDetailHeader.pickerFade(sectionTop: resting - 10, pinLine: 100, pickerHeight: 40) == gap + 10)
        #expect(
            RecipeDetailHeader.pickerFade(sectionTop: resting - 400, pinLine: 100, pickerHeight: 40)
                == DS.Size.rowEdgeFade)
    }

    // MARK: The section starts right under the picker (build 123 item 4)

    /// A fade's height of room under the picker left the Duo's cover screen blank above the
    /// bottom bar: the section now starts a small gap under it, inside the picker's own padding.
    @Test func theSectionStartsRightUnderThePicker() {
        #expect(RecipeDetailHeader.sectionGap < DS.Size.edgeFade)
        #expect(RecipeDetailHeader.sectionGap <= DS.Space.s)
    }

    /// The picker's fade at rest never reaches the first row, and that row is whole: it rests at
    /// full opacity a gap under the pinned picker and is gone before it slides far under it.
    @Test func theFirstRowIsWholeUnderThePinnedPicker() {
        let pinLine: CGFloat = 100
        let pickerHeight: CGFloat = 52
        let resting = pinLine + pickerHeight + RecipeDetailHeader.sectionGap
        let edge = RecipeDetailHeader.rowFadeEdge(pinLine: pinLine, pickerHeight: pickerHeight)
        let fade = RecipeDetailHeader.pickerFade(sectionTop: resting, pinLine: pinLine, pickerHeight: pickerHeight)
        #expect(fade <= resting - pinLine - pickerHeight)
        #expect(DSRowEdgeFade.opacity(top: resting, height: 40, barEdge: edge) == 1)
        #expect(DSRowEdgeFade.opacity(top: resting - 1, height: 40, barEdge: edge) < 1)
        // A page at rest with its first row there isn't moved to show it whole; one a few points
        // under moves back to there.
        #expect(DSRowEdgeFade.restingShift(rows: [(resting, 40), (resting + 52, 40)], barEdge: edge) == nil)
        #expect(DSRowEdgeFade.restingShift(rows: [(resting - 4, 40), (resting + 48, 40)], barEdge: edge) == -4)
        // Already fading when it reaches the picker's edge (where the picker's own fade has grown
        // over it), and gone a fade's height less the gap under it.
        #expect(DSRowEdgeFade.opacity(top: pinLine + pickerHeight, height: 40, barEdge: edge) < 1)
        #expect(
            DSRowEdgeFade.opacity(
                top: pinLine + pickerHeight - (DS.Size.edgeFade - RecipeDetailHeader.sectionGap), height: 40,
                barEdge: edge) == 0)
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

    /// "Step 1" mostly under the bar on an SE's cooking screen: moving on to ¼ tsp Turmeric left
    /// the heading faded out above it, a blank band with no step named (build 121 round 2, item
    /// 4). A heading comes back whole instead, even when it's the longer move.
    @Test func aHeadingMostlyGoneComesBackWhole() throws {
        let edge: CGFloat = 150
        let heading: CGFloat = edge - 30
        let rows: [(top: CGFloat, height: CGFloat)] = [(heading, 56), (heading + 56, 44), (heading + 100, 44)]
        // As a plain row it would let Turmeric come up to rest.
        #expect(DSRowEdgeFade.restingShift(rows: rows, barEdge: edge) == heading + 56 - edge - DS.Size.edgeFade)
        let shift = try #require(DSRowEdgeFade.restingShift(rows: rows, barEdge: edge, headings: [heading]))
        #expect(shift == heading - edge - DS.Size.edgeFade)
        // Where it then rests, the heading is fully drawn.
        #expect(DSRowEdgeFade.opacity(top: heading - shift, height: 56, barEdge: edge) == 1)
        // A heading already whole, or entirely under the bar, isn't moved.
        #expect(
            DSRowEdgeFade.restingShift(
                rows: [(edge + DS.Size.edgeFade, 56)], barEdge: edge, headings: [edge + DS.Size.edgeFade]) == nil)
        #expect(
            DSRowEdgeFade.restingShift(
                rows: [(edge - 60, 56), (edge + DS.Size.edgeFade, 44)], barEdge: edge, headings: [edge - 60]) == nil)
    }

    /// Lemon Wedges cut off at the bottom of an SE (build 121 round 2, item 5): stopped at its end,
    /// the list moved back to show its top row whole and pushed its last row off the screen. The
    /// room under the list covers the longest such move for a row up to a fade's row height.
    @Test func theLastRowStaysWholeWhenTheEndMovesBack() throws {
        let edge: CGFloat = 150
        for height in stride(from: CGFloat(24), through: DS.Size.rowEdgeFade, by: 4) {
            // The top row as far under the bar as it can be and still count as showing.
            let rows: [(top: CGFloat, height: CGFloat)] = [(edge - height + 1, height), (edge + 1, 44)]
            // A heading always comes back whole: the longest move back there is.
            let shift = try #require(
                DSRowEdgeFade.restingShift(rows: rows, barEdge: edge, maxForward: 0, headings: [edge - height + 1]))
            #expect(shift == -(height - 1 + DS.Size.edgeFade))
            // With a gap left under the last row.
            #expect(-shift + DS.Space.l <= DS.Size.listEndRoom, "a \(height) pt row moves back \(-shift) pt")
        }
    }

    /// The end of an SE's typed list (build 121 round 2, item 4): 1 cup Grape Tomatoes rested
    /// a few points into the fade with "Step 4" above it, mostly under the bar. Bringing the row
    /// back whole left the heading's foot in the band under the bar, drawn faded out: the list
    /// comes back far enough to show the heading whole instead.
    @Test func aHeadingOverTheRowComingBackComesBackToo() throws {
        let edge: CGFloat = 155
        let heading: CGFloat = edge - 53
        let rows: [(top: CGFloat, height: CGFloat)] = [(heading, 53), (edge, 44), (edge + 44, 44)]
        // Without knowing the heading, the row comes back alone.
        #expect(DSRowEdgeFade.restingShift(rows: rows, barEdge: edge, maxForward: 0) == -DS.Size.edgeFade)
        let shift = try #require(
            DSRowEdgeFade.restingShift(rows: rows, barEdge: edge, maxForward: 0, headings: [heading]))
        #expect(shift == heading - edge - DS.Size.edgeFade)
        #expect(DSRowEdgeFade.opacity(top: heading - shift, height: 53, barEdge: edge) == 1)
    }

    /// The SE's typed recipe 240 points down (build 121 round 2): "Ingredients" (32 tall) sits
    /// right over "Step 1" (50 tall) over 8 oz Bow Tie Pasta. Bringing the row back brought Step 1
    /// back whole, and that left Ingredients at the bar's edge, drawn faded out. Ingredients comes
    /// back whole too.
    @Test func twoHeadingsTogetherBothComeBackWhole() throws {
        let edge: CGFloat = 0
        let rows: [(top: CGFloat, height: CGFloat)] = [
            (-83, 32), (-51, 50), (-1, 42), (41, 0), (45, 50), (95, 42),
        ]
        let headings: [CGFloat] = [-83, -51, 45]
        let shift = try #require(DSRowEdgeFade.restingShift(rows: rows, barEdge: edge, headings: headings))
        #expect(shift == -83 - DS.Size.edgeFade)
        // Every heading on screen then rests whole.
        for top in headings where top - shift + 32 > edge {
            #expect(DSRowEdgeFade.opacity(top: top - shift, height: 32, barEdge: edge) == 1)
        }
    }

    /// A row moving on past a plain row brings no heading back: the shorter move stays.
    @Test func aPlainRowAboveDoesntPullTheListBack() {
        let edge: CGFloat = 200
        let rows: [(top: CGFloat, height: CGFloat)] = [(edge - 30, 44), (edge + 14, 44), (edge + 58, 44)]
        #expect(DSRowEdgeFade.restingShift(rows: rows, barEdge: edge, headings: [edge + 58]) == 14 - DS.Size.edgeFade)
    }

    /// The row frames say which rows are headings, as the list sits on screen.
    @Test @MainActor func rowFramesKnowTheirHeadings() {
        let frames = DSRowFrames()
        let heading = UUID(), row = UUID()
        frames.set(heading, (100, 56), isHeading: true)
        frames.set(row, (156, 44))
        #expect(frames.headingTops(scrolled: 40) == [60])
        frames.set(heading, nil, isHeading: true)
        #expect(frames.headingTops(scrolled: 40).isEmpty)
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

    /// The page's end keeps room for a row moved back at rest, so the last row stays clear of
    /// the bottom bar's fade (build 121 round 4).
    @Test func thePageEndHasRoomForARowMovedBack() {
        #expect(RecipeDetailView.bottomPadding >= DS.Size.edgeFade + DS.Size.rowEdgeFade)
    }
}
