import Foundation
import Testing

@testable import DinnerOS

/// The prep card as a picture: the "this week" amount, one bag card per future dinner, the
/// leftover line, the button's name, and what VoiceOver reads
/// (docs/shopping-providers.md#the-prep-plan). The cards are `PrepPreviewData`'s, so the
/// previews and the tests look at the same Tuesday.
struct PrepPresentationTests {
    private let locale = Locale(identifier: "en_US")

    private func presentation(_ card: PrepCard?, bags: Int? = nil) throws -> PrepCardPresentation {
        PrepCardPresentation(card: try #require(card), chosenBags: bags, locale: locale)
    }

    // MARK: - Bag count

    @Test func theCountStartsAtTheSuggestion() throws {
        let one = try presentation(PrepPreviewData.oneBag)
        #expect(one.bags == 1)
        #expect(one.bagRange == 1...1)
        #expect(!one.canAddBag)
        #expect(!one.canRemoveBag)
        let several = try presentation(PrepPreviewData.severalBags)
        #expect(several.bags == 4)
        #expect(several.maxBags == 4)
    }

    /// Only whole dinners: never more bags than the surplus holds, and never fewer than one
    /// while there is one to freeze — "none" is Skip.
    @Test func theCountStaysWithinTheWholeBagsTheSurplusHolds() throws {
        #expect(try presentation(PrepPreviewData.severalBags, bags: 9).bags == 4)
        #expect(try presentation(PrepPreviewData.severalBags, bags: 0).bags == 1)
        #expect(try presentation(PrepPreviewData.severalBags, bags: -2).bags == 1)
        let middle = try presentation(PrepPreviewData.severalBags, bags: 2)
        #expect(middle.canAddBag)
        #expect(middle.canRemoveBag)
        #expect(!(try presentation(PrepPreviewData.severalBags, bags: 4)).canAddBag)
        #expect(!(try presentation(PrepPreviewData.severalBags, bags: 1)).canRemoveBag)
    }

    /// Less than a dinner left over: no bag row, no controls, just the leftover line.
    @Test func underADinnerHasNoBags() throws {
        let small = try presentation(PrepPreviewData.leftoverOnly, bags: 3)
        #expect(small.bags == 0)
        #expect(small.bagRange == 0...0)
        #expect(!small.hasBags)
        #expect(!small.canAddBag)
        #expect(small.leftoverLine == "4 oz left over. Throw it in for a little more protein, or toss it.")
    }

    // MARK: - Words

    @Test func theButtonSaysWhatItFreezes() throws {
        #expect(try presentation(PrepPreviewData.oneBag).buttonTitle == "Freeze 1 Bag")
        #expect(try presentation(PrepPreviewData.severalBags, bags: 3).buttonTitle == "Freeze 3 Bags")
        #expect(try presentation(PrepPreviewData.leftoverOnly).buttonTitle == "Done")
    }

    /// The leftover belongs to the count beside it, and a count that leaves nothing says so by
    /// showing no line at all.
    @Test func theLeftoverLineFollowsTheCount() throws {
        #expect(
            try presentation(PrepPreviewData.oneBag).leftoverLine
                == "4 oz left over. Throw it in for a little more protein, or toss it.")
        #expect(
            try presentation(PrepPreviewData.severalBags, bags: 2).leftoverLine
                == "24 oz left over. Throw it in for a little more protein, or toss it.")
        #expect(
            try presentation(PrepPreviewData.severalBags, bags: 4).leftoverLine
                == "4 oz left over. Throw it in for a little more protein, or toss it.")
        #expect(try presentation(PrepPreviewData.noPhoto, bags: 3).leftoverLine == nil)
    }

    @Test func theCardReadsLikeAScale() throws {
        let one = try presentation(PrepPreviewData.oneBag)
        #expect(one.packText == "24 oz pack")
        #expect(one.keepText == "10 oz")
        #expect(one.keepCaption == "for Tuesday's Citrus Turkey Tacos")
        #expect(one.bagText == "10 oz")
        #expect(one.thawText == "about 3 hr to thaw")
        let several = try presentation(PrepPreviewData.severalBags)
        #expect(several.keepText == "20 oz")
        #expect(several.keepCaption == "for Tuesday's Citrus Pork Tacos and Thursday's Pork Fried Rice")
    }

    @Test func aCardWithoutAPhotoHasNoURL() throws {
        #expect(try #require(PrepPreviewData.noPhoto).imageURL == nil)
        #expect(try #require(PrepPreviewData.oneBag).imageURL?.absoluteString.hasPrefix("https://") == true)
        #expect(PrepIngredientTile.symbol(for: "meat-seafood") == "fork.knife")
        #expect(PrepIngredientTile.symbol(for: "produce") == "carrot")
    }

    // MARK: - VoiceOver

    @Test func eachCardReadsAsOneSentence() throws {
        let one = try presentation(PrepPreviewData.oneBag)
        #expect(one.keepAccessibilityLabel == "Keep 10 ounces of ground turkey out for Tuesday's Citrus Turkey Tacos")
        #expect(one.bagAccessibilityLabel(1) == "Freezer bag 1 of 1, 10 ounces, about 3 hours to thaw")
        #expect(
            one.leftoverAccessibilityLabel == "4 ounces left over. Throw it in for a little more protein, or toss it.")
        let several = try presentation(PrepPreviewData.severalBags, bags: 3)
        #expect(several.bagAccessibilityLabel(2) == "Freezer bag 2 of 3, 10 ounces, about 3 hours to thaw")
    }

    @Test func changingTheCountAnnouncesIt() throws {
        #expect(try presentation(PrepPreviewData.severalBags, bags: 1).bagCountAnnouncement == "1 bag to freeze")
        #expect(try presentation(PrepPreviewData.severalBags, bags: 2).bagCountAnnouncement == "2 bags to freeze")
    }

    @Test func amountsAreSpokenInWords() {
        #expect(PrepCardPresentation.spoken("10 oz") == "10 ounces")
        #expect(PrepCardPresentation.spoken("1 oz") == "1 ounce")
        #expect(PrepCardPresentation.spoken("3 lb 6 oz") == "3 pounds 6 ounces")
        #expect(PrepCardPresentation.spoken("1 lb 1 oz") == "1 pound 1 ounce")
        #expect(PrepCardPresentation.spoken("2 tbsp") == "2 tbsp")
    }

    // MARK: - Banner

    @Test func theBannerShowsTheFirstItemAtAGlance() throws {
        let session = try #require(PrepPreviewData.session(PrepPreviewData.oneBagJSON))
        let banner = PrepBannerPresentation(session: session)
        #expect(banner.card?.name == "Ground Turkey")
        #expect(banner.amountText == "10 oz this week")
        #expect(banner.detailText == "1 bag to freeze")
        #expect(banner.detailIsFreezer)
        #expect(
            banner.accessibilityLabel == "Put the groceries away, Ground Turkey, 10 ounces this week, 1 bag to freeze")
    }

    @Test func theBannerCountsSeveralItems() throws {
        let session = try #require(
            PrepPreviewData.session(
                PrepPreviewData.severalBagsJSON, PrepPreviewData.noPhotoJSON, PrepPreviewData.leftoverOnlyJSON))
        let banner = PrepBannerPresentation(session: session)
        #expect(banner.amountText == "20 oz this week")
        #expect(banner.detailText == "3 items to put away")
        #expect(!banner.detailIsFreezer)
    }

    @Test func aBannerWithNothingToFreezeSaysOnlyTheAmount() throws {
        let session = try #require(PrepPreviewData.session(PrepPreviewData.leftoverOnlyJSON))
        let banner = PrepBannerPresentation(session: session)
        #expect(banner.amountText == "10 oz this week")
        #expect(banner.detailText == nil)
    }
}
