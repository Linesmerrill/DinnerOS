import Foundation
import Testing

@testable import DinnerOS

/// The Menu tab's get-started card (#530): when it shows, and what dismissing it remembers.
struct FirstRunRecipesTests {
    /// An empty, loaded, unfiltered library that nobody dismissed.
    private func emptyLibrary() -> FirstRunRecipes.Input {
        FirstRunRecipes.Input(libraryLoaded: true, hasRecipes: false, isFiltered: false, isDismissed: false)
    }

    @Test func anEmptyLibraryShowsIt() {
        #expect(FirstRunRecipes.shows(emptyLibrary()))
    }

    @Test func aHouseholdWithRecipesNeverSeesIt() {
        var input = emptyLibrary()
        input.hasRecipes = true

        #expect(!FirstRunRecipes.shows(input))
    }

    @Test func itWaitsForTheLibraryRatherThanGuessing() {
        var input = emptyLibrary()
        input.libraryLoaded = false

        #expect(!FirstRunRecipes.shows(input))
    }

    /// A search that matches nothing is not an empty household.
    @Test func aFilteredListIsNotAnEmptyLibrary() {
        var input = emptyLibrary()
        input.isFiltered = true

        #expect(!FirstRunRecipes.shows(input))
    }

    @Test func dismissingHidesIt() {
        var input = emptyLibrary()
        input.isDismissed = true

        #expect(!FirstRunRecipes.shows(input))
    }

    /// A member who dismissed it in one household still gets it in another, and nobody else
    /// on the account is dismissed for them.
    @Test func dismissalIsPerMemberAndPerHousehold() throws {
        let suite = "FirstRunRecipesTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let dismissals = UserDefaultsFirstRunDismissals(defaults: defaults)

        #expect(dismissals.isDismissed(userID: "user-ada", householdID: "household-1") == false)
        dismissals.dismiss(userID: "user-ada", householdID: "household-1")

        #expect(dismissals.isDismissed(userID: "user-ada", householdID: "household-1"))
        #expect(dismissals.isDismissed(userID: "user-ada", householdID: "household-2") == false)
        #expect(dismissals.isDismissed(userID: "user-charles", householdID: "household-1") == false)
    }

    /// It survives a new store over the same defaults — the whole point of writing it down.
    @Test func dismissalOutlivesTheScreen() throws {
        let suite = "FirstRunRecipesTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        UserDefaultsFirstRunDismissals(defaults: defaults).dismiss(userID: "user-ada", householdID: "household-1")

        #expect(
            UserDefaultsFirstRunDismissals(defaults: defaults)
                .isDismissed(userID: "user-ada", householdID: "household-1"))
    }

    @Test func theInMemoryRecordBehavesLikeTheStoredOne() {
        let dismissals = InMemoryFirstRunDismissals()

        #expect(dismissals.isDismissed(userID: "user-ada", householdID: "household-1") == false)
        dismissals.dismiss(userID: "user-ada", householdID: "household-1")
        #expect(dismissals.isDismissed(userID: "user-ada", householdID: "household-1"))
        #expect(dismissals.isDismissed(userID: "user-ada", householdID: "household-2") == false)
    }
}
