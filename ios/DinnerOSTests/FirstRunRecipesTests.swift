import Foundation
import Testing

@testable import DinnerOS

/// The Menu tab's get-started surface (#530): when it shows, what it says about an import
/// that is running, finished, expired or not this member's to start, and what dismissing it
/// remembers.
struct FirstRunRecipesTests {
    private let link = MealKitLink(
        status: "active", accountLabel: "HelloFresh account", linkedAt: .distantPast, updatedAt: .distantPast)
    private let expiredLink = MealKitLink(
        status: "needs_reauth", accountLabel: "HelloFresh account", linkedAt: .distantPast,
        updatedAt: .distantPast)

    /// An empty library, a member who may import, and a server that can.
    private func emptyLibrary(
        canImport: Bool = true, importEnabled: Bool = true, link: MealKitLink? = nil,
        job: MealKitImportJob? = nil
    ) -> FirstRunRecipes.Input {
        FirstRunRecipes.Input(
            libraryLoaded: true, hasRecipes: false, isFiltered: false, isDismissed: false,
            canImport: canImport, importEnabled: importEnabled, link: link, job: job)
    }

    @Test func aHouseholdWithRecipesNeverSeesIt() {
        var input = emptyLibrary()
        input.hasRecipes = true

        #expect(FirstRunRecipes.state(for: input) == .hidden)
    }

    /// Recipes win over every other reason to show it, including a finished import that is
    /// still sitting in the status.
    @Test func recipesHideItEvenMidImport() {
        var input = emptyLibrary(link: link, job: MealKitImportJob(id: "job-1", status: "running"))
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

    @Test func nothingLinkedOffersTheImport() {
        #expect(FirstRunRecipes.state(for: emptyLibrary()) == .offer)
    }

    @Test func aLinkedAccountThatNeverRanOffersToRunNow() {
        #expect(FirstRunRecipes.state(for: emptyLibrary(link: link)) == .linked)
    }

    @Test func aRunInFlightShowsItsProgress() {
        let queued = MealKitImportJob(id: "job-1", status: "queued")
        let running = MealKitImportJob(
            id: "job-1", status: "running", phase: "recipes", recipesFound: 48, recipesDone: 12)

        #expect(FirstRunRecipes.state(for: emptyLibrary(link: link, job: queued)) == .importing(queued))
        #expect(FirstRunRecipes.state(for: emptyLibrary(link: link, job: running)) == .importing(running))
        #expect(running.progress == 0.25)
    }

    /// Another member's run is this member's progress too: the job belongs to the household.
    @Test func aRunStartedByAnotherMemberStillShows() {
        let running = MealKitImportJob(id: "job-1", status: "running", recipesFound: 10, recipesDone: 1)
        let detail = MealKitFormatting.summary(for: running, service: .helloFresh).detail

        #expect(FirstRunRecipes.state(for: emptyLibrary(link: link, job: running)) == .importing(running))
        #expect(detail == "1 of 10 recipes")
    }

    @Test func anExpiredSessionAsksForAnotherSignIn() {
        let paused = MealKitImportJob(
            id: "job-1", status: "paused_auth",
            lastError: MealKitImportError(code: "auth_expired", message: "Your session expired.", at: .distantPast))

        #expect(
            FirstRunRecipes.state(for: emptyLibrary(link: expiredLink, job: paused))
                == .needsSignIn(detail: "Your session expired."))
        // And before any run: the link itself says the session is gone.
        guard case .needsSignIn(let detail) = FirstRunRecipes.state(for: emptyLibrary(link: expiredLink)) else {
            Issue.record("an expired link should ask for a sign-in")
            return
        }
        #expect(detail.contains("expired"))
    }

    @Test func aStoppedRunSaysWhyAndOffersARetry() {
        let dead = MealKitImportJob(
            id: "job-1", status: "dead",
            lastError: MealKitImportError(code: "parse", message: "The page didn't look like a recipe.", at: .now))

        #expect(
            FirstRunRecipes.state(for: emptyLibrary(link: link, job: dead))
                == .stopped(detail: "The page didn't look like a recipe."))
    }

    /// A run with no message still says something true rather than nothing.
    @Test func aStoppedRunWithoutAMessageStillExplainsItself() {
        let dead = MealKitImportJob(id: "job-1", status: "dead")

        guard case .stopped(let detail) = FirstRunRecipes.state(for: emptyLibrary(link: link, job: dead)) else {
            Issue.record("a dead job should show as stopped")
            return
        }
        #expect(detail.contains("weren't changed"))
    }

    @Test func aFinishedRunThatAddedRecipesSaysHowMany() {
        let finished = MealKitImportJob(
            id: "job-1", status: "succeeded", phase: "done", recipesFound: 12, recipesDone: 12,
            imported: 10, updated: 1, unchanged: 1)

        #expect(FirstRunRecipes.state(for: emptyLibrary(link: link, job: finished)) == .imported(count: 11))
    }

    /// Nothing found is its own state: offering "1 recipe added" for a run that added none
    /// would be the surface lying about the import.
    @Test func aFinishedRunThatFoundNothingSaysSo() {
        let empty = MealKitImportJob(id: "job-1", status: "succeeded", phase: "done", unchanged: 3)

        #expect(FirstRunRecipes.state(for: emptyLibrary(link: link, job: empty)) == .foundNothing)
    }

    @Test func anUnlinkedRunFallsBackToTheLinkItself() {
        let canceled = MealKitImportJob(id: "job-1", status: "canceled")

        #expect(FirstRunRecipes.state(for: emptyLibrary(link: link, job: canceled)) == .linked)
        #expect(
            FirstRunRecipes.state(for: emptyLibrary(link: expiredLink, job: canceled))
                == .needsSignIn(detail: "Your meal-kit sign-in expired. Sign in again to carry on importing."))
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
