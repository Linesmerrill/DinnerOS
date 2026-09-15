import Foundation
import Observation
import os

/// The current household's plan for the week shown in the Week tab.
///
/// Main-actor state that lives as long as the app, like `RecipeLibrary`. Every change
/// shows the server's plan afterward: the plan a response returns, or a reload after a
/// `204`, a `403`, a `404`, or a `409`, so other members' edits and a week finalized
/// elsewhere appear without a manual refresh.
@Observable
final class PlanStore {
    enum Phase: Equatable {
        case idle
        /// Nothing is shown while the week loads.
        case loading
        case loaded
        /// The week failed to load, so there's nothing to show.
        case failed(String)
    }

    private(set) var householdID: String?
    private(set) var week: ISOWeek
    private(set) var phase: Phase = .idle
    private(set) var plan: Plan?
    /// Set when a reload failed while the plan stayed on screen.
    private(set) var refreshError: String?
    /// Changes in flight.
    private(set) var pendingChanges = 0

    var isSaving: Bool { pendingChanges > 0 }

    /// Entries can change only while the plan is a draft. The API enforces it too.
    var isDraft: Bool { plan?.status == .draft }

    /// This week in the household's time zone.
    var currentWeek: ISOWeek { .current(in: timeZone, now: now()) }

    /// Today, when the shown week is this week.
    var today: PlanDay? {
        week == currentWeek ? PlanDay.containing(now(), in: timeZone) : nil
    }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: PlansAPI?
    @ObservationIgnored private let checks: any GroceryCheckStorage
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private var timeZone: TimeZone = .autoupdatingCurrent
    /// Incremented whenever the shown plan is replaced, so a slow response for an old
    /// week, household, or state can't overwrite newer state.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "planning")

    init(
        session: AuthSession, api: PlansAPI?, checks: any GroceryCheckStorage, now: @escaping () -> Date = Date.init
    ) {
        self.session = session
        self.api = api
        self.checks = checks
        self.now = now
        week = .current(in: .autoupdatingCurrent, now: now())
    }

    /// A store frozen with `plan`, for SwiftUI previews. It has no network access.
    static func preview(session: AuthSession, plan: Plan?, phase: Phase = .loaded) -> PlanStore {
        let store = PlanStore(session: session, api: nil, checks: InMemoryGroceryChecks())
        store.householdID = plan?.householdID ?? "household-preview"
        if let week = plan.flatMap({ ISOWeek($0.week) }) {
            store.week = week
        }
        store.plan = plan
        store.phase = phase
        return store
    }

    // MARK: - Loading

    /// Shows `householdID`'s plans, starting at this week in `timeZone`. Loads unless that
    /// household's week is already loaded or loading.
    func activate(householdID: String, timeZone: TimeZone) async {
        self.timeZone = timeZone
        if householdID == self.householdID, phase != .idle {
            return
        }
        if householdID != self.householdID {
            clear()
            self.householdID = householdID
            week = currentWeek
        }
        await load(clearing: true)
    }

    /// Switches to `week` and loads it.
    func show(week newWeek: ISOWeek) async {
        guard newWeek != week || phase == .idle else { return }
        week = newWeek
        await load(clearing: true)
    }

    func showCurrentWeek() async {
        await show(week: currentWeek)
    }

    /// Pull to refresh: the plan stays on screen until the response arrives.
    func reload() async {
        await load(clearing: false)
    }

    private func load(clearing: Bool) async {
        guard let api, let householdID else { return }
        generation += 1
        let started = generation
        let week = week
        if clearing || plan == nil {
            plan = nil
            phase = .loading
        }
        do {
            let loaded = try await session.authorized { token in
                try await api.plan(householdID: householdID, week: week, accessToken: token)
            }
            guard started == generation else { return }
            plan = loaded
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if started == generation, phase == .loading { phase = .idle }
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Plan load failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    // MARK: - Changes

    /// Adds a recipe to `week` (the shown week when `nil`) and returns the new entry.
    @discardableResult
    func addEntry(_ entry: NewPlanEntry, to week: ISOWeek? = nil) async throws -> PlanEntry {
        let target = week ?? self.week
        let response = try await mutate(week: target) { api, householdID, token in
            try await api.addEntry(householdID: householdID, week: target, entry: entry, accessToken: token)
        }
        apply(response.plan, week: target)
        Self.logger.info("Plan entry added")
        return response.entry
    }

    func updateEntry(id: String, changes: PlanEntryChanges) async throws {
        guard !changes.isEmpty else { return }
        let target = week
        let updated = try await mutate(week: target) { api, householdID, token in
            try await api.updateEntry(
                householdID: householdID, week: target, entryID: id, changes: changes, accessToken: token)
        }
        apply(updated, week: target)
    }

    /// Moves an entry to `day`, or unschedules it when `day` is `nil`.
    func moveEntry(id: String, to day: PlanDay?) async throws {
        guard plan?.entries.first(where: { $0.id == id })?.day != day else { return }
        try await updateEntry(id: id, changes: PlanEntryChanges(day: .some(day)))
    }

    /// Removes the entry from the screen at once, then reloads. A failure puts it back.
    /// An entry someone else already removed (`404`) counts as removed.
    func deleteEntry(id: String) async throws {
        let target = week
        let previous = plan
        generation += 1
        let removal = generation
        plan?.entries.removeAll { $0.id == id }
        do {
            try await mutate(week: target) { api, householdID, token in
                try await api.deleteEntry(householdID: householdID, week: target, entryID: id, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            return
        } catch {
            if removal == generation, previous?.week == plan?.week {
                plan = previous
            }
            throw error
        }
        if target == week {
            await load(clearing: false)
        }
    }

    func setStatus(_ status: PlanStatus) async throws {
        let target = week
        let updated = try await mutate(week: target) { api, householdID, token in
            try await api.setStatus(householdID: householdID, week: target, status: status, accessToken: token)
        }
        apply(updated, week: target)
        Self.logger.info("Plan status changed to \(status.rawValue, privacy: .public)")
    }

    /// Weeks from `from` through `to`, for choosing a week to add to. Not stored.
    func summaries(from: ISOWeek, to: ISOWeek) async throws -> [PlanSummary] {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        return try await session.authorized { token in
            try await api.listPlans(householdID: householdID, from: from, to: to, accessToken: token)
        }
    }

    /// A grocery list screen's model for `week` of the current household. Checked-off lines
    /// offer to record purchases through `purchases`; specialty ingredient choices and batches go
    /// through `specialties`.
    func makeGroceryList(
        week: ISOWeek, purchases: (any PantryPurchaseRecording)? = nil,
        specialties: (any SpecialtyChoosing)? = nil, canAddToPantry: Bool = false
    ) -> GroceryListModel? {
        guard let householdID else { return nil }
        return GroceryListModel(
            householdID: householdID, week: week, session: session, api: api, checks: checks, purchases: purchases,
            specialties: specialties, canAddToPantry: canAddToPantry)
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
    }

    private func clear() {
        generation += 1
        phase = .idle
        plan = nil
        refreshError = nil
    }

    // MARK: - Helpers

    /// Runs a change. A rejected change (`403`, `404`, `409`) reloads the shown week when
    /// it's the week changed, because the local plan is probably stale (for example,
    /// finalized by someone else), then rethrows.
    private func mutate<Result: Sendable>(
        week target: ISOWeek, _ operation: (PlansAPI, String, String) async throws -> Result
    ) async throws -> Result {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        pendingChanges += 1
        defer { pendingChanges -= 1 }
        do {
            return try await session.authorized { token in try await operation(api, householdID, token) }
        } catch let error as APIError where [403, 404, 409].contains(error.status) {
            Self.logger.notice("Plan change rejected: \(Self.describe(error), privacy: .public)")
            if target == week, householdID == self.householdID {
                await load(clearing: false)
            }
            throw error
        }
    }

    /// Shows a plan returned by a change when it's for the shown week and household.
    private func apply(_ updated: Plan, week target: ISOWeek) {
        guard target == week, updated.householdID == householdID else { return }
        // Drops a reload that started before the change.
        generation += 1
        plan = updated
        refreshError = nil
        phase = .loaded
    }

    /// Status and error code only; never tokens or response bodies.
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
