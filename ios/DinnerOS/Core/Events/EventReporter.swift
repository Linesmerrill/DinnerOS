import Foundation
import Observation
import os

/// Records what the user does in the app and sends it to the household's event history
/// for recommendations.
///
/// Main-actor state that lives as long as the app, like `PlanStore`. Events go into an
/// offline-safe queue on disk and are sent in batches:
///
/// - when the app goes to the background, every 60 seconds while it's active, and as
///   soon as 20 events are waiting;
/// - at most 100 events per request, each with a UUID `clientEventId`, so a retry after
///   a lost response is stored once;
/// - after a failure, retries wait with exponential backoff, longer after `429`;
/// - events older than 29 days are dropped before sending, because the API rejects
///   anything older than 30.
///
/// A `200` removes the whole batch, including rejected events, which can never succeed.
/// Only counts are logged, never payloads or rejection messages. The queue belongs to
/// one user and household, and is discarded on sign-out or when the household changes.
@Observable
final class EventReporter {
    /// What the user said happened to a planned meal, shown on its row.
    typealias EntryOutcome = MealOutcome

    /// Outcomes by plan entry ID, with when each was answered.
    ///
    /// Stored, not derived: it survives a relaunch with the queue, because the event that
    /// carried the answer is deleted as soon as the API accepts it while the answer itself is
    /// what the cards keep showing.
    private(set) var recordedOutcomes: [String: RecordedOutcome] = [:]

    /// Outcomes by plan entry ID.
    var outcomes: [String: EntryOutcome] {
        recordedOutcomes.mapValues(\.outcome)
    }

    static let maxBatchSize = EventLimits.maxBatchSize
    static let flushThreshold = 20
    static let flushInterval = Duration.seconds(60)
    /// A day under the API's 30-day limit, so an event isn't rejected in flight.
    static let maxEventAge: TimeInterval = 29 * 86_400
    /// How long a recipe stays on screen before it counts as viewed.
    static let viewDwell = Duration.seconds(3)
    /// At most one `recipe.viewed` per recipe in this window.
    static let viewedDebounce: TimeInterval = 10 * 60
    /// The oldest events are dropped beyond this, so a long offline stretch can't grow
    /// the file without bound.
    static let maxQueuedEvents = 500
    /// How long a recorded outcome is kept on device. Longer than `maxEventAge`, which exists
    /// because the API rejects old events: an outcome is a local record of an answer, and the
    /// Menu can show a week from months ago, which should still say what happened.
    static let maxOutcomeAge: TimeInterval = 120 * 86_400
    /// How long after answering the answer can still be taken back, while its event is only
    /// queued and the API hasn't deducted anything from the pantry.
    static let undoWindow: TimeInterval = 8
    static let baseRetryDelay: TimeInterval = 5
    /// The API refills one batch every 10 seconds after a burst of 30.
    static let rateLimitedRetryDelay: TimeInterval = 30
    static let maxRetryDelay: TimeInterval = 15 * 60

    /// Events waiting to be sent, oldest first.
    var queuedEvents: [ClientEvent] { snapshot.events }
    /// When the next send may start after a failure; `nil` when there's no backoff.
    @ObservationIgnored private(set) var retryNotBefore: Date?
    var hasScheduledFlush: Bool { flushTask != nil }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: EventsAPI?
    @ObservationIgnored private let storage: any EventQueueStorage
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private let sleep: (Duration) async throws -> Void

    @ObservationIgnored private var snapshot = EventQueueSnapshot()
    @ObservationIgnored private var hasLoaded = false
    /// Whether the current user and household are confirmed, so events may be recorded
    /// and sent.
    @ObservationIgnored private var isActive = false
    @ObservationIgnored private var lastViewed: [String: Date] = [:]
    @ObservationIgnored private var consecutiveFailures = 0
    @ObservationIgnored private var flushTask: Task<Void, Never>?
    @ObservationIgnored private var periodicTask: Task<Void, Never>?
    /// Incremented when the queue is discarded, so a send that finishes afterwards
    /// can't change the new queue.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "events")

    init(
        session: AuthSession,
        api: EventsAPI?,
        storage: any EventQueueStorage,
        now: @escaping () -> Date = Date.init,
        sleep: @escaping (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.session = session
        self.api = api
        self.storage = storage
        self.now = now
        self.sleep = sleep
    }

    /// A reporter that records nothing, for SwiftUI previews.
    static func preview(session: AuthSession) -> EventReporter {
        EventReporter(session: session, api: nil, storage: InMemoryEventQueueStorage())
    }

    // MARK: - Scope

    /// Starts recording for `householdID` as `userID`. Queued events for anyone else or
    /// another household are discarded; the rest are sent.
    func activate(householdID: String, userID: String) {
        loadIfNeeded()
        if snapshot.householdID != householdID || snapshot.userID != userID {
            if !snapshot.events.isEmpty {
                Self.logger.info("Discarded \(self.snapshot.events.count, privacy: .public) events for another scope")
            }
            discard(keeping: EventQueueSnapshot(userID: userID, householdID: householdID))
            persist()
        }
        isActive = true
        if !snapshot.events.isEmpty {
            startFlush()
        }
    }

    /// Discards the queue, on disk too, and stops recording until the next `activate`.
    /// For sign-out and leaving the last household.
    func reset() {
        hasLoaded = true
        isActive = false
        discard(keeping: EventQueueSnapshot())
        do {
            try storage.clear()
        } catch {
            Self.logger.error("Event queue clear failed: \(String(describing: type(of: error)), privacy: .public)")
        }
    }

    private func discard(keeping empty: EventQueueSnapshot) {
        generation += 1
        flushTask?.cancel()
        flushTask = nil
        snapshot = empty
        lastViewed = [:]
        recordedOutcomes = empty.outcomes
        consecutiveFailures = 0
        retryNotBefore = nil
    }

    // MARK: - Recording

    /// Records `recipe.viewed`, at most once per recipe per `viewedDebounce`.
    func recipeViewed(recipeID: String, surface: RecipeViewSurface = .detail) {
        guard isActive else { return }
        let date = now()
        if let last = lastViewed[recipeID], date.timeIntervalSince(last) < Self.viewedDebounce {
            return
        }
        lastViewed[recipeID] = date
        record(.recipeViewed(RecipeViewedPayload(surface: surface)), recipeID: recipeID, week: nil)
    }

    func recipeCooked(_ entry: PlanEntry, week: ISOWeek) {
        guard isActive else { return }
        record(.recipeCooked(RecipeCookedPayload(entry: entry)), recipeID: entry.recipe.id, week: week)
        setOutcome(.cooked, for: entry.id)
    }

    func recipeSkipped(_ entry: PlanEntry, week: ISOWeek, reason: SkipReason?) {
        guard isActive else { return }
        record(
            .recipeSkipped(RecipeSkippedPayload(entry: entry, reason: reason)), recipeID: entry.recipe.id, week: week)
        setOutcome(.skipped(reason), for: entry.id)
    }

    private func setOutcome(_ outcome: EntryOutcome, for entryID: String) {
        recordedOutcomes[entryID] = RecordedOutcome(outcome: outcome, recordedAt: now())
        pruneExpiredOutcomes()
        persist()
    }

    /// Whether the answer for `entryID` can still be taken back: it was answered within
    /// `undoWindow` and its event hasn't been accepted by the API yet.
    ///
    /// Once the event is sent the pantry has already been deducted, and undoing here would
    /// only hide that from the person who could still fix it.
    func canUndoOutcome(entryID: String) -> Bool {
        guard let recorded = recordedOutcomes[entryID] else { return false }
        guard now().timeIntervalSince(recorded.recordedAt) < Self.undoWindow else { return false }
        return snapshot.events.contains { $0.entryID == entryID && $0.type.isOutcome }
    }

    /// Takes back a cooked or skipped answer, dropping the queued event so the API never sees
    /// it. Does nothing once the event has been sent. Returns whether anything was undone.
    @discardableResult
    func undoOutcome(entryID: String) -> Bool {
        guard canUndoOutcome(entryID: entryID) else { return false }
        snapshot.events.removeAll { $0.entryID == entryID && $0.type.isOutcome }
        recordedOutcomes[entryID] = nil
        persist()
        return true
    }

    func groceryItemChecked(_ item: GroceryItem, checked: Bool, week: ISOWeek) {
        guard isActive else { return }
        record(.groceryItemChecked(GroceryItemCheckedPayload(item: item, checked: checked)), recipeID: nil, week: week)
    }

    private func record(_ payload: ClientEventPayload, recipeID: String?, week: ISOWeek?) {
        snapshot.events.append(
            ClientEvent(recipeID: recipeID, week: week?.description, occurredAt: now(), payload: payload))
        let overflow = snapshot.events.count - Self.maxQueuedEvents
        if overflow > 0 {
            snapshot.events.removeFirst(overflow)
            Self.logger.notice("Event queue full; dropped \(overflow, privacy: .public) oldest events")
        }
        persist()
        if snapshot.events.count >= Self.flushThreshold {
            startFlush()
        }
    }

    // MARK: - Sending

    /// The app became active: sends on a timer until it goes to the background.
    func appDidBecomeActive() {
        guard periodicTask == nil else { return }
        let interval = Self.flushInterval
        periodicTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let sleep = self?.sleep else { return }
                do {
                    try await sleep(interval)
                } catch {
                    return
                }
                await self?.flush()
            }
        }
    }

    /// The app went to the background: stops the timer and sends what's queued.
    func appDidEnterBackground() async {
        periodicTask?.cancel()
        periodicTask = nil
        await flush()
    }

    /// Sends queued events unless a retry delay is running. Returns when the send, or one
    /// already in flight, finishes.
    func flush() async {
        startFlush()
        await flushTask?.value
    }

    private func startFlush() {
        guard flushTask == nil, isActive, api != nil, !snapshot.events.isEmpty else { return }
        let started = generation
        flushTask = Task {
            await sendQueuedEvents(generation: started)
            if started == generation {
                flushTask = nil
            }
        }
    }

    private func sendQueuedEvents(generation started: Int) async {
        guard let api, let householdID = snapshot.householdID else { return }
        pruneExpiredEvents()
        while !snapshot.events.isEmpty, started == generation {
            if let retryNotBefore, now() < retryNotBefore {
                return
            }
            let batch = Array(snapshot.events.prefix(Self.maxBatchSize))
            do {
                let response = try await session.authorized { token in
                    try await api.send(batch, householdID: householdID, accessToken: token)
                }
                guard started == generation else { return }
                remove(batch)
                consecutiveFailures = 0
                retryNotBefore = nil
                Self.logger.info(
                    """
                    Sent \(batch.count, privacy: .public) events: \(response.accepted, privacy: .public) accepted, \
                    \(response.duplicates, privacy: .public) duplicates, \(response.rejected.count, privacy: .public) rejected
                    """)
            } catch is CancellationError {
                return
            } catch {
                guard started == generation else { return }
                switch Self.disposition(for: error) {
                case .retry(let rateLimited):
                    consecutiveFailures += 1
                    let delay = Self.retryDelay(afterFailures: consecutiveFailures, rateLimited: rateLimited)
                    retryNotBefore = now().addingTimeInterval(delay)
                    Self.logger.notice(
                        "Event send failed (\(Self.describe(error), privacy: .public)); retrying in \(Int(delay), privacy: .public)s"
                    )
                    return
                case .discard:
                    remove(batch)
                    Self.logger.error(
                        "Event batch of \(batch.count, privacy: .public) discarded: \(Self.describe(error), privacy: .public)"
                    )
                case .stop:
                    return
                }
            }
        }
    }

    private func remove(_ batch: [ClientEvent]) {
        let sent = Set(batch.map(\.clientEventID))
        snapshot.events.removeAll { sent.contains($0.clientEventID) }
        persist()
    }

    private func pruneExpiredEvents() {
        let cutoff = now().addingTimeInterval(-Self.maxEventAge)
        let count = snapshot.events.count
        snapshot.events.removeAll { $0.occurredAt < cutoff }
        let pruned = count - snapshot.events.count
        if pruned > 0 {
            Self.logger.info("Dropped \(pruned, privacy: .public) events older than 29 days")
            persist()
        }
    }

    enum FailureDisposition: Equatable {
        /// Keep the batch and try again after a delay.
        case retry(rateLimited: Bool)
        /// Remove the batch: the server can never accept it, or already did.
        case discard
        /// Keep the batch and wait for the next trigger (signed out, or sign-in rejected).
        case stop
    }

    static func disposition(for error: any Error) -> FailureDisposition {
        switch error {
        case let apiError as APIError:
            switch apiError {
            case .server(let status, _, _, _) where status == 429:
                .retry(rateLimited: true)
            case .server(let status, _, _, _) where status == 401:
                .stop
            case .server(let status, _, _, _) where status == 408 || status >= 500:
                .retry(rateLimited: false)
            case .server:
                // 400, 403, 404, 413: resending the same batch gets the same answer.
                .discard
            case .decoding:
                // A 2xx the app couldn't read; the events were stored.
                .discard
            case .transport, .invalidResponse:
                .retry(rateLimited: false)
            }
        case is AuthSessionError:
            .stop
        default:
            .retry(rateLimited: false)
        }
    }

    /// Doubles from the base delay with each consecutive failure, up to `maxRetryDelay`.
    static func retryDelay(afterFailures failures: Int, rateLimited: Bool) -> TimeInterval {
        let base = rateLimited ? rateLimitedRetryDelay : baseRetryDelay
        let exponent = Double(min(max(failures - 1, 0), 20))
        return min(base * pow(2, exponent), maxRetryDelay)
    }

    // MARK: - Persistence

    private func loadIfNeeded() {
        guard !hasLoaded else { return }
        hasLoaded = true
        do {
            if let stored = try storage.load() {
                snapshot = stored
                recordedOutcomes = stored.outcomes
            }
        } catch {
            Self.logger.error("Event queue unreadable; starting empty")
            try? storage.clear()
        }
        pruneExpiredEvents()
        pruneExpiredOutcomes()
    }

    private func pruneExpiredOutcomes() {
        let cutoff = now().addingTimeInterval(-Self.maxOutcomeAge)
        recordedOutcomes = recordedOutcomes.filter { $0.value.recordedAt >= cutoff }
    }

    private func persist() {
        snapshot.outcomes = recordedOutcomes
        do {
            try storage.save(snapshot)
        } catch {
            // Events still send this launch; they just won't survive a relaunch.
            Self.logger.error("Event queue write failed: \(String(describing: type(of: error)), privacy: .public)")
        }
    }

    /// Status and error code only; never tokens, payloads, or response bodies.
    private static func describe(_ error: any Error) -> String {
        switch error as? APIError {
        case .server(let status, let code, _, let requestID):
            "\(status) \(code) request=\(requestID ?? "-")"
        case .transport(let code):
            "transport \(code.rawValue)"
        case .invalidResponse:
            "invalid response"
        case .decoding(let type):
            "decoding \(type)"
        case nil:
            String(describing: type(of: error))
        }
    }
}
