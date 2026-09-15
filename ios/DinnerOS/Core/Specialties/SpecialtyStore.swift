import Foundation
import Observation
import os

/// The current household's specialty ingredients and their choices.
///
/// Main-actor state that lives as long as the app, like `PantryStore`. The list is small (the
/// curated set), so it loads at once. Changes apply the specialty ingredient the API returns, or
/// reload that one ingredient after a change that returns nothing. A rejected change (403, 404,
/// 409) reloads the list, because it usually means the list is stale.
///
/// Every successful change increments `revision`, so an open grocery list knows to reload, and
/// a recorded batch hands its pantry item to `onBatchRecorded`.
@Observable
final class SpecialtyStore {
    enum Phase: Equatable {
        case idle
        /// Nothing is shown while the list loads.
        case loading
        case loaded
        /// The load failed, so there's nothing to show.
        case failed(String)
    }

    private(set) var householdID: String?
    private(set) var phase: Phase = .idle
    /// In the API's order: most used first, then by name.
    private(set) var items: [SpecialtyIngredient] = []
    /// Set when a reload failed while items stayed on screen.
    private(set) var refreshError: String?
    /// Incremented after every change that can alter a grocery list or the pantry.
    private(set) var revision = 0

    /// Receives a batch's pantry item and household after "Made a Batch", so the pantry can show it.
    @ObservationIgnored var onBatchRecorded: ((PantryItem, String) -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: SpecialtiesAPI?
    /// The user the items were loaded for, so another sign-in never sees them.
    @ObservationIgnored private var userID: String?
    /// Incremented whenever the list restarts, so a slow response for an old household can't
    /// overwrite newer state.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "specialties")

    init(session: AuthSession, api: SpecialtiesAPI?) {
        self.session = session
        self.api = api
    }

    /// A store frozen with the given items, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, phase: Phase = .loaded, items: [SpecialtyIngredient] = [],
        householdID: String = "household-1"
    ) -> SpecialtyStore {
        let store = SpecialtyStore(session: session, api: nil)
        store.householdID = householdID
        store.userID = session.currentUser?.id
        store.phase = phase
        store.items = items
        return store
    }

    /// Specialty ingredients without a choice.
    var needsChoiceCount: Int {
        items.count { $0.choice == nil }
    }

    func ingredient(withID id: String) -> SpecialtyIngredient? {
        items.first { $0.id == id }
    }

    // MARK: - Loading

    /// Shows `householdID`'s specialty ingredients. Loads unless they're already loaded or
    /// loading for the signed-in user.
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

    /// Pull to refresh. Items stay on screen until the response arrives.
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
            Self.logger.notice("Specialty list load failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    /// Loads one specialty ingredient and shows it in the list, adding it when the list doesn't
    /// have it (for example, one no recipe uses yet).
    @discardableResult
    func reload(specialtyID: String) async throws -> SpecialtyIngredient {
        let (api, target) = try require(nil)
        let started = generation
        let loaded = try await session.authorized { token in
            try await api.ingredient(householdID: target, specialtyID: specialtyID, accessToken: token)
        }
        if started == generation { apply([loaded]) }
        return loaded
    }

    // MARK: - Choices

    /// Chooses every suggested option for specialty ingredients without a choice.
    func applyDefaults() async throws -> SpecialtyDefaultsOutcome {
        let (api, target) = try require(nil)
        let started = generation
        let response = try await perform(target) { token in
            try await api.applyDefaults(householdID: target, accessToken: token)
        }
        if started == generation { apply(response.items) }
        changed()
        Self.logger.info("Specialty defaults applied to \(response.items.count, privacy: .public)")
        return SpecialtyDefaultsOutcome(chosenNames: response.items.map(\.name), skipped: response.skipped)
    }

    /// Chooses an option in `householdID` (the shown household's when `nil`). A grocery list can
    /// choose before this list was ever opened; the result is shown only for the shown household.
    @discardableResult
    func choose(
        optionID: String, forSpecialtyWithID specialtyID: String, householdID: String? = nil
    ) async throws -> SpecialtyIngredient {
        let (api, target) = try require(householdID)
        let started = generation
        let updated = try await perform(target) { token in
            try await api.setChoice(
                householdID: target, specialtyID: specialtyID, optionID: optionID, accessToken: token)
        }
        if started == generation, target == self.householdID { apply([updated]) }
        changed()
        Self.logger.info("Specialty choice set")
        return updated
    }

    /// Keeps the ingredient on grocery lists by its own name and stops asking.
    @discardableResult
    func keepAsIs(specialtyID: String) async throws -> SpecialtyIngredient {
        try await choose(optionID: SpecialtyChoice.asIsOptionID, forSpecialtyWithID: specialtyID)
    }

    /// Clears the choice, so grocery lists suggest options again.
    func clearChoice(specialtyID: String) async throws {
        let (api, target) = try require(nil)
        try await perform(target) { token in
            try await api.clearChoice(householdID: target, specialtyID: specialtyID, accessToken: token)
        }
        changed()
        try await reload(specialtyID: specialtyID)
    }

    // MARK: - Household options

    /// Adds the household's own option (usually a copy with `basedOnOptionID`) and chooses it.
    /// When choosing fails, the new option is still shown and `SpecialtyOptionNotChosenError`
    /// names it, so trying again chooses it instead of adding another copy.
    @discardableResult
    func addOption(
        _ option: SpecialtyOptionRequest, toSpecialtyWithID specialtyID: String
    ) async throws -> SpecialtyIngredient {
        let (api, target) = try require(nil)
        let created = try await perform(target) { token in
            try await api.createOption(
                householdID: target, specialtyID: specialtyID, option: option, accessToken: token)
        }
        Self.logger.info("Specialty option added")
        do {
            return try await choose(optionID: created.id, forSpecialtyWithID: specialtyID, householdID: target)
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            changed()
            _ = try? await reload(specialtyID: specialtyID)
            throw SpecialtyOptionNotChosenError(
                optionID: created.id, message: HouseholdStore.message(for: error),
                isForbidden: (error as? APIError)?.status == 403)
        }
    }

    func updateOption(
        _ option: SpecialtyOptionRequest, optionID: String, specialtyID: String
    ) async throws {
        let (api, target) = try require(nil)
        try await perform(target) { token in
            try await api.updateOption(
                householdID: target, specialtyID: specialtyID, optionID: optionID, option: option, accessToken: token)
        }
        changed()
        try await reload(specialtyID: specialtyID)
    }

    /// Deletes a household option. The API clears the choice when it was chosen.
    func deleteOption(optionID: String, specialtyID: String) async throws {
        let (api, target) = try require(nil)
        try await perform(target) { token in
            try await api.deleteOption(
                householdID: target, specialtyID: specialtyID, optionID: optionID, accessToken: token)
        }
        changed()
        try await reload(specialtyID: specialtyID)
    }

    // MARK: - Batches

    /// Records "Made a Batch" in `householdID` (the shown household's when `nil`). Send the same
    /// `clientPurchaseID` when trying again, so a lost response records one batch. The pantry item
    /// goes to `onBatchRecorded`, and the specialty ingredient reloads to show the new stock.
    @discardableResult
    func recordBatch(
        specialtyID: String, optionID: String? = nil, batches: Int = 1, clientPurchaseID: String,
        householdID: String? = nil
    ) async throws -> RecordSpecialtyBatchResponse {
        let (api, target) = try require(householdID)
        let count = min(max(batches, 1), SpecialtiesAPI.maxBatches)
        let request = RecordSpecialtyBatchRequest(
            optionID: optionID, batches: count > 1 ? count : nil, clientPurchaseID: clientPurchaseID)
        let response = try await perform(target) { token in
            try await api.recordBatch(
                householdID: target, specialtyID: specialtyID, request: request, accessToken: token)
        }
        Self.logger.info("House-made batch recorded")
        onBatchRecorded?(response.item, target)
        changed()
        if target == self.householdID, phase == .loaded {
            _ = try? await reload(specialtyID: specialtyID)
        }
        return response
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
        userID = nil
    }

    private func clear() {
        generation += 1
        phase = .idle
        items = []
        refreshError = nil
    }

    // MARK: - Helpers

    private func changed() {
        revision += 1
    }

    /// Replaces items with the same ID in place, and adds new ones at the end.
    private func apply(_ updated: [SpecialtyIngredient]) {
        guard phase == .loaded else { return }
        for ingredient in updated {
            if let index = items.firstIndex(where: { $0.id == ingredient.id }) {
                items[index] = ingredient
            } else {
                items.append(ingredient)
            }
        }
    }

    @discardableResult
    private func perform<Result: Sendable>(
        _ target: String, _ operation: (String) async throws -> Result
    ) async throws -> Result {
        do {
            return try await session.authorized { token in try await operation(token) }
        } catch let error as APIError where [403, 404, 409].contains(error.status) {
            Self.logger.notice("Specialty change rejected: \(Self.describe(error), privacy: .public)")
            if target == householdID, phase != .idle {
                await load(showingProgress: false)
            }
            throw error
        }
    }

    private func require(_ householdID: String?) throws -> (SpecialtiesAPI, String) {
        guard let api else { throw AuthSessionError.notConfigured }
        guard let target = householdID ?? self.householdID else { throw AuthSessionError.signedOut }
        return (api, target)
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

/// An option was added, but choosing it failed. The option exists; choose `optionID` to finish.
nonisolated struct SpecialtyOptionNotChosenError: Error, Equatable, LocalizedError {
    let optionID: String
    let message: String
    let isForbidden: Bool

    var errorDescription: String? {
        String(localized: "The option was saved, but it couldn't be chosen. \(message)")
    }
}

/// Chooses specialty ingredient options and records house-made batches. `SpecialtyStore` is the
/// app's; grocery lists depend on this instead of the whole store.
protocol SpecialtyChoosing: AnyObject {
    /// Incremented after every change, so an open grocery list knows to reload.
    var revision: Int { get }

    @discardableResult
    func choose(
        optionID: String, forSpecialtyWithID specialtyID: String, householdID: String?
    ) async throws -> SpecialtyIngredient

    @discardableResult
    func recordBatch(
        specialtyID: String, optionID: String?, batches: Int, clientPurchaseID: String, householdID: String?
    ) async throws -> RecordSpecialtyBatchResponse
}

extension SpecialtyStore: SpecialtyChoosing {}
