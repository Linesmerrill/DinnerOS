import Foundation
import Testing

@testable import DinnerOS

/// The widget's links and its reading of the week.
struct DinnerWidgetTests {
    @Test func linksRoundTrip() {
        for link in [
            DinnerWidgetLink.recipe(id: "abc123", name: "Pad See Ew & Friends"), .autopilot, .shop, .menu,
        ] {
            #expect(DinnerWidgetLink(url: link.url) == link)
        }
        #expect(DinnerWidgetLink(url: URL(string: "dinneros://invite?token=x")!) == nil)
        #expect(DinnerWidgetLink(url: URL(string: "https://example.com/recipe?id=1")!) == nil)
    }

    private func meal(_ id: String, addon: Bool = false) -> DinnerWidgetSnapshot.Meal {
        DinnerWidgetSnapshot.Meal(recipeID: id, name: id, imageURL: nil, minutes: 20, isAddon: addon)
    }

    @Test func tonightAndTheRestOfTheWeek() throws {
        let snapshot = DinnerWidgetSnapshot(
            updatedAt: .now,
            days: [
                .init(date: "2026-09-27", meals: [meal("sun")]),
                .init(date: "2026-09-28", meals: [meal("bread", addon: true), meal("pasta")]),
                .init(date: "2026-09-29", meals: []),
                .init(date: "2026-09-30", meals: [meal("tacos")]),
            ],
            unscheduled: [], nextWeek: [meal("burgers")], timeZone: "America/Phoenix")
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try #require(TimeZone(identifier: "America/Phoenix"))
        let monday = try #require(calendar.date(from: DateComponents(year: 2026, month: 9, day: 28, hour: 18)))
        // The main dish first, the add-on after.
        #expect(snapshot.meals(on: monday).map(\.recipeID) == ["pasta", "bread"])
        #expect(snapshot.upcoming(after: monday).map(\.meal.recipeID) == ["tacos"])
        #expect(snapshot.hasWeek)
        #expect(!DinnerWidgetSnapshot.empty().hasWeek)
    }
}
