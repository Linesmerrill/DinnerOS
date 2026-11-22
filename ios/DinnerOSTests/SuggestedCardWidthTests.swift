import CoreGraphics
import Testing

@testable import DinnerOS

/// The Suggested for You cards fit a short week inside the margins on an iPad (build 122 review
/// 4: on an iPad in portrait the third card ran off the right edge).
@MainActor
struct SuggestedCardWidthTests {
    /// The row's width taken by `count` cards, their gaps and the margins on both sides.
    private func rowWidth(_ width: CGFloat, count: Int) -> CGFloat {
        let card = SuggestedMealsRow.cardWidth(in: width, count: count)
        return CGFloat(count) * card + CGFloat(count - 1) * DS.Space.m + 2 * DS.Space.l
    }

    @Test(arguments: [834.0, 1210.0, 1032.0, 1366.0])
    func threeCardsFitAnIPad(width: CGFloat) {
        #expect(rowWidth(width, count: 3) <= width + 0.5)
        #expect(SuggestedMealsRow.cardWidth(in: width, count: 3) >= DS.Size.carouselCardMinWidth)
    }

    @Test func aLongWeekKeepsReadableCardsAndScrolls() {
        let card = SuggestedMealsRow.cardWidth(in: 834, count: 7)
        #expect(card >= 280)
        #expect(rowWidth(834, count: 7) > 834)
    }

    @Test func cardsNeverGrowToFill() {
        #expect(SuggestedMealsRow.cardWidth(in: 1210, count: 1) == SuggestedMealsRow.cardWidth(in: 1210, count: 7))
    }

    @Test func aPhoneShowsOneCardAndAPeek() {
        #expect(SuggestedMealsRow.cardWidth(in: 402, count: 3) == 402 * 0.62)
    }
}
