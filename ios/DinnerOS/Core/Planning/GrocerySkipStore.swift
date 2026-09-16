import Foundation
import Observation
import os

/// What a grocery list needs to skip and resume ingredients. Narrower than the whole store, so
/// the list model depends on this instead of the app's, exactly as `SpecialtyChoosing` does.
protocol GrocerySkipping: AnyObject {
    /// Incremented after every change, so an open grocery list knows to reload.
    var revision: Int { get }

    /// The household's skip for an ingredient key, when it has one.
    func skip(forIngredientKey key: String) -> GrocerySkip?

    @discardableResult
    func skip(_ request: GrocerySkipRequest, householdID: String?) async throws -> GrocerySkip

    func resume(skipID: String, householdID: String?) async throws
}

/// The ingredients the current household leaves off its grocery list.
///
/// Main-actor state that lives as long as the app, like `SpecialtyStore`. A household skips a
/// handful of ingredients at most (the API caps it at 500 and doesn't paginate), so the whole
/// list loads at once. Every successful change increments `revision`, so an open grocery list
/// reloads and the skipped line disappears from it.
///
/// A skip is not a pantry item and not a check-off: it says the household never *wants* the
/// ingredient, where the pantry says it *has* it and a check says someone *bought* it.
@Observable
final class GrocerySkipStore: GrocerySkipping {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        /// The load failed, so there's nothing to show.
        case failed(String)
    }

    private(set) var householdID: String?
    private(set) var phase: Phase = .idle
    /// Newest first, as the API returns them.
    private(set) var items: [GrocerySkip] = []
    /// Set when a reload failed while the list stayed on screen.
    private(set) var refreshError: String?
    /// Incremented after every change that can alter a grocery list.
    private(set) var revision = 0
    /// Skips being changed or resumed, by ingredient key, so a row can show progress.
    private(set) var busyKeys: Set<String> = []
    /// The role may not change the plan, so skipping is hidden. The API enforces it anyway.
    private(set) var isForbidden = false

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: GrocerySkipsAPI?
    /// The user the skips were loaded for, so another sign-in never sees them.
    @ObservationIgnored private var userID: String?
    /// Incremented whenever the list restarts, so a slow response for an old household can't
    /// overwrite newer state.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "grocery-skips")

    init(session: AuthSession, api: GrocerySkipsAPI?) {
        self.session = session
        self.api = api
    }

    /// A store frozen with the given skips, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, phase: Phase = .loaded, items: [GrocerySkip] = [],
        householdID: String = "household-1"
    ) -> GrocerySkipStore {
        let store = GrocerySkipStore(session: session, api: nil)
        store.householdID = householdID
        store.userID = session.currentUser?.id
        store.phase = phase
        store.items = items
        return store
    }

    // MARK: - Reading

    func skip(forIngredientKey key: String) -> GrocerySkip? {
        items.first { $0.ingredientKey == key }
    }

    /// The skips that end on their own, and the ones that don't. The review screen shows them
    /// apart, because "back next week" and "never again" are different promises.
    var thisWeek: [GrocerySkip] { items.filter { $0.scope == .week } }
    var forever: [GrocerySkip] { items.filter { $0.scope == .always } }

    func isBusy(ingredientKey: String) -> Bool { busyKeys.contains(ingredientKey) }

    // MARK: - Loading

    /// Shows `householdID`'s skips. Loads unless they're already loaded or loading for the
    /// signed-in user.
    func activate(householdID: String) async {
        let currentUserID = session.currentUser?.id
        let isSameList = householdID == self.householdID && currentUserID == userID
        if isSameList, phase != .idle {
            return
        }
        if !isSameList {
            clear()
            self.householdID = householdID
            userID = currentUserID
        }
        await load(showingProgress: true)
    }

    func refresh() async {
        guard householdID != nil else { return }
        await load(showingProgress: false)
    }

    func retry() async {
        guard householdID != nil else { return }
        await load(showingProgress: true)
    }

    private func load(showingProgress: Bool) async {
        guard let api, let householdID else { return }
        generation += 1
        let started = generation
        if showingProgress || phase != .loaded {
            items = []
            phase = .loading
        }
        do {
            let loaded = try await session.authorized { token in
                try await api.list(householdID: householdID, accessToken: token)
            }
            guard started == generation else { return }
            items = loaded
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if started == generation, phase == .loading { phase = .idle }
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Skipped ingredients failed to load")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    // MARK: - Changing

    /// Skips an ingredient, or changes the lifetime of a skip the household already has. The
    /// API replaces rather than stacking, so the result is always the one skip for it.
    @discardableResult
    func skip(_ request: GrocerySkipRequest, householdID: String? = nil) async throws -> GrocerySkip {
        let (api, target) = try require(householdID)
        busyKeys.insert(request.ingredientKey)
        defer { busyKeys.remove(request.ingredientKey) }
        do {
            let stored = try await session.authorized { token in
                try await api.skip(householdID: target, request: request, accessToken: token)
            }
            if target == self.householdID {
                apply(stored)
                changed()
            }
            Self.logger.info("Ingredient skipped")
            return stored
        } catch {
            try noteForbidden(error)
            throw error
        }
    }

    /// Resumes an ingredient.
    func resume(skipID: String, householdID: String? = nil) async throws {
        let (api, target) = try require(householdID)
        let key = items.first { $0.id == skipID }?.ingredientKey
        if let key { busyKeys.insert(key) }
        defer { if let key { busyKeys.remove(key) } }
        do {
            try await session.authorized { token in
                try await api.resume(householdID: target, skipID: skipID, accessToken: token)
            }
            if target == self.householdID {
                items.removeAll { $0.id == skipID }
                changed()
            }
            Self.logger.info("Ingredient resumed")
        } catch let error as APIError where error.status == 404 {
            // Someone else resumed it. The end state is the one we wanted.
            if target == self.householdID {
                items.removeAll { $0.id == skipID }
                changed()
            }
        } catch {
            try noteForbidden(error)
            throw error
        }
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
        userID = nil
    }

    // MARK: - Helpers

    private func require(_ householdID: String?) throws -> (GrocerySkipsAPI, String) {
        guard let api else { throw AuthSessionError.notConfigured }
        guard let target = householdID ?? self.householdID else { throw AuthSessionError.signedOut }
        return (api, target)
    }

    /// Replaces the skip for the same ingredient, or adds it at the front (newest first).
    private func apply(_ skip: GrocerySkip) {
        if let index = items.firstIndex(where: { $0.ingredientKey == skip.ingredientKey }) {
            items[index] = skip
        } else {
            items.insert(skip, at: 0)
        }
    }

    private func changed() {
        revision += 1
    }

    /// Remembers a refused change so the controls can be hidden, then rethrows.
    private func noteForbidden(_ error: any Error) throws {
        if let apiError = error as? APIError, apiError.status == 403 {
            isForbidden = true
        }
    }

    private func clear() {
        generation += 1
        phase = .idle
        items = []
        refreshError = nil
        busyKeys = []
        isForbidden = false
    }
}
