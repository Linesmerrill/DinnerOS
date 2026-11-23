import CoreGraphics
import Foundation
import Testing

@testable import DinnerOS

/// Build 123, from Hark's build 122 round 5 screenshot review.
struct Build123LayoutReviewTests {
    private let locale = Locale(identifier: "en_US")

    // MARK: - Menu filter chips fade at the edge they run past

    @Test func aRowWithNothingPastAnEdgeKeepsTheInsetFade() {
        #expect(CarouselEdgeFade.width(overflow: 0) == CarouselEdgeFade.inset)
        #expect(CarouselEdgeFade.width(overflow: 4) == CarouselEdgeFade.inset)
    }

    @Test func aRowRunningPastAnEdgeFadesWideThere() {
        #expect(CarouselEdgeFade.wide > CarouselEdgeFade.inset)
        #expect(CarouselEdgeFade.width(overflow: 300) == CarouselEdgeFade.wide)
        // On the way to the end it eases back rather than snapping.
        let between = CarouselEdgeFade.width(overflow: 28)
        #expect(between > CarouselEdgeFade.inset && between < CarouselEdgeFade.wide)
    }

    /// The chips on an iPhone SE at rest: "Main Protein" and more run past the trailing edge,
    /// nothing past the leading one.
    @Test func theChipsAtRestOverflowOnlyAtTheTrailingEdge() {
        let overflow = CarouselEdgeFade.overflow(
            visible: CGRect(x: 0, y: 0, width: 375, height: 40), contentWidth: 760)
        #expect(overflow.leading == 0)
        #expect(overflow.trailing == 385)
        #expect(CarouselEdgeFade.width(overflow: overflow.leading) == CarouselEdgeFade.inset)
        #expect(CarouselEdgeFade.width(overflow: overflow.trailing) == CarouselEdgeFade.wide)

        let atEnd = CarouselEdgeFade.overflow(
            visible: CGRect(x: 385, y: 0, width: 375, height: 40), contentWidth: 760)
        #expect(atEnd.trailing == 0)
        #expect(CarouselEdgeFade.width(overflow: atEnd.leading) == CarouselEdgeFade.wide)
    }

    // MARK: - Shared Recipes: See All over the row's last card

    /// Three recipes on an iPad in landscape: the title line ends at the third card, not at the
    /// screen's far side.
    @Test func aShortRowsTitleLineEndsAtItsLastCard() throws {
        let rowWidth: CGFloat = 1210
        let card = DiscoverLayout.fittedCardWidth(rowWidth: rowWidth, isWide: true)
        let width = try #require(
            DiscoverLayout.headerWidth(rowWidth: rowWidth, cardWidth: card, count: 3, isWide: true))
        #expect(width == card * 3 + DiscoverLayout.rowSpacing * 2)
        #expect(width < rowWidth - DiscoverLayout.margin(isWide: true) * 2)
    }

    @Test func aFullRowsTitleLineSpansTheRow() {
        let rowWidth: CGFloat = 1210
        let card = DiscoverLayout.fittedCardWidth(rowWidth: rowWidth, isWide: true)
        #expect(DiscoverLayout.headerWidth(rowWidth: rowWidth, cardWidth: card, count: 12, isWide: true) == nil)
        // Not measured yet, or empty.
        #expect(DiscoverLayout.headerWidth(rowWidth: 0, cardWidth: card, count: 3, isWide: true) == nil)
        #expect(DiscoverLayout.headerWidth(rowWidth: rowWidth, cardWidth: card, count: 0, isWide: true) == nil)
    }

    // MARK: - Pantry: an amount shown once

    /// Garlic, 8 cloves, all there: "~100% left", not "~100% left (8 cloves)" under "8 cloves".
    @Test func theEstimateDoesntRepeatTheRowsAmount() {
        let garlic = PantryFixtures.estimate(percentRemaining: 100, unit: "clove", start: 8, remaining: 8)
        #expect(PantryUsageFormat.remainingShort(garlic, besides: "8 cloves", locale: locale) == "~100% left")
        #expect(PantryUsageFormat.remainingShort(garlic, locale: locale) == "~100% left (8 cloves)")
    }

    @Test func theEstimateStillSaysAnAmountThatDiffers() {
        let used = PantryFixtures.estimate(percentRemaining: 67, unit: "oz", start: 8, remaining: 5.33)
        #expect(PantryUsageFormat.remainingShort(used, besides: "8 oz", locale: locale) == "~67% left (5.5 oz)")
    }
}
