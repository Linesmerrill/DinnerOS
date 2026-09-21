import Foundation
import Testing

@testable import DinnerOS

/// `PairingsStore`: reading the week's suggestions, accepting, dismissing, keeping one as a
/// rule, and taking an accepted add-on back off the week.
struct PairingsStoreTests {
    private struct Harness {
        let store: PairingsStore
        let plans: PlanStore
        let server: FakePairingsServer
        let planServer: FakePlanServer
        let planned: PlanRecorder
        let profiles: ProfileRecorder
    }

    /// Plans handed to `planDidChange`.
    @MainActor
    private final class PlanRecorder {
        var plans: [Plan] = []
    }

    /// Profiles handed to `profileDidChange`.
    @MainActor
    private final class ProfileRecorder {
        var profiles: [AutopilotProfile] = []
    }

    private let week = ISOWeek("2026-W38") ?? .current(in: .gmt, weekStartsOn: .mon)

    /// "Now" is Wednesday, September 16, 2026 at noon UTC, a day inside `week`. Without a fixed
    /// clock the plan store opens whichever week the run falls in, so the fixtures' 2026-W38
    /// plan never reaches it and every expectation about its entries fails on some weekdays.
    private func makeHarness(
        server: FakePairingsServer = FakePairingsServer(), planServer: FakePlanServer = FakePlanServer(),
        now: String = "2026-09-16T12:00:00Z"
    ) async throws -> Harness {
        // One transport for both: pairings changes the plan, so the two have to agree.
        let transport = StubTransport { request in
            let path = request.url?.path() ?? ""
            return path.contains("/autopilot/") ? server.handle(request) : planServer.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let instant = try #require(JSONCoding.parseDate(now))
        let plans = PlanStore(
            session: session, api: PlansAPI(client: client), checks: InMemoryGroceryChecks(), now: { instant })
        await plans.activate(householdID: "household-1", timeZone: .gmt, weekStartsOn: .mon)
        let households = HouseholdStore.preview(
            session: session, phase: .ready, current: HouseholdPreviewData.detail)
        let library = RecipeLibrary(session: session, api: RecipesAPI(client: client))
        let planner = MealPlanner(plans: plans, library: library, households: households)
        let store = PairingsStore(
            session: session, api: AutopilotAPI(client: client), plans: plans, planner: planner)
        let planned = PlanRecorder()
        let profiles = ProfileRecorder()
        store.planDidChange = { planned.plans.append($0) }
        store.profileDidChange = { profiles.profiles.append($0) }
        return Harness(
            store: store, plans: plans, server: server, planServer: planServer, planned: planned,
            profiles: profiles)
    }

    private func activated(
        _ server: FakePairingsServer = FakePairingsServer(), planServer: FakePlanServer = FakePlanServer()
    ) async throws -> Harness {
        let harness = try await makeHarness(server: server, planServer: planServer)
        await harness.store.showWeek(week, householdID: "household-1")
        return harness
    }

    // MARK: Reading

    @Test func showWeekLoadsEveryMealsSuggestions() async throws {
        let harness = try await activated()
        let store = harness.store

        #expect(store.week == week)
        #expect(store.pairings.meals.map(\.entryID) == ["entry-1", "entry-2"])
        #expect(store.openSuggestions.count == 2)
        #expect(store.suggestions(forEntry: "entry-1").map(\.key) == ["recipe:addon-1"])
        #expect(store.suggestions(forEntry: "entry-2").map(\.key) == ["grocery:club crackers"])
        #expect(!store.isLoading)
        #expect(!store.isForbidden)
        #expect(
            harness.server.log.contains("GET /households/household-1/autopilot/weeks/2026-W38/pairings"))
    }

    @Test func aForbiddenWeekHidesEverything() async throws {
        let server = FakePairingsServer()
        server.failNext(status: 403, code: "forbidden")
        let harness = try await activated(server)

        #expect(harness.store.isForbidden)
        #expect(harness.store.pairings.isEmpty)
    }

    /// A server without pairings, or a week nobody planned.
    @Test func aNotFoundWeekMeansNoSuggestions() async throws {
        let server = FakePairingsServer()
        server.failNext(status: 404, code: "not_found")
        let harness = try await activated(server)

        #expect(harness.store.pairings.isEmpty)
        #expect(!harness.store.isForbidden)
    }

    // MARK: Accepting

    @Test func acceptingAnAddOnAddsItAndDropsItFromTheSuggestions() async throws {
        let harness = try await activated()
        let store = harness.store

        let result = try #require(try await store.accept(entryID: "entry-1", key: "recipe:addon-1"))

        #expect(result.status == .added)
        #expect(result.entry?.isFromAutopilot == true)
        #expect(result.entry?.day == .tue)
        // The plan the change returned reaches the Menu and the week.
        #expect(harness.planned.plans.count == 1)
        #expect(harness.planned.plans.first?.entries.map(\.recipe.id) == ["addon-1"])
        let body = try #require(
            harness.server.body(of: "POST /households/household-1/autopilot/weeks/2026-W38/pairings/accept"))
        // The server's own key, sent back verbatim.
        #expect(body["key"] as? String == "recipe:addon-1")
        #expect(body["entryId"] as? String == "entry-1")
        // Accepted pairings are left out of later reads.
        #expect(store.suggestions(forEntry: "entry-1").isEmpty)
        #expect(store.openSuggestions.count == 1)
        #expect(!store.isBusy(entryID: "entry-1", key: "recipe:addon-1"))
    }

    @Test func acceptingAGroceryItemPutsItOnTheWeeksList() async throws {
        let harness = try await activated()
        let store = harness.store

        let result = try #require(try await store.accept(entryID: "entry-2", key: "grocery:club crackers"))

        #expect(result.status == .added)
        #expect(result.entry == nil)
        #expect(result.groceryItem?.name == "Club Crackers")
        #expect(store.pairings.groceryItems.map(\.key) == ["grocery:club crackers"])
        #expect(store.groceryLine(key: "grocery:club crackers")?.id == "item-1")
        #expect(store.groceryLine(id: "item-1")?.name == "Club Crackers")
    }

    @Test func acceptingTwiceChangesNothingAndReportsAlreadyAdded() async throws {
        let harness = try await activated()
        let store = harness.store
        _ = try await store.accept(entryID: "entry-1", key: "recipe:addon-1")

        // The API leaves it out of later reads, so a second accept comes from a stale screen.
        let again = try #require(try await store.accept(entryID: "entry-1", key: "recipe:addon-1"))

        #expect(again.status == .alreadyAdded)
        #expect(again.plan.entries.count == 1)
    }

    // MARK: Dismissing

    @Test func dismissingHidesItForThatMealThisWeek() async throws {
        let harness = try await activated()
        let store = harness.store

        try await store.dismiss(entryID: "entry-1", key: "recipe:addon-1")

        #expect(harness.server.dismissedKeys == ["entry-1/recipe:addon-1"])
        #expect(store.suggestions(forEntry: "entry-1").isEmpty)
        // The other meal's suggestion is untouched.
        #expect(store.suggestions(forEntry: "entry-2").count == 1)
        // Nothing was planned.
        #expect(harness.planned.plans.isEmpty)
    }

    // MARK: Rules

    @Test func keepingALearnedPairingAsARuleReturnsTheProfile() async throws {
        let harness = try await activated()

        let result = try #require(
            try await harness.store.makeRule(entryID: "entry-1", key: "recipe:addon-1", frequency: .always))

        #expect(result.status == "created")
        #expect(result.rule?.frequency == .always)
        #expect(harness.profiles.profiles.count == 1)
        #expect(harness.profiles.profiles.first?.pairings.isEmpty == false)
        let body = try #require(
            harness.server.body(of: "POST /households/household-1/autopilot/weeks/2026-W38/pairings/rules"))
        #expect(body["entryId"] as? String == "entry-1")
        #expect(body["key"] as? String == "recipe:addon-1")
        #expect(body["frequency"] as? String == "always")
        // A slot's pairing sends `slotId` instead.
        _ = try await harness.store.makeRule(slotID: "mon", key: "recipe:addon-1")
        let slotBody = try #require(
            harness.server.body(of: "POST /households/household-1/autopilot/weeks/2026-W38/pairings/rules"))
        #expect(slotBody["slotId"] as? String == "mon")
        #expect(slotBody["entryId"] == nil)
    }

    // MARK: Plan, then accept

    /// A pairing always belongs to a planned meal, so the main recipe is planned first and
    /// the pairing accepted against the **new** entry (#312).
    @Test func addingFromTheRecipeScreenPlansTheMealFirstThenAccepts() async throws {
        let harness = try await activated()
        let store = harness.store
        #expect(harness.plans.plan?.entries.isEmpty == true)

        let result = try #require(
            try await store.acceptForRecipe(
                key: "recipe:addon-1", mainRecipeID: "recipe-1", mainRecipeName: "Placeholder Pasta Bake",
                mainEntryID: nil, servings: 2))

        // The meal was planned before the pairing was accepted.
        #expect(harness.planServer.log.contains("POST /households/household-1/plans/2026-W38/entries"))
        let accepted = try #require(
            harness.server.body(of: "POST /households/household-1/autopilot/weeks/2026-W38/pairings/accept"))
        // The entry the plan just created, not a guess.
        #expect(accepted["entryId"] as? String == "entry-1")
        #expect(result.status == .added)
        #expect(harness.plans.plan?.entries.contains { $0.recipe.id == "recipe-1" } == true)
    }

    /// When the recipe is already planned, the carousel's own `entryId` is used and nothing
    /// extra is added to the week.
    @Test func addingFromTheRecipeScreenUsesTheEntryItAlreadyHas() async throws {
        let harness = try await activated()

        let result = try #require(
            try await harness.store.acceptForRecipe(
                key: "recipe:addon-1", mainRecipeID: "recipe-1", mainRecipeName: "Placeholder Pasta Bake",
                mainEntryID: "entry-1"))

        #expect(result.status == .added)
        #expect(!harness.planServer.log.contains("POST /households/household-1/plans/2026-W38/entries"))
    }

    /// The main recipe can't be planned (no such serving size), so nothing is accepted.
    @Test func aFailedPlanMeansNothingIsAccepted() async throws {
        let harness = try await activated()

        let result = try await harness.store.acceptForRecipe(
            key: "recipe:addon-1", mainRecipeID: "recipe-1", mainRecipeName: "Placeholder Pasta Bake",
            mainEntryID: nil, servings: 99)

        #expect(result == nil)
        #expect(
            !harness.server.log.contains(
                "POST /households/household-1/autopilot/weeks/2026-W38/pairings/accept"))
    }

    // MARK: Unchecking

    @Test func uncheckingAnAddOnRemovesItsPlanEntryOnThatMealsDay() async throws {
        let harness = try await activated()
        let store = harness.store
        let accepted = try #require(try await store.accept(entryID: "entry-1", key: "recipe:addon-1"))
        let entryID = try #require(accepted.entry?.id)
        // The plan the accept returned is what the week now shows.
        harness.plans.present(accepted.plan)
        #expect(harness.plans.plan?.entries.map(\.id) == [entryID])

        // The accepted pairing is no longer offered, so unchecking works from what the
        // carousel still shows: the target, not the week's open suggestions.
        let pairing = Pairing(
            key: "recipe:addon-1", target: .recipe(PairingRecipe(id: "addon-1", name: "Sample Garlic Bread")))
        try await store.remove(pairing, mainEntryID: "entry-1")

        #expect(harness.planServer.log.contains("DELETE /households/household-1/plans/2026-W38/entries/\(entryID)"))
    }

    @Test func uncheckingAGroceryItemTakesItOffTheWeeksList() async throws {
        let harness = try await activated()
        let store = harness.store
        _ = try await store.accept(entryID: "entry-2", key: "grocery:club crackers")
        #expect(harness.server.groceryItemIDs == ["item-1"])

        let pairing = Pairing(
            key: "grocery:club crackers",
            target: .groceryItem(PairingGroceryItem(name: "Club Crackers", quantity: 1, unit: "package")))
        try await store.remove(pairing, mainEntryID: "entry-2")

        #expect(
            harness.server.log.contains(
                "DELETE /households/household-1/autopilot/weeks/2026-W38/pairings/grocery-items/item-1"))
        #expect(harness.server.groceryItemIDs.isEmpty)
        #expect(store.pairings.groceryItems.isEmpty)
        // It's offered again once it's off the list.
        #expect(store.groceryLine(key: "grocery:club crackers") == nil)
    }

    @Test func removingAGroceryItemSomeoneElseAlreadyRemovedIsNotAnError() async throws {
        let harness = try await activated()
        _ = try await harness.store.accept(entryID: "entry-2", key: "grocery:club crackers")
        harness.server.update { $0.groceryItems = [:] }

        try await harness.store.removeGroceryItem(id: "item-1")

        #expect(harness.store.pairings.groceryItems.isEmpty)
    }

    // MARK: Failures and reset

    @Test func aFinalizedWeekRejectsAnAcceptAndReloads() async throws {
        let harness = try await activated()
        harness.server.update { $0.planStatus = "finalized" }

        await #expect(throws: APIError.self) {
            try await harness.store.accept(entryID: "entry-1", key: "recipe:addon-1")
        }
        // The suggestion is still there; nothing was added.
        #expect(harness.store.suggestions(forEntry: "entry-1").count == 1)
        #expect(harness.planned.plans.isEmpty)
    }

    @Test func switchingHouseholdsAndResetForgetEverything() async throws {
        let harness = try await activated()
        let store = harness.store
        #expect(!store.pairings.isEmpty)

        // The other household has its own meals, and none of them are paired.
        harness.server.update {
            $0.householdID = "household-2"
            $0.meals = []
        }
        await store.showWeek(week, householdID: "household-2")
        #expect(store.householdID == "household-2")
        #expect(store.pairings.isEmpty)

        store.reset()

        #expect(store.householdID == nil)
        #expect(store.week == nil)
        #expect(store.pairings.isEmpty)
        #expect(!store.isForbidden)
    }
}
