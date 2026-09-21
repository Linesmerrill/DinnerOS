import Foundation
import Testing

@testable import DinnerOS

/// The Menu tab's get-started surface (#530): when it shows, what it says about an import
/// that is running, finished, expired or not this member's to start, and what dismissing it
/// remembers.
struct FirstRunRecipesTests {
    /// An empty library, a member who may import, and a server that can.
    private func emptyLibrary(
        canImport: Bool = true, importEnabled: Bool = true, job: MealKitImportJob? = nil
    ) -> FirstRunRecipes.Input {
        FirstRunRecipes.Input(
            libraryLoaded: true, hasRecipes: false, isFiltered: false, isDismissed: false,
            canImport: canImport, importEnabled: importEnabled, job: job)
    }

    @Test func aHouseholdWithRecipesNeverSeesIt() {
        var input = emptyLibrary()
        input.hasRecipes = true

        #expect(FirstRunRecipes.state(for: input) == .hidden)
    }

    /// Recipes win over every other reason to show it, including a finished import that is
    /// still sitting in the status.
    @Test func recipesHideItEvenMidImport() {
        var input = emptyLibrary(job: MealKitImportJob(id: "job-1", status: "running"))
        input.hasRecipes = true

        #expect(FirstRunRecipes.state(for: input) == .hidden)
    }

    @Test func itWaitsForTheLibraryRatherThanGuessing() {
        var input = emptyLibrary()
        input.libraryLoaded = false

        #expect(FirstRunRecipes.state(for: input) == .hidden)
    }

    /// A search that matches nothing is not an empty household.
    @Test func aFilteredListIsNotAnEmptyLibrary() {
        var input = emptyLibrary()
        input.isFiltered = true

        #expect(FirstRunRecipes.state(for: input) == .hidden)
    }

    @Test func dismissingHidesIt() {
        var input = emptyLibrary()
        input.isDismissed = true

        #expect(FirstRunRecipes.state(for: input) == .hidden)
    }

    /// Nothing about the account is stored (#533), so with no run in the status the
    /// sign-in is the only thing to offer.
    @Test func nothingImportedYetOffersTheSignIn() {
        #expect(FirstRunRecipes.state(for: emptyLibrary()) == .offer)
    }

    @Test func aRunInFlightShowsItsProgress() {
        let queued = MealKitImportJob(id: "job-1", status: "queued")
        let running = MealKitImportJob(
            id: "job-1", status: "running", phase: "recipes", recipesFound: 48, recipesDone: 12)

        #expect(FirstRunRecipes.state(for: emptyLibrary(job: queued)) == .importing(queued))
        #expect(FirstRunRecipes.state(for: emptyLibrary(job: running)) == .importing(running))
        #expect(running.progress == 0.25)
    }

    /// Another member's run is this member's progress too: the job belongs to the household.
    @Test func aRunStartedByAnotherMemberStillShows() {
        let running = MealKitImportJob(id: "job-1", status: "running", recipesFound: 10, recipesDone: 1)
        let detail = MealKitFormatting.summary(for: running, service: .helloFresh).detail

        #expect(FirstRunRecipes.state(for: emptyLibrary(job: running)) == .importing(running))
        #expect(detail == "1 of 10 recipes")
    }

    @Test func aStoppedRunSaysWhyAndOffersARetry() {
        let dead = MealKitImportJob(
            id: "job-1", status: "dead",
            lastError: MealKitImportError(code: "parse", message: "The page didn't look like a recipe.", at: .now))

        #expect(
            FirstRunRecipes.state(for: emptyLibrary(job: dead))
                == .stopped(detail: "The page didn't look like a recipe."))
    }

    /// A run with no message still says something true rather than nothing.
    @Test func aStoppedRunWithoutAMessageStillExplainsItself() {
        let dead = MealKitImportJob(id: "job-1", status: "dead")

        guard case .stopped(let detail) = FirstRunRecipes.state(for: emptyLibrary(job: dead)) else {
            Issue.record("a dead job should show as stopped")
            return
        }
        #expect(detail.contains("weren't changed"))
    }

    @Test func aFinishedRunThatAddedRecipesSaysHowMany() {
        let finished = MealKitImportJob(
            id: "job-1", status: "succeeded", phase: "done", recipesFound: 12, recipesDone: 12,
            imported: 10, updated: 1, unchanged: 1)

        #expect(FirstRunRecipes.state(for: emptyLibrary(job: finished)) == .imported(count: 11))
    }

    /// Nothing found is its own state: offering "1 recipe added" for a run that added none
    /// would be the surface lying about the import.
    @Test func aFinishedRunThatFoundNothingSaysSo() {
        let empty = MealKitImportJob(id: "job-1", status: "succeeded", phase: "done", unchanged: 3)

        #expect(FirstRunRecipes.state(for: emptyLibrary(job: empty)) == .foundNothing)
    }

    /// A canceled run leaves nothing behind — nothing about the account is stored (#533) —
    /// so the surface is back to offering the sign-in.
    @Test func aCanceledRunGoesBackToTheOffer() {
        let canceled = MealKitImportJob(id: "job-1", status: "canceled")

        #expect(FirstRunRecipes.state(for: emptyLibrary(job: canceled)) == .offer)
    }

    @Test func aMemberWithoutTheImportPermissionIsOfferedTheCatalogInstead() {
        #expect(
            FirstRunRecipes.state(for: emptyLibrary(canImport: false)) == .unavailable(.noPermission))
        // And their status is never read, because the request would be a guaranteed 403.
        #expect(FirstRunRecipes.readsImportStatus(canImport: false, hasRecipes: false) == false)
        #expect(FirstRunRecipes.readsImportStatus(canImport: true, hasRecipes: false))
        #expect(FirstRunRecipes.readsImportStatus(canImport: true, hasRecipes: true) == false)
    }

    @Test func aServerWithoutImportOffersTheCatalogInstead() {
        #expect(
            FirstRunRecipes.state(for: emptyLibrary(importEnabled: false)) == .unavailable(.notOnThisServer))
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
