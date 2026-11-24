import CoreGraphics
import Testing

@testable import DinnerOS

/// The Menu's big cards: Your Meals and Suggested for You are one width, and a wide row shows a
/// peek of the card past the edge.
@MainActor
struct SuggestedCardWidthTests {
    /// Build 121 review round 2, Hark's item 12: one suggestion on an iPad in portrait was a
    /// 575-point banner under 360-point meal cards. Every suggestion is a meal card's width.
    @Test(arguments: [834.0, 1210.0, 1032.0, 1366.0, 744.0])
    func aSuggestionIsAMealCardsWidth(width: CGFloat) {
        #expect(SuggestedMealsRow.cardWidth(in: width) == MealCarouselLayout.cardWidth(in: width))
        #expect(SuggestedMealsRow.cardWidth(in: width) <= 420)
    }

    /// Build 121 review round 2, Hark's item 10: on an iPad in landscape three meal cards filled
    /// the row exactly, so a fourth meal had no sign of being there. A wide row ends in a peek.
    @Test(arguments: [834.0, 1210.0, 1032.0, 1366.0, 744.0])
    func aWideRowShowsAPeekOfTheNextCard(width: CGFloat) {
        let card = MealCarouselLayout.cardWidth(in: width)
        let whole = CGFloat(MealCarouselLayout.wholeCards(in: width))
        let shown = width - DS.Space.l - whole * (card + DS.Space.m)
        #expect(shown >= card * 0.2)
        #expect(shown < card)
        #expect(card >= MealCarouselLayout.minWideCard)
    }

    @Test func anIPadShowsTwoOrThreeWholeCards() {
        #expect(MealCarouselLayout.wholeCards(in: 834) == 2)
        #expect(MealCarouselLayout.wholeCards(in: 1210) == 3)
    }

    /// A wide card's photo widens instead of growing taller than a planned meal's photo.
    @Test func aWideCardsPhotoKeepsItsHeight() {
        #expect(abs(SuggestedMealsRow.photoAspectRatio(cardWidth: 280) - 4.0 / 3.0) < 0.0001)
        #expect(abs(SuggestedMealsRow.photoAspectRatio(cardWidth: 0) - 4.0 / 3.0) < 0.0001)
        let wide: CGFloat = 579
        #expect(wide / SuggestedMealsRow.photoAspectRatio(cardWidth: wide) <= SuggestedMealsRow.maxPhotoHeight + 0.5)
    }

    @Test func aPhoneShowsOneCardAndAPeek() {
        #expect(SuggestedMealsRow.cardWidth(in: 402) == 402 * 0.62)
        #expect(MealCarouselLayout.cardWidth(in: 402) == 402 * 0.84)
    }
}
