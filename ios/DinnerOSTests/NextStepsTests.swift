import Foundation
import Testing

@testable import DinnerOS

/// Synthetic steps shaped like the API's. `nonisolated` so the stub transport's closures can
/// read them.
private nonisolated enum NextStepFixtures {
    static let autopilot = NextStep(
        id: NextStep.autopilotSetup, path: ["household", "planning", "autopilotPreferences"])
    static let importReview = NextStep(
        id: NextStep.importReview, path: ["household", "planning", "importReview"])
    static let freeMonth = NextStep(id: NextStep.freeMonthSteps, path: ["household", "giveFreeMonth"])

    static func json(_ steps: [NextStep]) -> Data {
        let items = steps.map { step in
            #"{"id":"\#(step.id)","path":[\#(step.path.map { "\"\($0)\"" }.joined(separator: ","))]}"#
        }
        return Data(#"{"items":[\#(items.joined(separator: ","))]}"#.utf8)
    }
}

/// Next-step dots (decisions 736-739): which rows a step's path badges, and clearing a step the
/// moment the app finishes it.
struct NextStepsTests {
    private func makeStore(_ transport: StubTransport) async throws -> NextStepsStore {
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        return NextStepsStore(session: session, api: NextStepsAPI(client: client))
    }

    // MARK: - Paths

    @Test func autopilotStepBadgesTheTabThePlanningRowAndAutopilotPreferences() {
        let store = NextStepsStore.preview(
            session: AuthSession(api: nil, store: InMemoryTokenStore()), steps: [NextStepFixtures.autopilot])
        #expect(store.badgedRows == [.household, .planning, .autopilotPreferences])
        #expect(store.shows(.importReview) == false)
        #expect(store.shows(.giveFreeMonth) == false)
    }

    @Test func everyRowKeyMatchesTheAPI() {
        #expect(
            NextStepRow.allCases.map(\.rawValue) == [
                "household", "planning", "autopilotPreferences", "importReview", "giveFreeMonth",
            ])
    }

    @Test func aStepThroughARowThisVersionDoesntHaveShowsNoDots() {
        let newer = NextStep(id: "orderDay", path: ["household", "planning", "orderDay"])
        #expect(newer.rows == nil)
        #expect(NextStep(id: "empty", path: []).rows == nil)
        let store = NextStepsStore.preview(session: AuthSession(api: nil, store: InMemoryTokenStore()), steps: [newer])
        #expect(store.badgedRows.isEmpty)
    }

    @Test func onlyTheHouseholdTabCarriesADot() {
        let store = NextStepsStore.preview(
            session: AuthSession(api: nil, store: InMemoryTokenStore()), steps: [NextStepFixtures.freeMonth])
        #expect(AppTab.allCases.filter { store.shows(tab: $0.nextStepRow) } == [.household])
    }

    // MARK: - Loading

    @Test func loadReadsTheHouseholdsSteps() async throws {
        let transport = StubTransport { _ in
            (200, NextStepFixtures.json([NextStepFixtures.autopilot, NextStepFixtures.importReview]))
        }
        let store = try await makeStore(transport)
        store.activate(householdID: "household-1")
        await store.load()

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path() == "/api/v1/households/household-1/next-steps")
        #expect(store.pending == [NextStepFixtures.autopilot, NextStepFixtures.importReview])
        #expect(store.badgedRows == [.household, .planning, .autopilotPreferences, .importReview])
    }

    @Test func anAPIWithoutNextStepsShowsNoDots() async throws {
        let transport = StubTransport { _ in (404, Data(#"{"error":{"code":"not_found","message":"no"}}"#.utf8)) }
        let store = try await makeStore(transport)
        store.activate(householdID: "household-1")
        await store.load()
        #expect(store.badgedRows.isEmpty)
    }

    @Test func aFailedLoadKeepsTheDots() async throws {
        let calls = Counter()
        let transport = StubTransport { _ in
            calls.increment()
            return calls.value > 1 ? (500, Data()) : (200, NextStepFixtures.json([NextStepFixtures.autopilot]))
        }
        let store = try await makeStore(transport)
        store.activate(householdID: "household-1")
        await store.load()
        await store.load()
        #expect(calls.value >= 2)
        #expect(store.shows(.autopilotPreferences))
    }

    @Test func anotherHouseholdDropsTheOldDots() async throws {
        let transport = StubTransport { _ in (200, NextStepFixtures.json([NextStepFixtures.autopilot])) }
        let store = try await makeStore(transport)
        store.activate(householdID: "household-1")
        await store.load()
        store.activate(householdID: "household-2")
        #expect(store.badgedRows.isEmpty)
    }

    // MARK: - Clearing

    @Test func finishingAStepClearsItsDotsAtOnce() {
        let store = NextStepsStore.preview(
            session: AuthSession(api: nil, store: InMemoryTokenStore()),
            steps: [NextStepFixtures.autopilot, NextStepFixtures.importReview])
        store.finish(NextStep.autopilotSetup)
        #expect(store.shows(.autopilotPreferences) == false)
        // Planning still leads to Import Review.
        #expect(store.shows(.planning))
        #expect(store.shows(.household))
        store.finish(NextStep.importReview)
        #expect(store.badgedRows.isEmpty)
    }

    @Test func aFinishedStepStaysHiddenWhileASavedAnswerStillListsIt() {
        let store = NextStepsStore.preview(
            session: AuthSession(api: nil, store: InMemoryTokenStore()), steps: [NextStepFixtures.autopilot])
        store.finish(NextStep.autopilotSetup)
        // Offline, the last saved answer comes back with the step still in it.
        store.apply([NextStepFixtures.autopilot])
        #expect(store.badgedRows.isEmpty)
        // The API agrees it's done: nothing to remember any more.
        store.apply([])
        #expect(store.finished.isEmpty)
        // Pending again later (a new import, say) shows again.
        store.apply([NextStepFixtures.autopilot])
        #expect(store.shows(.autopilotPreferences))
    }

    @Test func reopeningAStepBringsItsDotsBack() {
        let store = NextStepsStore.preview(
            session: AuthSession(api: nil, store: InMemoryTokenStore()), steps: [NextStepFixtures.importReview])
        store.finish(NextStep.importReview)
        store.reopen(NextStep.importReview)
        #expect(store.shows(.importReview))
    }

    @Test func finishingAStepThatIsntPendingChangesNothing() {
        let store = NextStepsStore.preview(session: AuthSession(api: nil, store: InMemoryTokenStore()), steps: [])
        store.finish(NextStep.freeMonthSteps)
        #expect(store.finished.isEmpty)
        // So a later answer that lists it shows it.
        store.apply([NextStepFixtures.freeMonth])
        #expect(store.shows(.giveFreeMonth))
    }
}
