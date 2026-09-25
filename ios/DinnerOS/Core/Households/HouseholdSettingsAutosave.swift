import Foundation
import Observation

/// When a debounced save goes out, and whether a save's answer is still the newest.
///
/// Pure and clock-free: every call takes the time it happens at, so the rules are tested with
/// plain dates. `HouseholdSettingsAutosave` drives it with a real (injected) clock.
///
/// - Every edit bumps `revision` and restarts the wait: the save goes out `delay` after the
///   *last* edit, so a burst of stepper taps or keystrokes becomes one request.
/// - `flush` makes a pending save due now (return key, leaving a field, a confirmed dialog).
/// - One save is out at a time. A wait that ends while one is out leaves the save due, and it
///   goes as soon as the one out answers, carrying everything changed since.
/// - An answer counts only for the save that is out (`finish` returns `false` otherwise), and
///   a save only marks what it sent as saved: edits made while it was out stay unsaved.
nonisolated struct AutosaveTimeline: Equatable, Sendable {
    /// Bumped by every edit.
    private(set) var revision = 0
    /// The newest revision the server has accepted, or that turned out to need no request.
    private(set) var savedRevision = 0
    /// When the pending save should go out; `nil` when nothing is waiting.
    private(set) var dueAt: Date?
    /// The revision of the save that is out.
    private(set) var inFlight: Int?
    /// The revision of the latest save that failed, until one succeeds.
    private(set) var failedRevision: Int?

    var hasUnsavedEdits: Bool { revision > savedRevision }
    var isSaving: Bool { inFlight != nil }
    var isPending: Bool { dueAt != nil }

    mutating func edit(at now: Date, delay: Duration) {
        revision += 1
        dueAt = now.addingTimeInterval(delay.timeInterval)
    }

    /// Makes unsaved edits due now, including a failed save's (a retry).
    mutating func flush(at now: Date) {
        guard hasUnsavedEdits else { return }
        dueAt = now
    }

    /// Drops a pending save without sending it, for someone who may no longer edit.
    mutating func discardPending() {
        dueAt = nil
    }

    /// How long until the pending save is due; `.zero` when it's due, `nil` when none waits.
    func wait(at now: Date) -> Duration? {
        guard let dueAt else { return nil }
        return .seconds(max(0, dueAt.timeIntervalSince(now)))
    }

    /// Starts the pending save if it's due and nothing else is out, returning the revision it
    /// carries. A due save while another is out stays due.
    mutating func begin(at now: Date) -> Int? {
        guard let dueAt, dueAt <= now, inFlight == nil else { return nil }
        self.dueAt = nil
        guard hasUnsavedEdits else { return nil }
        inFlight = revision
        return revision
    }

    /// Records the answer to the save of `revision`. Returns `false`, changing nothing, for an
    /// answer that isn't to the save that is out.
    mutating func finish(_ sent: Int, succeeded: Bool) -> Bool {
        guard inFlight == sent else { return false }
        inFlight = nil
        if succeeded {
            savedRevision = max(savedRevision, sent)
            failedRevision = nil
        } else {
            failedRevision = sent
        }
        return true
    }
}

nonisolated extension Duration {
    var timeInterval: TimeInterval {
        let (seconds, attoseconds) = components
        return TimeInterval(seconds) + TimeInterval(attoseconds) / 1e18
    }
}

/// The Household tab's settings, edited in place and saved as the member goes.
///
/// There is no Save button: each change waits a moment for the next one (`controlDelay` for
/// pickers and steppers, `typingDelay` for text), then every change since the last save goes
/// out as one `PATCH`. Leaving a text field or pressing return saves straight away.
///
/// What the member typed stays on screen until the server has it: a failed save keeps the
/// draft and says so, with a retry. A server answer only fills in fields someone else changed
/// (`HouseholdSettingsDraft.rebase`), and an answer older than one already applied — by the
/// household's `updatedAt` — is ignored.
@Observable
final class HouseholdSettingsAutosave {
    enum Status: Equatable {
        /// Nothing saved yet on this screen.
        case idle
        /// A save is waiting for the member to pause. Shown as nothing: it's about to go.
        case pending
        /// A save is out.
        case saving
        /// Everything the member changed is saved.
        case saved
        /// The latest save failed; the draft still holds what didn't save.
        case failed(String)
    }

    /// After a picker, toggle, or stepper. Long enough to gather a run of stepper taps or a
    /// second thought on a picker into one request, short enough to feel immediate.
    static let controlDelay: Duration = .milliseconds(800)
    /// After a keystroke. A pause mid-word is usually longer than a keystroke gap (~200 ms) but
    /// much shorter than this; leaving the field or pressing return saves at once anyway.
    static let typingDelay: Duration = .milliseconds(1500)

    let householdID: String
    /// What the screen shows and the member edits.
    private(set) var draft: HouseholdSettingsDraft
    /// The newest household the server has answered with.
    private(set) var confirmed: Household
    /// Without `household.update` the settings are shown, read-only, from `confirmed`.
    private(set) var canEdit: Bool
    private(set) var timeline = AutosaveTimeline()
    /// Why the latest save failed, including which settings it carried.
    private(set) var failureMessage: String?
    /// Bumped by each save the server accepted; `lastSaved` is what that save sent.
    private(set) var saveCount = 0
    private(set) var lastSaved: HouseholdChanges?

    @ObservationIgnored private let save: @MainActor (HouseholdChanges) async throws -> Household
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private let sleep: (Duration) async throws -> Void
    /// What the save that is out sent, so an answer arriving meanwhile leaves those fields alone.
    @ObservationIgnored private var inFlightChanges: HouseholdChanges?
    /// The wait for the pending save; cancelled and replaced by every edit. Never the save
    /// itself: an edit must not cancel a request that is already out.
    @ObservationIgnored private var waiting: Task<Void, Never>?
    /// The latest wait-then-save, for tests to await.
    @ObservationIgnored private(set) var work: Task<Void, Never>?

    init(
        household: Household,
        canEdit: Bool,
        now: @escaping () -> Date = Date.init,
        sleep: @escaping (Duration) async throws -> Void = { try await Task.sleep(for: $0) },
        save: @escaping @MainActor (HouseholdChanges) async throws -> Household
    ) {
        householdID = household.id
        draft = HouseholdSettingsDraft(household)
        confirmed = household
        self.canEdit = canEdit
        self.now = now
        self.sleep = sleep
        self.save = save
    }

    var status: Status {
        if timeline.isSaving { return .saving }
        if timeline.isPending { return .pending }
        if let failureMessage, timeline.hasUnsavedEdits { return .failed(failureMessage) }
        return saveCount > 0 ? .saved : .idle
    }

    // MARK: - Editing

    /// Changes one setting and saves it `delay` after the member's last change.
    func edit<Value: Equatable>(
        _ field: WritableKeyPath<HouseholdSettingsDraft, Value>, to value: Value, delay: Duration = controlDelay
    ) {
        guard canEdit, draft[keyPath: field] != value else { return }
        draft[keyPath: field] = value
        timeline.edit(at: now(), delay: delay)
        schedule()
    }

    /// Saves pending changes now: the member left a text field, pressed return, confirmed a
    /// dialog, or left the screen. Also the retry after a failure.
    func flush() {
        guard canEdit else { return }
        timeline.flush(at: now())
        schedule()
    }

    /// A newer household from a reload. Older than what's applied is ignored; otherwise fields
    /// someone else changed come in unless the member has their own change to them.
    func serverDidChange(_ household: Household, canEdit: Bool) {
        self.canEdit = canEdit
        if !canEdit {
            timeline.discardPending()
            waiting?.cancel()
        }
        apply(household)
    }

    // MARK: - Saving

    private func apply(_ household: Household) {
        guard household.id == householdID, household.updatedAt >= confirmed.updatedAt else { return }
        draft.rebase(from: confirmed, to: household, keeping: inFlightChanges)
        confirmed = household
    }

    private func schedule() {
        waiting?.cancel()
        guard timeline.isPending else { return }
        // Holds on to `self` until it's done, so a change the member made still goes out when
        // the screen closes or another household becomes current.
        let task = Task { await self.waitThenSave() }
        waiting = task
        work = task
    }

    private func waitThenSave() async {
        // Re-checked after every sleep: an edit meanwhile moved the time, or replaced this wait.
        while let wait = timeline.wait(at: now()), wait > .zero {
            guard !Task.isCancelled else { return }
            do { try await sleep(wait) } catch { return }
        }
        guard !Task.isCancelled else { return }
        // From here this task is the save, which a later edit must not cancel.
        waiting = nil
        await saveIfDue()
    }

    private func saveIfDue() async {
        guard canEdit, let revision = timeline.begin(at: now()) else { return }
        let changes = draft.changes(against: confirmed)
        guard !changes.isEmpty else {
            // Changed and changed back, or only an invalid field: nothing to send.
            if timeline.finish(revision, succeeded: true) { failureMessage = nil }
            schedule()
            return
        }
        inFlightChanges = changes
        do {
            let household = try await save(changes)
            inFlightChanges = nil
            if timeline.finish(revision, succeeded: true) {
                failureMessage = nil
                saveCount += 1
                lastSaved = changes
            }
            apply(household)
        } catch {
            inFlightChanges = nil
            if timeline.finish(revision, succeeded: false), !(error is CancellationError) {
                failureMessage = Self.failureMessage(for: changes, error: error)
            }
        }
        // Edits made while this save was out go now if their wait is over, or when it is.
        schedule()
    }

    private static func failureMessage(for changes: HouseholdChanges, error: any Error) -> String {
        let fields = HouseholdSettingsDraft.fieldNames(changes).formatted(.list(type: .and))
        return String(localized: "Couldn't save the \(fields). \(HouseholdStore.message(for: error))")
    }
}
