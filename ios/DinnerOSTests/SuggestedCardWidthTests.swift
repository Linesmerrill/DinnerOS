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

    /// Build 123 review, Hark's item 6: two suggestions on an iPad in portrait took two-thirds
    /// of the row, their reasons cut short and the right third empty. A short week fills the
    /// row margin to margin.
    @Test(arguments: [(834.0, 2), (834.0, 3), (1210.0, 2), (1210.0, 3), (1210.0, 4), (1032.0, 2)])
    func aShortWeekFillsTheRow(width: CGFloat, count: Int) {
        #expect(abs(rowWidth(width, count: count) - width) < 0.5)
    }

    /// One suggestion alone stays card-sized rather than a banner across an iPad.
    @Test func oneCardStaysCardSized() {
        #expect(SuggestedMealsRow.cardWidth(in: 1210, count: 1) == SuggestedMealsRow.maxCardWidth)
        #expect(SuggestedMealsRow.cardWidth(in: 834, count: 1) == SuggestedMealsRow.maxCardWidth)
    }

    /// A wide card's photo widens instead of growing taller than a planned meal's photo.
    @Test func aWideCardsPhotoKeepsItsHeight() {
        #expect(abs(SuggestedMealsRow.photoAspectRatio(cardWidth: 280) - 4.0 / 3.0) < 0.0001)
        #expect(abs(SuggestedMealsRow.photoAspectRatio(cardWidth: 0) - 4.0 / 3.0) < 0.0001)
        let wide: CGFloat = 579
        #expect(wide / SuggestedMealsRow.photoAspectRatio(cardWidth: wide) <= SuggestedMealsRow.maxPhotoHeight + 0.5)
    }

    @Test func aPhoneShowsOneCardAndAPeek() {
        #expect(SuggestedMealsRow.cardWidth(in: 402, count: 3) == 402 * 0.62)
    }
}
