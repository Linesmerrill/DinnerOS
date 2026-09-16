import Foundation
import Testing

@testable import DinnerOS

struct MealPlannerTests {
    private struct Harness {
        let planner: MealPlanner
        let plans: PlanStore
        let planServer: FakePlanServer
    }

    private func makeHarness() async throws -> Harness {
        let planServer = FakePlanServer()
        let recipeServer = FakeRecipeServer(
            .init(recipes: ["household-1": [.init(id: "recipe-1", name: "Test Kitchen Tacos")]]))
        let transport = StubTransport { request in
            request.url?.path().contains("/plans") == true
                ? planServer.handle(request) : recipeServer.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let plans = PlanStore(session: session, api: PlansAPI(client: client), checks: InMemoryGroceryChecks())
        let library = RecipeLibrary(session: session, api: RecipesAPI(client: client))
        // The preview household's usual size is 4.
        let households = HouseholdPreviewData.store(session: session)
        await plans.activate(householdID: "household-1", timeZone: .gmt)
        await library.activate(householdID: "household-1")
        let planner = MealPlanner(plans: plans, library: library, households: households)
        return Harness(planner: planner, plans: plans, planServer: planServer)
    }

    @Test func addUsesTheUsualServingsAndUndoRemovesIt() async throws {
        let harness = try await makeHarness()
        let planner = harness.planner
        let plans = harness.plans

        let entry = try #require(
            await planner.add(recipeID: "recipe-1", name: "Test Kitchen Tacos", to: plans.week, day: .wed))

        #expect(entry.servings == 4)
        #expect(entry.day == .wed)
        #expect(planner.entries(recipeID: "recipe-1").map(\.id) == [entry.id])
        #expect(planner.toast?.message == "Added Test Kitchen Tacos")
        #expect(planner.toast?.undo == .remove(entryID: entry.id))
        #expect(planner.busyRecipeIDs.isEmpty)

        await planner.undo()

        #expect(planner.toast == nil)
        #expect(plans.plan?.entries.isEmpty == true)
    }

    @Test func servingsStepThroughSupportedSizesAndRemoveBelowTheSmallest() async throws {
        let harness = try await makeHarness()
        let planner = harness.planner
        let plans = harness.plans
        let added = try #require(
            await planner.add(recipeID: "recipe-1", name: "Tacos", to: plans.week, servings: 2, showsToast: false))
        #expect(planner.toast == nil)

        await planner.changeServings(added, by: 1)
        let bigger = try #require(plans.plan?.entries.first)
        #expect(bigger.servings == 4)

        // Already the largest size: nothing is sent.
        let requests = harness.planServer.log.count
        await planner.changeServings(bigger, by: 1)
        #expect(harness.planServer.log.count == requests)

        await planner.changeServings(bigger, by: -1)
        let smaller = try #require(plans.plan?.entries.first)
        #expect(smaller.servings == 2)

        await planner.changeServings(smaller, by: -1)
        #expect(plans.plan?.entries.isEmpty == true)
        guard case .restore(let restore, _) = planner.toast?.undo else {
            Issue.record("expected an undo that restores the meal")
            return
        }
        #expect(restore.servings == 2)

        await planner.undo()
        #expect(plans.plan?.entries.map(\.servings) == [2])
    }

    @Test func failedAddShowsAnError() async throws {
        let harness = try await makeHarness()
        harness.planServer.failNext()

        let entry = await harness.planner.add(
            recipeID: "recipe-1", name: "Tacos", to: harness.plans.week, servings: 2)

        #expect(entry == nil)
        #expect(harness.planner.errorMessage != nil)
        #expect(harness.planner.toast == nil)
    }

    @Test func dismissingAnOldToastKeepsTheNewOne() async throws {
        let harness = try await makeHarness()
        let planner = harness.planner
        await planner.add(recipeID: "recipe-1", name: "Tacos", to: harness.plans.week, servings: 2)
        let first = try #require(planner.toast)
        await planner.add(recipeID: "recipe-1", name: "Tacos", to: harness.plans.week, servings: 4)
        let second = try #require(planner.toast)

        planner.dismissToast(first.id)
        #expect(planner.toast?.id == second.id)

        planner.reset()
        #expect(planner.toast == nil)
    }

    @Test func switchingHouseholdsDropsTheOtherOnesToastAndError() async throws {
        let harness = try await makeHarness()
        let planner = harness.planner
        planner.activate(householdID: "household-1")
        await planner.add(recipeID: "recipe-1", name: "Tacos", to: harness.plans.week, servings: 2)
        planner.errorMessage = "Couldn't change your meals."
        #expect(planner.toast != nil)

        planner.activate(householdID: "household-2")

        #expect(planner.householdID == "household-2")
        #expect(planner.toast == nil)
        #expect(planner.errorMessage == nil)

        // Undo has nothing left to send: the entry belonged to the household we left.
        let requests = harness.planServer.log.count
        await planner.undo()
        #expect(harness.planServer.log.count == requests)

        // Coming back doesn't bring the old toast with it.
        planner.activate(householdID: "household-1")
        #expect(planner.toast == nil)
    }
}
