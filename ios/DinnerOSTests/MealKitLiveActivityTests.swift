import Foundation
import Testing

@testable import DinnerOS

/// An `ImportActivityCenter` with no Lock Screen: it records what it was asked to do.
private final class FakeActivityCenter: ImportActivityCenter {
    var activitiesEnabled: Bool
    var running: [MealKitImportActivityAttributes] = []
    var started: [(MealKitImportActivityAttributes, MealKitImportActivityAttributes.ContentState)] = []
    var updated: [(String, MealKitImportActivityAttributes.ContentState)] = []
    var ended: [(String, MealKitImportActivityAttributes.ContentState, Duration)] = []
    var startError: (any Error)?
    var onPushToken: ((MealKitImportActivityAttributes, String) -> Void)?
    var onDismissed: ((MealKitImportActivityAttributes) -> Void)?

    init(enabled: Bool) {
        activitiesEnabled = enabled
    }

    func runningActivities() -> [MealKitImportActivityAttributes] { running }

    func start(
        _ attributes: MealKitImportActivityAttributes, state: MealKitImportActivityAttributes.ContentState
    ) throws {
        if let startError { throw startError }
        started.append((attributes, state))
        running.append(attributes)
    }

    func update(jobID: String, state: MealKitImportActivityAttributes.ContentState) async {
        updated.append((jobID, state))
    }

    func end(jobID: String, state: MealKitImportActivityAttributes.ContentState, linger: Duration) async {
        ended.append((jobID, state, linger))
        running.removeAll { $0.jobID == jobID }
    }
}

private struct ActivityRefused: Error {}

private func job(
    id: String = "job-1", status: String, found: Int = 740, done: Int = 40, imported: Int = 0, updated: Int = 0,
    unchanged: Int = 0, failures: Int = 0
) -> MealKitImportJob {
    MealKitImportJob(
        id: id, status: status, phase: "recipes", recipesFound: found, recipesDone: done, imported: imported,
        updated: updated, unchanged: unchanged,
        failures: (0..<failures).map { MealKitImportFailure(sourceRecipeID: "r\($0)", reason: "unreadable") })
}

/// The meal-kit import's Live Activity: what it shows for each state of a run, and when it starts.
struct MealKitLiveActivityTests {
    typealias State = MealKitImportActivityAttributes.ContentState

    // MARK: - Content state from the job

    @Test func aRunningImportShowsItsCountAndProgress() {
        let state = MealKitLiveActivities.contentState(for: job(status: "running", done: 40))
        #expect(state == State(phase: .importing, done: 40, total: 740))
        #expect(state.headline == "40 of 740 recipes")
        #expect(state.status == "Importing")
        #expect(abs(state.fraction - 40.0 / 740.0) < 0.0001)
        #expect(!state.isFinal)
    }

    @Test func aQueuedImportIsWaitingForItsFirstOrNextBatch() {
        let first = MealKitLiveActivities.contentState(for: job(status: "queued", done: 0))
        #expect(first.phase == .waiting)
        #expect(first.status == "Starting soon")

        let between = MealKitLiveActivities.contentState(for: job(status: "queued", done: 80))
        #expect(between.phase == .waiting)
        #expect(between.status == "Next batch soon")
        #expect(between.headline == "80 of 740 recipes")
    }

    @Test func aFinishedImportIsTheGreenDoneState() {
        let state = MealKitLiveActivities.contentState(
            for: job(status: "succeeded", done: 740, imported: 700, updated: 30, unchanged: 10))
        #expect(state == State(phase: .done, done: 740, total: 740))
        #expect(state.headline == "740 recipes imported")
        #expect(state.status == "All done")
        #expect(state.fraction == 1)
        #expect(state.isFinal && !state.isStopped)

        let withFailures = MealKitLiveActivities.contentState(
            for: job(status: "succeeded", done: 740, imported: 737, failures: 3))
        #expect(withFailures.headline == "737 recipes imported")
        #expect(withFailures.status == "3 couldn’t be read")
    }

    @Test func aFailedOrStoppedImportSaysItStopped() {
        let failed = MealKitLiveActivities.contentState(for: job(status: "dead", done: 120))
        #expect(failed.phase == .failed)
        #expect(failed.headline == "Import stopped")
        #expect(failed.status == "Open DinnerOS to see why")
        #expect(failed.isStopped)

        let canceled = MealKitLiveActivities.contentState(for: job(status: "canceled", done: 120))
        #expect(canceled.phase == .canceled)
        #expect(canceled.headline == "Import stopped")
        #expect(canceled.isStopped)
    }

    @Test func endingsLingerAsLongAsTheServerSays() {
        #expect(MealKitLiveActivities.linger(for: .done) == .seconds(4 * 60 * 60))
        #expect(MealKitLiveActivities.linger(for: .failed) == .seconds(60 * 60))
        #expect(MealKitLiveActivities.linger(for: .canceled) == .zero)
    }

    /// The server's push carries this exact JSON as `content-state` (`internal/liveactivity`).
    @Test func theServersContentStateDecodes() throws {
        let json = Data(#"{"phase":"done","done":737,"total":740,"failed":3}"#.utf8)
        let state = try JSONDecoder().decode(State.self, from: json)
        #expect(state == State(phase: .done, done: 737, total: 740, failed: 3))

        // A phase a newer server invented never claims progress or an ending.
        let unknown = Data(#"{"phase":"rewinding","done":1,"total":2,"failed":0}"#.utf8)
        #expect(try JSONDecoder().decode(State.self, from: unknown).phase == .waiting)

        // And the attributes stay tiny: ActivityKit caps a push payload at 4 KB.
        let attributes = MealKitImportActivityAttributes(
            serviceName: "HelloFresh", service: "hellofresh", householdID: "66e5a1f2c3b4a5d6e7f80a01",
            jobID: "66e5a1f2c3b4a5d6e7f80b01")
        #expect(try JSONEncoder().encode(attributes).count < 256)
        #expect(try JSONEncoder().encode(state).count < 128)
    }

    // MARK: - Starting

    @Test func withLiveActivitiesOffNothingStarts() async {
        let center = FakeActivityCenter(enabled: false)
        let activities = MealKitLiveActivities(center: center, sendToken: nil, dropToken: nil)

        let decision = await activities.importQueued(
            job(status: "queued", done: 0), householdID: "household-1", service: .helloFresh)

        #expect(decision == .disabled)
        #expect(center.started.isEmpty)
    }

    @Test func queueingAnImportStartsAnActivityOnce() async throws {
        let center = FakeActivityCenter(enabled: true)
        let activities = MealKitLiveActivities(center: center, sendToken: nil, dropToken: nil)

        let first = await activities.importQueued(
            job(status: "queued", done: 0), householdID: "household-1", service: .helloFresh)
        let second = await activities.importQueued(
            job(status: "running", done: 10), householdID: "household-1", service: .helloFresh)

        #expect(first == .started)
        #expect(second == .alreadyRunning)
        #expect(center.started.count == 1)
        let (attributes, state) = try #require(center.started.first)
        #expect(attributes.serviceName == "HelloFresh")
        #expect(attributes.service == "hellofresh")
        #expect(attributes.jobID == "job-1")
        #expect(state.phase == .waiting)
        #expect(center.updated.last?.1.done == 10)
    }

    @Test func aRefusedOrPointlessActivityIsNotAnError() async {
        let center = FakeActivityCenter(enabled: true)
        center.startError = ActivityRefused()
        let activities = MealKitLiveActivities(center: center, sendToken: nil, dropToken: nil)

        #expect(
            await activities.importQueued(job(status: "queued"), householdID: "h", service: .helloFresh) == .failed)
        #expect(
            await activities.importQueued(job(status: "succeeded"), householdID: "h", service: .helloFresh)
                == .notWorking)
    }

    @Test func anOlderRunsActivityEndsWhenANewRunStarts() async {
        let center = FakeActivityCenter(enabled: true)
        center.running = [
            MealKitImportActivityAttributes(
                serviceName: "HelloFresh", service: "hellofresh", householdID: "household-1", jobID: "old-job")
        ]
        let activities = MealKitLiveActivities(center: center, sendToken: nil, dropToken: nil)

        await activities.importQueued(job(status: "queued"), householdID: "household-1", service: .helloFresh)

        #expect(center.ended.map(\.0) == ["old-job"])
        #expect(center.started.map(\.0.jobID) == ["job-1"])
    }

    // MARK: - Updating from the poll

    @Test func thePollUpdatesAndThenEndsTheActivity() async {
        let center = FakeActivityCenter(enabled: true)
        let activities = MealKitLiveActivities(center: center, sendToken: nil, dropToken: nil)
        await activities.importQueued(job(status: "queued", done: 0), householdID: "h", service: .helloFresh)

        await activities.statusLoaded(job(status: "running", done: 50))
        await activities.statusLoaded(job(status: "succeeded", done: 740, imported: 740))

        #expect(center.updated.map(\.1.done) == [50])
        let ended = center.ended.first
        #expect(ended?.1.phase == .done)
        #expect(ended?.2 == .seconds(4 * 60 * 60))
    }

    @Test func thePollNeverStartsAnActivity() async {
        let center = FakeActivityCenter(enabled: true)
        let activities = MealKitLiveActivities(center: center, sendToken: nil, dropToken: nil)

        await activities.statusLoaded(job(status: "running", done: 50))

        #expect(center.started.isEmpty && center.updated.isEmpty && center.ended.isEmpty)
    }

    // MARK: - Tokens

    @Test func everyPushTokenIsHandedToTheServer() async throws {
        let center = FakeActivityCenter(enabled: true)
        let received = AsyncStream<String>.makeStream()
        _ = MealKitLiveActivities(
            center: center,
            sendToken: { householdID, service, jobID, token in
                received.continuation.yield("\(householdID) \(service) \(jobID) \(token)")
            }, dropToken: nil)
        let attributes = MealKitImportActivityAttributes(
            serviceName: "HelloFresh", service: "hellofresh", householdID: "household-1", jobID: "job-1")

        center.onPushToken?(attributes, "aa11")
        center.onPushToken?(attributes, "bb22")  // rotated

        var iterator = received.stream.makeAsyncIterator()
        #expect(await iterator.next() == "household-1 hellofresh job-1 aa11")
        #expect(await iterator.next() == "household-1 hellofresh job-1 bb22")
    }

    @Test func registeringATokenPutsItInTheBodyOfTheJobsRoute() async throws {
        let transport = StubTransport { _ in (204, Data()) }
        let api = MealKitAPI(
            client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))

        try await api.registerLiveActivity(
            householdID: "household-1", service: "hellofresh", jobID: "job-1", token: "abcdef0123456789",
            environment: .sandbox, accessToken: "access-1")
        try await api.unregisterLiveActivity(
            householdID: "household-1", service: "hellofresh", jobID: "job-1", accessToken: "access-1")

        let put = try #require(transport.requests.first)
        #expect(put.httpMethod == "PUT")
        #expect(put.url?.path() == "/api/v1/households/household-1/meal-kit/hellofresh/imports/job-1/live-activity")
        #expect(put.url?.query() == nil)
        let body = try #require(put.httpBody)
        let fields = try #require(try JSONSerialization.jsonObject(with: body) as? [String: String])
        #expect(fields == ["token": "abcdef0123456789", "environment": "sandbox"])
        #expect(transport.requests.last?.httpMethod == "DELETE")
    }

    // MARK: - The store

    @Test func theImportGoesAheadExactlyAsBeforeWithLiveActivitiesOff() async throws {
        let transport = StubTransport { _ in
            (
                202,
                Data(
                    """
                    {"id":"job-9","source":"hellofresh","status":"queued","phase":"recipes",
                     "recipesFound":740,"recipesDone":0,"imported":0,"updated":0,"unchanged":0,
                     "reviewItems":0,"failures":[],"attempts":0,"maxAttempts":5,"lastError":null,
                     "createdAt":"2026-09-27T10:00:00Z","updatedAt":"2026-09-27T10:00:00Z","finishedAt":null}
                    """.utf8)
            )
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let store = MealKitImportStore(session: session, api: MealKitAPI(client: client))
        store.activate(householdID: "household-1")
        let center = FakeActivityCenter(enabled: false)
        store.liveActivities = MealKitLiveActivities(center: center, sendToken: nil, dropToken: nil)

        try await store.startImport(harvest: MealKitHarvest(recipes: []))

        #expect(store.job?.id == "job-9")
        #expect(store.isImporting)
        #expect(center.started.isEmpty)
        #expect(transport.requests.count == 1)  // the import itself, and no token call
    }
}
