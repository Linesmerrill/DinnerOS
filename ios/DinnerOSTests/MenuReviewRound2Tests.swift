import CoreGraphics
import Testing

@testable import DinnerOS

/// Build 121 review round 2, Hark's Menu items 9, 11 and 13.
@MainActor
struct MenuReviewRound2Tests {
    /// Item 9: By Day lists the meals without a day first, then the whole week from its first
    /// day, past days included, instead of opening on today.
    @Test func byDayShowsTheWholeWeekWithMealsWithoutADayFirst() {
        let parts = WeekView.listParts(weekStartsOn: .sun, hasUnscheduled: true)
        #expect(parts.first == .unscheduled)
        #expect(Array(parts.dropFirst()) == PlanDay.week(startingOn: .sun).map { WeekView.ListPart.day($0) })
        #expect(parts.count == 8)
        let noneWithoutADay = WeekView.listParts(weekStartsOn: .mon, hasUnscheduled: false)
        #expect(noneWithoutADay.count == 7)
        #expect(noneWithoutADay.first == .day(.mon))
    }

    private func card(_ id: String) -> MenuCard {
        MenuCard(recipe: .placeholder(id: id, name: "Meal \(id)", imageURLString: nil))
    }

    /// Item 13: a meal marked made leaves New to You at once, and only New to You.
    @Test func aMadeMealLeavesNewToYou() {
        let newToYou = MenuSection(id: MenuSection.newToYouID, title: "New to You", items: [card("a"), card("b")])
        #expect(newToYou.droppingMade(["a"]).items.map(\.recipe.id) == ["b"])
        #expect(newToYou.droppingMade([]).items.count == 2)
        let favorites = MenuSection(id: "favorites", title: "Your Favorites", items: [card("a")])
        #expect(favorites.droppingMade(["a"]).items.count == 1)
    }

    /// Item 11: rows shorter than the screen sit in a centered column; rows that fill it keep
    /// the plain margin.
    @Test func shortSharedRecipeRowsAreCentered() {
        let width: CGFloat = 1210
        let fits = DiscoverLayout.fittedCount(rowWidth: width, isWide: true)
        let card = DiscoverLayout.fittedCardWidth(rowWidth: width, isWide: true)
        #expect(fits > 3)
        let inset = DiscoverLayout.columnInset(rowWidth: width, isWide: true, longestRow: 3)
        let cards = 3 * card + 2 * DiscoverLayout.rowSpacing
        #expect(abs((width - inset - cards) - inset) <= 1)
        #expect(
            DiscoverLayout.columnInset(rowWidth: width, isWide: true, longestRow: fits + 4)
                == DiscoverLayout.margin(isWide: true))
        let unmeasured = DiscoverLayout.columnInset(rowWidth: 0, isWide: true, longestRow: 3)
        #expect(unmeasured == DiscoverLayout.margin(isWide: true))
    }
}
