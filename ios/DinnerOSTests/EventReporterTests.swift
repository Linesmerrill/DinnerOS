import Foundation
import Synchronization
import Testing

@testable import DinnerOS

/// A clock tests move by hand.
final class ManualClock {
    var now: Date

    init(_ start: Date = Date(timeIntervalSince1970: 1_789_473_600)) {
        now = start
    }

    func advance(by seconds: TimeInterval) {
        now = now.addingTimeInterval(seconds)
    }
}

/// An in-memory stand-in for event ingestion. It records every batch and answers with
/// scripted statuses (200 once the script runs out).
nonisolated final class FakeEventServer: Sendable {
    struct State: Sendable {
        var statuses: [Int] = []
        var rejectedIndexes: Set<Int> = []
        var bodies: [Data] = []
        var paths: [String] = []
    }

    private let state = Mutex(State())

    var paths: [String] { state.withLock { $0.paths } }
    var requestCount: Int { state.withLock { $0.bodies.count } }

    /// Each request's events as JSON objects.
    var batches: [[[String: Any]]] {
        state.withLock { $0.bodies }.map(Self.events(in:))
    }

    func script(_ statuses: [Int]) {
        state.withLock { $0.statuses = statuses }
    }

    func reject(indexes: Set<Int>) {
        state.withLock { $0.rejectedIndexes = indexes }
    }

    func handle(_ request: URLRequest) -> (status: Int, body: Data) {
        state.withLock { state in
            state.paths.append(request.url?.path() ?? "")
            state.bodies.append(request.httpBody ?? Data())
            let status = state.statuses.isEmpty ? 200 : state.statuses.removeFirst()
            switch status {
            case 200:
                let count = Self.events(in: request.httpBody ?? Data()).count
                let rejected = state.rejectedIndexes.filter { $0 < count }.sorted()
                let items = rejected.map { #"{"index":\#($0),"message":"payload is not valid"}"# }
                return (
                    200,
                    Data(
                        #"{"accepted":\#(count - rejected.count),"duplicates":0,"rejected":[\#(items.joined(separator: ","))]}"#
                            .utf8)
                )
            case 429:
                return (429, Fixtures.errorJSON(code: "rate_limited"))
            case 500...:
                return (status, Fixtures.errorJSON(code: "internal"))
            default:
                return (status, Fixtures.errorJSON(code: "invalid_request"))
            }
        }
    }

    private static func events(in body: Data) -> [[String: Any]] {
        let object = try? JSONSerialization.jsonObject(with: body) as? [String: Any]
        return object?["events"] as? [[String: Any]] ?? []
    }
}

struct EventReporterTests {
    private struct Harness {
        let reporter: EventReporter
        let server: FakeEventServer
        let clock: ManualClock
    }

    private let week = ISOWeek("2026-W38") ?? .current(in: .gmt)

    private func makeHarness(
        storage: any EventQueueStorage = InMemoryEventQueueStorage(),
        server: FakeEventServer = FakeEventServer(),
        clock: ManualClock = ManualClock(),
        activate: Bool = true
    ) async throws -> Harness {
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let reporter = EventReporter(
            session: session, api: EventsAPI(client: client), storage: storage, now: { clock.now },
            sleep: { _ in throw CancellationError() })
        if activate {
            reporter.activate(householdID: "household-1", userID: Fixtures.user.id)
        }
        return Harness(reporter: reporter, server: server, clock: clock)
    }

    private func entry(id: String = "66e5a1f2c3b4a5d6e7f80c01", date: String? = "2026-09-14") -> PlanEntry {
        PlanEntry(
            id: id,
            recipe: PlanEntryRecipe(id: "66e5a1f2c3b4a5d6e7f80915", name: "Test Kitchen Tacos", imageURLString: nil),
            day: date == nil ? nil : .mon, date: date, servings: 2, note: "private note", addedBy: Fixtures.user.id,
            addedAt: Date(timeIntervalSince1970: 0))
    }

    private func groceryItem(key: String, name: String) -> GroceryItem {
        GroceryItem(
            ingredientKey: key, name: name, amounts: [], quantityText: "", unquantified: false, status: .toBuy,
            recipes: [])
    }

    private func recordCooked(_ reporter: EventReporter, count: Int) {
        for index in 0..<count {
            reporter.recipeCooked(entry(id: "entry-\(index)"), week: week)
        }
    }

    /// The event without its random ID, as sorted JSON.
    private func canonical(_ event: [String: Any]) throws -> String {
        var copy = event
        copy["clientEventId"] = nil
        let data = try JSONSerialization.data(withJSONObject: copy, options: [.sortedKeys, .withoutEscapingSlashes])
        return String(decoding: data, as: UTF8.self)
    }

    // MARK: Request shape

    @Test func sendsEachTypeInTheDocumentedShape() async throws {
        let harness = try await makeHarness()
        let reporter = harness.reporter

        reporter.recipeViewed(recipeID: "66e5a1f2c3b4a5d6e7f80915")
        reporter.recipeCooked(entry(), week: week)
        reporter.recipeSkipped(entry(id: "entry-2", date: nil), week: week, reason: .ateOut)
        reporter.groceryItemChecked(
            groceryItem(key: "66e5a1f2c3b4a5d6e7f80a12", name: "Yellow Onion"), checked: true, week: week)
        reporter.groceryItemChecked(
            groceryItem(key: "name:mystery paste", name: " Mystery Paste "), checked: false, week: week)
        #expect(reporter.queuedEvents.count == 5)
        #expect(!reporter.hasScheduledFlush)

        await reporter.flush()

        #expect(harness.server.paths == ["/api/v1/households/household-1/events"])
        let events = try #require(harness.server.batches.first)
        let at = #""occurredAt":"2026-09-15T12:00:00.000Z""#
        #expect(
            try events.map(canonical) == [
                #"{\#(at),"payload":{"surface":"detail"},"recipeId":"66e5a1f2c3b4a5d6e7f80915","type":"recipe.viewed"}"#,
                #"{\#(at),"payload":{"date":"2026-09-14","entryId":"66e5a1f2c3b4a5d6e7f80c01","servings":2},"#
                    + #""recipeId":"66e5a1f2c3b4a5d6e7f80915","type":"recipe.cooked","week":"2026-W38"}"#,
                #"{\#(at),"payload":{"entryId":"entry-2","reason":"ate-out"},"#
                    + #""recipeId":"66e5a1f2c3b4a5d6e7f80915","type":"recipe.skipped","week":"2026-W38"}"#,
                #"{\#(at),"payload":{"checked":true,"ingredientId":"66e5a1f2c3b4a5d6e7f80a12","name":"Yellow Onion"},"#
                    + #""type":"grocery.item_checked","week":"2026-W38"}"#,
                #"{\#(at),"payload":{"checked":false,"name":"Mystery Paste"},"type":"grocery.item_checked","week":"2026-W38"}"#,
            ])
        let ids = events.compactMap { $0["clientEventId"] as? String }
        #expect(Set(ids.compactMap(UUID.init(uuidString:))).count == 5)
        #expect(reporter.queuedEvents.isEmpty)
        #expect(reporter.outcomes == ["66e5a1f2c3b4a5d6e7f80c01": .cooked, "entry-2": .skipped(.ateOut)])
    }

    @Test func eventsAPIPostsTheBatchAndDecodesTheResult() async throws {
        let transport = StubTransport { _ in
            (
                200,
                Data(
                    #"{"accepted":1,"duplicates":1,"rejected":[{"index":2,"message":"occurredAt must be within the last 30 days"}]}"#
                        .utf8)
            )
        }
        let baseURL = try #require(URL(string: Fixtures.baseURLString))
        let api = EventsAPI(client: APIClient(baseURL: baseURL, transport: transport))
        let event = ClientEvent(
            clientEventID: "0d8f6c1e-2c1a-4c55-9d7e-3f0f1b6a2e11", recipeID: nil, week: nil,
            occurredAt: Date(timeIntervalSince1970: 0),
            payload: .groceryItemChecked(GroceryItemCheckedPayload(ingredientID: nil, name: "Salt", checked: true)))

        let response = try await api.send([event, event, event], householdID: "household-1", accessToken: "token-1")

        let request = try #require(transport.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/v1/households/household-1/events")
        #expect(request.bearerToken == "token-1")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        let body = try #require(request.httpBody)
        let decoded = try JSONCoding.makeDecoder().decode([String: [ClientEvent]].self, from: body)
        #expect(decoded == ["events": [event, event, event]])
        #expect(
            response
                == IngestEventsResponse(
                    accepted: 1, duplicates: 1,
                    rejected: [EventRejection(index: 2, message: "occurredAt must be within the last 30 days")]))
    }

    @Test func payloadsLeaveOutValuesTheAPIWouldReject() {
        let longKey = String(repeating: "k", count: 65)
        let payload = GroceryItemCheckedPayload(
            item: groceryItem(key: longKey, name: String(repeating: "é", count: 150)), checked: true)
        #expect(payload.ingredientID == nil)
        #expect(payload.name?.unicodeScalars.count == EventLimits.maxNameLength)

        let big = PlanEntry(
            id: "e", recipe: PlanEntryRecipe(id: "r", name: "n", imageURLString: nil), day: nil, date: nil,
            servings: 16, note: "", addedBy: "u", addedAt: .now)
        #expect(RecipeCookedPayload(entry: big) == RecipeCookedPayload(entryID: "e", date: nil, servings: nil))
    }

    // MARK: Persistence

    @Test func theQueueSurvivesARelaunch() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "EventQueueTests-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appending(path: "event-queue.json")
        let clock = ManualClock()

        let first = try await makeHarness(storage: FileEventQueueStorage(url: url), clock: clock)
        first.reporter.recipeCooked(entry(), week: week)
        first.reporter.recipeSkipped(entry(id: "entry-2"), week: week, reason: nil)
        first.reporter.groceryItemChecked(groceryItem(key: "i-salt", name: "Salt"), checked: true, week: week)
        clock.advance(by: 1)
        first.reporter.recipeViewed(recipeID: "recipe-9", surface: .plan)
        let recorded = first.reporter.queuedEvents

        let stored = try #require(try FileEventQueueStorage(url: url).load())
        #expect(stored.events == recorded)
        #expect(stored.householdID == "household-1")
        #expect(stored.userID == Fixtures.user.id)

        let relaunched = try await makeHarness(storage: FileEventQueueStorage(url: url), clock: clock, activate: false)
        #expect(relaunched.reporter.queuedEvents.isEmpty)
        relaunched.reporter.activate(householdID: "household-1", userID: Fixtures.user.id)
        #expect(relaunched.reporter.queuedEvents == recorded)
        await relaunched.reporter.flush()

        let sentIDs = try #require(relaunched.server.batches.first).compactMap { $0["clientEventId"] as? String }
        #expect(sentIDs == recorded.map(\.clientEventID))
        #expect(try FileEventQueueStorage(url: url).load()?.events.isEmpty == true)
    }

    @Test func anUnreadableFileStartsEmpty() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "EventQueueTests-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let url = directory.appending(path: "event-queue.json")
        try Data("not json".utf8).write(to: url)

        let harness = try await makeHarness(storage: FileEventQueueStorage(url: url))

        #expect(harness.reporter.queuedEvents.isEmpty)
        harness.reporter.recipeViewed(recipeID: "recipe-1")
        #expect(try FileEventQueueStorage(url: url).load()?.events.count == 1)
    }

    // MARK: Batching

    @Test func sendsAtMost100EventsPerRequestOldestFirst() async throws {
        let harness = try await makeHarness()

        recordCooked(harness.reporter, count: 250)
        await harness.reporter.flush()

        let batches = harness.server.batches
        #expect(batches.map(\.count) == [100, 100, 50])
        let firstPayload = batches.first?.first?["payload"] as? [String: Any]
        #expect(firstPayload?["entryId"] as? String == "entry-0")
        #expect(harness.reporter.queuedEvents.isEmpty)
    }

    @Test func reachingTwentyEventsSchedulesASend() async throws {
        let harness = try await makeHarness()

        recordCooked(harness.reporter, count: EventReporter.flushThreshold - 1)
        #expect(!harness.reporter.hasScheduledFlush)
        harness.reporter.recipeViewed(recipeID: "recipe-1")
        #expect(harness.reporter.hasScheduledFlush)

        await harness.reporter.flush()
        #expect(harness.server.batches.map(\.count) == [20])
        #expect(!harness.reporter.hasScheduledFlush)
    }

    @Test func goingToTheBackgroundSends() async throws {
        let harness = try await makeHarness()
        harness.reporter.recipeViewed(recipeID: "recipe-1")
        harness.reporter.appDidBecomeActive()

        await harness.reporter.appDidEnterBackground()

        #expect(harness.server.requestCount == 1)
        #expect(harness.reporter.queuedEvents.isEmpty)
    }

    // MARK: Failures

    @Test func serverErrorsBackOffExponentially() async throws {
        let harness = try await makeHarness()
        let reporter = harness.reporter
        let clock = harness.clock
        harness.server.script([500, 503])
        reporter.recipeViewed(recipeID: "recipe-1")

        await reporter.flush()
        #expect(harness.server.requestCount == 1)
        #expect(reporter.queuedEvents.count == 1)
        #expect(reporter.retryNotBefore == clock.now.addingTimeInterval(5))

        await reporter.flush()
        clock.advance(by: 4)
        await reporter.flush()
        #expect(harness.server.requestCount == 1)

        clock.advance(by: 1)
        await reporter.flush()
        #expect(harness.server.requestCount == 2)
        #expect(reporter.retryNotBefore == clock.now.addingTimeInterval(10))

        clock.advance(by: 10)
        await reporter.flush()
        #expect(harness.server.requestCount == 3)
        #expect(reporter.queuedEvents.isEmpty)
        #expect(reporter.retryNotBefore == nil)
    }

    @Test func rateLimitingBacksOffLonger() async throws {
        let harness = try await makeHarness()
        let reporter = harness.reporter
        let clock = harness.clock
        harness.server.script([429, 429])
        recordCooked(reporter, count: 3)

        await reporter.flush()
        #expect(reporter.retryNotBefore == clock.now.addingTimeInterval(30))

        clock.advance(by: 29)
        await reporter.flush()
        #expect(harness.server.requestCount == 1)

        clock.advance(by: 1)
        await reporter.flush()
        #expect(harness.server.requestCount == 2)
        #expect(reporter.retryNotBefore == clock.now.addingTimeInterval(60))

        clock.advance(by: 60)
        await reporter.flush()
        #expect(harness.server.batches.last?.count == 3)
        #expect(reporter.queuedEvents.isEmpty)
    }

    @Test func offlineSendsKeepTheQueueAndBackOff() async throws {
        let clock = ManualClock()
        let transport = StubTransport { _ in throw URLError(.notConnectedToInternet) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let reporter = EventReporter(
            session: session, api: EventsAPI(client: client), storage: InMemoryEventQueueStorage(),
            now: { clock.now })
        reporter.activate(householdID: "household-1", userID: Fixtures.user.id)
        reporter.recipeViewed(recipeID: "recipe-1")

        await reporter.flush()

        #expect(reporter.queuedEvents.count == 1)
        #expect(reporter.retryNotBefore == clock.now.addingTimeInterval(5))
    }

    @Test func retryDelaysDoubleUpToACap() {
        #expect(
            [1, 2, 3, 4].map { EventReporter.retryDelay(afterFailures: $0, rateLimited: false) } == [5, 10, 20, 40])
        #expect([1, 2].map { EventReporter.retryDelay(afterFailures: $0, rateLimited: true) } == [30, 60])
        #expect(EventReporter.retryDelay(afterFailures: 12, rateLimited: false) == EventReporter.maxRetryDelay)
        #expect(EventReporter.retryDelay(afterFailures: 500, rateLimited: true) == EventReporter.maxRetryDelay)
    }

    @Test func failuresAreRetriedDiscardedOrLeftForLater() {
        func server(_ status: Int) -> APIError {
            .server(status: status, code: "x", message: "", requestID: nil)
        }
        #expect(EventReporter.disposition(for: server(429)) == .retry(rateLimited: true))
        #expect(EventReporter.disposition(for: server(500)) == .retry(rateLimited: false))
        #expect(EventReporter.disposition(for: server(408)) == .retry(rateLimited: false))
        #expect(EventReporter.disposition(for: APIError.transport(.timedOut)) == .retry(rateLimited: false))
        #expect(EventReporter.disposition(for: server(400)) == .discard)
        #expect(EventReporter.disposition(for: server(404)) == .discard)
        #expect(EventReporter.disposition(for: server(413)) == .discard)
        #expect(EventReporter.disposition(for: APIError.decoding(type: "IngestEventsResponse")) == .discard)
        #expect(EventReporter.disposition(for: server(401)) == .stop)
        #expect(EventReporter.disposition(for: AuthSessionError.signedOut) == .stop)
    }

    @Test func rejectedEventsAreRemovedWithTheBatch() async throws {
        let harness = try await makeHarness()
        harness.server.reject(indexes: [0, 2])
        recordCooked(harness.reporter, count: 3)

        await harness.reporter.flush()
        await harness.reporter.flush()

        #expect(harness.server.requestCount == 1)
        #expect(harness.reporter.queuedEvents.isEmpty)
        #expect(harness.reporter.retryNotBefore == nil)
    }

    @Test func aBatchTheServerRefusesIsDiscarded() async throws {
        let harness = try await makeHarness()
        harness.server.script([400])
        recordCooked(harness.reporter, count: 2)

        await harness.reporter.flush()

        #expect(harness.server.requestCount == 1)
        #expect(harness.reporter.queuedEvents.isEmpty)
        #expect(harness.reporter.retryNotBefore == nil)
    }

    // MARK: Pruning

    @Test func eventsOlderThan29DaysAreDroppedBeforeSending() async throws {
        let harness = try await makeHarness()
        harness.reporter.recipeCooked(entry(id: "entry-old"), week: week)
        harness.clock.advance(by: EventReporter.maxEventAge + 1)
        harness.reporter.recipeCooked(entry(id: "entry-new"), week: week)

        await harness.reporter.flush()

        let sent = try #require(harness.server.batches.first)
        #expect(sent.count == 1)
        #expect((sent.first?["payload"] as? [String: Any])?["entryId"] as? String == "entry-new")
        #expect(harness.reporter.queuedEvents.isEmpty)
    }

    @Test func expiredEventsAreDroppedWhenTheQueueLoads() async throws {
        let clock = ManualClock()
        let old = ClientEvent(
            recipeID: "recipe-1", week: nil, occurredAt: clock.now.addingTimeInterval(-EventReporter.maxEventAge - 60),
            payload: .recipeViewed(RecipeViewedPayload(surface: .detail)))
        let fresh = ClientEvent(
            recipeID: "recipe-2", week: nil, occurredAt: clock.now.addingTimeInterval(-60),
            payload: .recipeViewed(RecipeViewedPayload(surface: .detail)))
        let storage = InMemoryEventQueueStorage(
            snapshot: EventQueueSnapshot(userID: Fixtures.user.id, householdID: "household-1", events: [old, fresh]))

        let harness = try await makeHarness(storage: storage, clock: clock)

        #expect(harness.reporter.queuedEvents.map(\.clientEventID) == [fresh.clientEventID])
        #expect(storage.snapshot?.events == [fresh])
    }

    // MARK: Viewed debounce

    @Test func viewedIsRecordedAtMostOncePerRecipePerTenMinutes() async throws {
        let harness = try await makeHarness()
        let reporter = harness.reporter

        reporter.recipeViewed(recipeID: "recipe-1")
        reporter.recipeViewed(recipeID: "recipe-1")
        reporter.recipeViewed(recipeID: "recipe-2")
        #expect(reporter.queuedEvents.map(\.recipeID) == ["recipe-1", "recipe-2"])

        harness.clock.advance(by: EventReporter.viewedDebounce - 1)
        reporter.recipeViewed(recipeID: "recipe-1")
        #expect(reporter.queuedEvents.count == 2)

        harness.clock.advance(by: 1)
        reporter.recipeViewed(recipeID: "recipe-1")
        #expect(reporter.queuedEvents.map(\.recipeID) == ["recipe-1", "recipe-2", "recipe-1"])
    }

    // MARK: Scope

    @Test func nothingIsRecordedBeforeAHouseholdIsActive() async throws {
        let harness = try await makeHarness(activate: false)

        harness.reporter.recipeViewed(recipeID: "recipe-1")
        harness.reporter.recipeCooked(entry(), week: week)
        await harness.reporter.flush()

        #expect(harness.reporter.queuedEvents.isEmpty)
        #expect(harness.reporter.outcomes.isEmpty)
        #expect(harness.server.requestCount == 0)
    }

    @Test func signingOutDiscardsTheQueueOnDisk() async throws {
        let storage = InMemoryEventQueueStorage()
        let harness = try await makeHarness(storage: storage)
        let reporter = harness.reporter
        reporter.recipeCooked(entry(), week: week)
        reporter.recipeViewed(recipeID: "recipe-1")
        #expect(storage.snapshot?.events.count == 2)

        reporter.reset()

        #expect(reporter.queuedEvents.isEmpty)
        #expect(reporter.outcomes.isEmpty)
        #expect(storage.snapshot == nil)
        reporter.recipeViewed(recipeID: "recipe-2")
        await reporter.flush()
        #expect(reporter.queuedEvents.isEmpty)
        #expect(harness.server.requestCount == 0)

        // After signing back in, the debounce starts fresh.
        reporter.activate(householdID: "household-1", userID: Fixtures.user.id)
        reporter.recipeViewed(recipeID: "recipe-1")
        #expect(reporter.queuedEvents.count == 1)
    }

    @Test func switchingHouseholdsOrUsersDiscardsTheQueue() async throws {
        let storage = InMemoryEventQueueStorage()
        let harness = try await makeHarness(storage: storage)
        let reporter = harness.reporter
        recordCooked(reporter, count: 2)

        reporter.activate(householdID: "household-1", userID: Fixtures.user.id)
        #expect(reporter.queuedEvents.count == 2)

        reporter.activate(householdID: "household-2", userID: Fixtures.user.id)
        #expect(reporter.queuedEvents.isEmpty)
        #expect(reporter.outcomes.isEmpty)
        #expect(storage.snapshot == EventQueueSnapshot(userID: Fixtures.user.id, householdID: "household-2"))

        reporter.recipeViewed(recipeID: "recipe-1")
        await reporter.flush()
        #expect(harness.server.paths == ["/api/v1/households/household-2/events"])

        reporter.recipeViewed(recipeID: "recipe-2")
        reporter.activate(householdID: "household-2", userID: "another-user")
        #expect(reporter.queuedEvents.isEmpty)
        #expect(storage.snapshot?.userID == "another-user")
    }

    @Test func aSendThatFinishesAfterSignOutChangesNothing() async throws {
        let storage = InMemoryEventQueueStorage()
        let gate = AsyncGate()
        let server = FakeEventServer()
        let transport = StubTransport { request in
            await gate.wait()
            return server.handle(request)
        }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let clock = ManualClock()
        let reporter = EventReporter(
            session: session, api: EventsAPI(client: client), storage: storage, now: { clock.now })
        reporter.activate(householdID: "household-1", userID: Fixtures.user.id)
        recordCooked(reporter, count: 2)

        let sending = Task { await reporter.flush() }
        await gate.waitUntilBlocked()
        reporter.reset()
        reporter.activate(householdID: "household-1", userID: Fixtures.user.id)
        reporter.recipeViewed(recipeID: "recipe-1")
        await gate.open()
        await sending.value

        #expect(reporter.queuedEvents.map(\.recipeID) == ["recipe-1"])
        #expect(storage.snapshot?.events.count == 1)
    }
}

/// Holds requests until a test opens it.
actor AsyncGate {
    private var isOpen = false
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var blockedWaiters: [CheckedContinuation<Void, Never>] = []

    func wait() async {
        guard !isOpen else { return }
        await withCheckedContinuation { continuation in
            waiters.append(continuation)
            blockedWaiters.forEach { $0.resume() }
            blockedWaiters = []
        }
    }

    /// Returns once a request is waiting at the gate.
    func waitUntilBlocked() async {
        guard waiters.isEmpty else { return }
        await withCheckedContinuation { blockedWaiters.append($0) }
    }

    func open() {
        isOpen = true
        waiters.forEach { $0.resume() }
        waiters = []
    }
}
