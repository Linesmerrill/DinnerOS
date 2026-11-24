import CoreGraphics

/// How wide the Menu's big meal cards are: Your Meals and Suggested for You share one width, so
/// the rows line up and no suggestion is a banner across an iPad (build 121 review round 2,
/// Hark's item 12: one suggestion was 575 points wide under 360-point meal cards).
///
/// A phone shows one card and a peek of the next. A wider screen shows as many whole cards as
/// fit at a readable size and a quarter of the next one, so a meal past the edge always shows
/// (Hark's item 10: on an iPad in landscape three cards filled the row exactly and the fourth
/// meal had no sign of being there).
enum MealCarouselLayout {
    /// Below this a screen is a phone's.
    static let wideWidth: CGFloat = 600
    /// The narrowest a card gets on a wide screen.
    static let minWideCard: CGFloat = 290
    /// How much of the next card shows at the edge on a wide screen.
    static let peek: CGFloat = 0.25

    /// The number of whole cards a wide row `width` points across shows.
    static func wholeCards(in width: CGFloat) -> Int {
        var count = 1
        while wideCard(in: width, whole: count + 1) >= minWideCard { count += 1 }
        return count
    }

    /// A card's width in a row `width` wide, for a meal card (`phoneShare` of a phone's width).
    static func cardWidth(in width: CGFloat, phoneShare: CGFloat = 0.84) -> CGFloat {
        guard width >= wideWidth else { return width * phoneShare }
        return wideCard(in: width, whole: wholeCards(in: width))
    }

    /// `whole` cards after the leading margin, a gap after each, and `peek` of the next.
    private static func wideCard(in width: CGFloat, whole: Int) -> CGFloat {
        let cards = CGFloat(whole)
        return ((width - DS.Space.l - cards * DS.Space.m) / (cards + peek)).rounded(.down)
    }
}
