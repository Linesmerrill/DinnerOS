import SwiftUI
import Testing

@testable import DinnerOS

/// Build 122, Hark's items 7, 9 and 16: nothing on the Menu or in Shared Recipes rests cut
/// mid-text at the content's edge, and a search result fits above the keyboard.
@MainActor
struct Build122WholeCardsTests {
    /// iPhone SE, the Duo's cover screen content, iPhone 17 Pro, iPhone 17 Pro Max.
    nonisolated private static let phoneWidths: [CGFloat] = [320, 375, 382, 402, 440]

    // MARK: - Week strip (#9)

    @Test(arguments: phoneWidths)
    func aPhoneStripShowsWholeWeeksOnly(stripWidth: CGFloat) {
        for size: DynamicTypeSize in [.large, .xxxLarge, .accessibility1] {
            let pill = WeekStripMetrics.pillWidth(stripWidth: stripWidth, typeSize: size)
            let row = stripWidth - WeekStripMetrics.margin * 2
            let count = ((row + WeekStripMetrics.spacing) / (pill + WeekStripMetrics.spacing)).rounded(.down)
            let used = count * pill + (count - 1) * WeekStripMetrics.spacing
            // What's left over is less than a point per pill: no part of a third week shows.
            #expect(row - used < count, "\(row - used) points of a cut-off week at \(stripWidth)")
            #expect(pill >= WeekStripMetrics.minimumPillWidth)
        }
    }

    @Test(arguments: phoneWidths)
    func aPhoneStripStillShowsNextWeekWhole(stripWidth: CGFloat) {
        let pill = WeekStripMetrics.pillWidth(stripWidth: stripWidth, typeSize: .large)
        let nextEnd = WeekStripMetrics.margin + pill * 2 + WeekStripMetrics.spacing
        #expect(nextEnd <= stripWidth - WeekStripMetrics.margin)
    }

    // MARK: - Shared Recipes rows (#16)

    @Test(arguments: [343.0, 375.0, 382.0, 402.0, 440.0, 744.0, 1032.0, 1210.0])
    func aSharedRecipesRowFitsWholeCards(rowWidth: Double) {
        let width = CGFloat(rowWidth)
        let isWide = width >= 700
        let card = DiscoverLayout.fittedCardWidth(rowWidth: width, isWide: isWide)
        let available = width - DiscoverLayout.margin(isWide: isWide) * 2
        let count = ((available + DiscoverLayout.rowSpacing) / (card + DiscoverLayout.rowSpacing)).rounded(.down)
        let used = count * card + (count - 1) * DiscoverLayout.rowSpacing
        #expect(count >= 1)
        #expect(available - used < count, "\(available - used) points of a cut-off card at \(width)")
        #expect(card >= DiscoverLayout.gridMinimum)
    }

    @Test func anUnmeasuredRowUsesThePlainCardWidth() {
        #expect(DiscoverLayout.fittedCardWidth(rowWidth: 0, isWide: false) == DiscoverLayout.cardWidth(isWide: false))
        #expect(DiscoverLayout.fittedCardWidth(rowWidth: 0, isWide: true) == DiscoverLayout.cardWidth(isWide: true))
    }

    /// The Duo's cover screen showed one card and a half, cut mid-title.
    @Test func theDuoShowsTwoWholeCards() {
        let card = DiscoverLayout.fittedCardWidth(rowWidth: 375, isWide: false)
        #expect(DiscoverLayout.margin(isWide: false) * 2 + card * 2 + DiscoverLayout.rowSpacing <= 375)
    }

    // MARK: - Search above the keyboard (#7)

    @Test func searchResultsAreRowsOnlyWhileTyping() {
        #expect(AllMealsLayout.showsSearchRows(isSearching: true, isKeyboardUp: true))
        #expect(!AllMealsLayout.showsSearchRows(isSearching: true, isKeyboardUp: false))
        #expect(!AllMealsLayout.showsSearchRows(isSearching: false, isKeyboardUp: true))
    }
}
