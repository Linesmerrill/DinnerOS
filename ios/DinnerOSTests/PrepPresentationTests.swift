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
        #expect(one.keepCaption == "for Tuesday’s Citrus Turkey Tacos")
        #expect(one.bagText == "10 oz")
        #expect(one.thawText == "about 3 hr to thaw")
        let several = try presentation(PrepPreviewData.severalBags)
        #expect(several.keepText == "20 oz")
        #expect(several.keepCaption == "for Tuesday’s Citrus Pork Tacos and Thursday’s Pork Fried Rice")
    }

    @Test func aCardWithoutAPhotoHasNoURL() throws {
        #expect(try #require(PrepPreviewData.noPhoto).imageURL == nil)
        #expect(try #require(PrepPreviewData.oneBag).imageURL?.absoluteString.hasPrefix("https://") == true)
        // Every thumbnail without a photo shows the same fallback, whatever its aisle.
        #expect(IngredientFallbackGlyph.symbol == "fork.knife")
        #expect(IngredientFallbackGlyph.symbol == DS.Colors.thumbnailGlyphSymbol)
    }

    // MARK: - VoiceOver

    @Test func eachCardReadsAsOneSentence() throws {
        let one = try presentation(PrepPreviewData.oneBag)
        #expect(one.keepAccessibilityLabel == "Keep 10 ounces of ground turkey out for Tuesday’s Citrus Turkey Tacos")
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
        #expect(banner.listsItems)
        #expect(banner.countTitle == "7 bags to freeze")
        #expect(banner.moreText == nil)
        // Each item says what to do with it, so the member knows which is which at a glance.
        #expect(banner.rows.map(\.card.name) == ["Ground Pork", "Chicken Thighs"])
        // An older server sends no rest: each says the bag size, and the extra under it.
        #expect(banner.rows.map(\.glanceText) == ["4 bags, 10 oz each", "3 bags, 12 oz each"])
        #expect(banner.rows.map(\.glanceNote) == ["Plus 4 oz extra.", nil])
        // Less than a bag isn't something to put away: it's listed last, with why under it.
        #expect(banner.extraRows.map(\.card.name) == ["Ground Beef"])
        #expect(banner.extraRows.map(\.extraText) == ["4 oz"])
        #expect(banner.extraRows.map(\.glanceText) == ["4 oz extra"])
        #expect(banner.extraRows.map(\.glanceNote) == ["Too little to freeze. Cook it in or toss it."])
        let spoken =
            "Put the groceries away. 7 bags to freeze. Ground Pork, 4 bags, 10 ounces each. Plus 4 ounces extra. "
            + "Chicken Thighs, 3 bags, 12 ounces each. "
            + "Ground Beef, 4 ounces extra. Too little to freeze. Cook it in or toss it"
        #expect(banner.accessibilityLabel == spoken)
    }

    /// The API's own words win at the suggested count: what's left after the week, then how
    /// it splits, so the two lines add up (Hark, build 122 round 5: "2 bags, 10 oz each" with
    /// "You already have 9 oz." added up to nothing). What was on hand stays off the banner.
    @Test func theBannerUsesTheAPIsWordsAndTheyAddUp() throws {
        let json = PrepPreviewData.severalBagsJSON.replacingOccurrences(
            of: #""instruction":"""#,
            with: #""glanceText":"44 oz left after this week","glanceNote":"Freeze 4 bags of 10 oz, 4 oz extra.","#
                + #""restText":"44 oz","onHandText":"9 oz","instruction":"""#)
        let card = try #require(PrepPreviewData.session(json)?.cards.first)
        let suggested = try presentation(card)
        #expect(suggested.glanceText == "44 oz left after this week")
        #expect(suggested.glanceNote == "Freeze 4 bags of 10 oz, 4 oz extra.")
        // Another count builds its own words: the same rest, split the new way.
        let two = try presentation(card, bags: 2)
        #expect(two.glanceText == "44 oz left after this week")
        #expect(two.glanceNote == "Freeze 2 bags of 10 oz, 24 oz extra.")
        let one = try presentation(card, bags: 1)
        #expect(one.glanceNote == "Freeze 1 bag of 10 oz, 34 oz extra.")
        for row in [suggested, two, one] {
            #expect(row.glanceNote?.contains("9 oz") == false)
        }
        #expect(
            PrepCardPresentation.spoken("Freeze 2 bags of 10 oz, 3 oz extra.")
                == "Freeze 2 bags of 10 ounces, 3 ounces extra.")
    }

    /// Every bag spoken for: the note says only the bags.
    @Test func theNoteWithNothingExtraSaysOnlyTheBags() throws {
        let json = PrepPreviewData.oneBagJSON.replacingOccurrences(
            of: #""instruction":"""#, with: #""restText":"10 oz","instruction":"""#
        ).replacingOccurrences(of: #""leftoverText":"4 oz""#, with: #""leftoverText":"""#)
        let card = try #require(PrepPreviewData.session(json)?.cards.first)
        let row = try presentation(card)
        #expect(row.glanceText == "10 oz left after this week")
        #expect(row.glanceNote == "Freeze 1 bag of 10 oz.")
    }

    @Test func aBannerWithOnlyExtrasFreezesNothing() throws {
        let session = try #require(
            PrepPreviewData.session(
                PrepPreviewData.leftoverOnlyJSON,
                PrepPreviewData.leftoverOnlyJSON.replacingOccurrences(of: "l3\"", with: "l8\"")))
        let banner = PrepBannerPresentation(session: session)
        #expect(banner.countTitle == "A little extra this week")
        #expect(banner.rows.isEmpty)
        #expect(banner.extraRows.count == 2)
    }

    @Test func aLongBannerListsFourAndCountsTheRest() throws {
        let session = try #require(
            PrepPreviewData.session(
                PrepPreviewData.severalBagsJSON, PrepPreviewData.noPhotoJSON, PrepPreviewData.leftoverOnlyJSON,
                PrepPreviewData.oneBagJSON,
                PrepPreviewData.oneBagJSON.replacingOccurrences(of: "l1\"", with: "l9\"")))
        let banner = PrepBannerPresentation(session: session)
        #expect(banner.rows.count == 4)
        #expect(banner.moreText == "And 1 more")
    }

    @Test func aBannerWithNothingToFreezeSaysOnlyTheAmount() throws {
        let session = try #require(PrepPreviewData.session(PrepPreviewData.leftoverOnlyJSON))
        let banner = PrepBannerPresentation(session: session)
        #expect(banner.amountText == "10 oz this week")
        #expect(banner.detailText == nil)
    }
}
