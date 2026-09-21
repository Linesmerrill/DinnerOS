import Foundation
import Observation
import os

/// The current household's pantry.
///
/// Main-actor state that lives as long as the app, like `RecipeLibrary`. The API caps a
/// pantry at 1000 items and doesn't paginate, so the whole pantry loads at once and search,
/// status filters, and grouping run on device. Changes apply the items the API returns. A
/// rejected change (403, 404, 409) reloads instead, because it usually means the list is
/// stale.
@Observable
final class PantryStore {
    enum Phase: Equatable {
        case idle
        /// No items are shown while the pantry loads.
        case loading
        case loaded
        /// The load failed, so there's nothing to show.
        case failed(String)
    }

    private(set) var householdID: String?
    private(set) var phase: Phase = .idle
    /// In the API's order: aisle, then name.
    private(set) var items: [PantryItem] = []
    /// Set when a reload failed while items stayed on screen.
    private(set) var refreshError: String?
    /// The household's low-stock setting; `nil` until `loadSettings()` succeeds.
    private(set) var settings: PantrySettings?

    /// Called after the pantry loads or changes, because both can create notifications (a
    /// pantry read runs the API's low-stock check). The app refreshes the unread count here.
    @ObservationIgnored var onChange: (() -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: PantryAPI?
    @ObservationIgnored private let ingredientsAPI: IngredientsAPI?
    /// The user the items were loaded for, so another sign-in never sees them.
    @ObservationIgnored private var userID: String?
    /// Incremented whenever the list restarts, so a slow response for an old household
    /// can't overwrite newer state.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "pantry")

    init(session: AuthSession, api: PantryAPI?, ingredientsAPI: IngredientsAPI?) {
        self.session = session
        self.api = api
        self.ingredientsAPI = ingredientsAPI
    }

    /// A store frozen with the given items, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, phase: Phase = .loaded, items: [PantryItem] = [], householdID: String = "household-1"
    ) -> PantryStore {
        let store = PantryStore(session: session, api: nil, ingredientsAPI: nil)
        store.householdID = householdID
        store.userID = session.currentUser?.id
        store.phase = phase
        store.items = PantryList.sorted(items)
        return store
    }

    // MARK: - Loading

    /// Shows `householdID`'s pantry. Loads unless that pantry is already loaded or loading
    /// for the signed-in user.
    func activate(householdID: String) async {
        let currentUserID = session.currentUser?.id
        let isSamePantry = householdID == self.householdID && currentUserID == userID
        if isSamePantry, phase != .idle {
            return
        }
        if !isSamePantry {
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
                try await api.listItems(householdID: householdID, accessToken: token)
            }
            guard started == generation else { return }
            items = PantryList.sorted(loaded)
            refreshError = nil
            phase = .loaded
            onChange?()
        } catch is CancellationError {
            // A cancelled view task; the next activation loads again.
            if started == generation, phase == .loading { phase = .idle }
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Pantry load failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    // MARK: - Changes

    /// Adds an item, or merges into the item that already has the ingredient.
    @discardableResult
    func add(_ newItem: NewPantryItem) async throws -> PantryItem {
        let (api, householdID) = try requirePantry()
        let started = generation
        let item = try await perform { token in
            try await api.addItem(householdID: householdID, item: newItem, accessToken: token)
        }
        if started == generation { apply([item]) }
        return item
    }

    @discardableResult
    func update(_ item: PantryItem, changes: PantryItemChanges) async throws -> PantryItem {
        guard !changes.isEmpty else { return item }
        let (api, householdID) = try requirePantry()
        let started = generation
        let updated = try await perform { token in
            try await api.updateItem(householdID: householdID, itemID: item.id, changes: changes, accessToken: token)
        }
        if started == generation { apply([updated]) }
        return updated
    }

    /// Marking an item out clears its amount on the server.
    func setStatus(of item: PantryItem, to status: PantryStatus) async throws {
        guard item.status != status else { return }
        try await update(item, changes: PantryItemChanges(status: status))
    }

    func delete(_ item: PantryItem) async throws {
        let (api, householdID) = try requirePantry()
        let started = generation
        try await perform { token in
            try await api.deleteItem(householdID: householdID, itemID: item.id, accessToken: token)
        }
        if started == generation {
            items.removeAll { $0.id == item.id }
        }
    }

    /// Sets many statuses through the bulk endpoint, in batches the API accepts. Items the
    /// API reports missing are dropped from the list and counted in the outcome.
    func setStatus(ofItemsWithIDs ids: [String], to status: PantryStatus) async throws -> PantryBulkOutcome {
        let (api, householdID) = try requirePantry()
        var seen = Set<String>()
        let unique = ids.filter { seen.insert($0).inserted }
        let names = Dictionary(items.map { ($0.id, $0.displayName) }, uniquingKeysWith: { first, _ in first })
        let started = generation
        var updatedCount = 0
        var missing: [String] = []

        for start in stride(from: 0, to: unique.count, by: PantryAPI.maxBulkUpdates) {
            let updates = unique[start..<min(start + PantryAPI.maxBulkUpdates, unique.count)].map {
                PantryStatusUpdate(id: $0, status: status)
            }
            let response = try await perform { token in
                try await api.setStatuses(householdID: householdID, updates: updates, accessToken: token)
            }
            updatedCount += response.items.count
            missing += response.missing
            if started == generation {
                apply(response.items)
                let gone = Set(response.missing)
                items.removeAll { gone.contains($0.id) }
            }
        }
        if !missing.isEmpty {
            Self.logger.info("Bulk status change skipped \(missing.count, privacy: .public) missing items")
        }
        return PantryBulkOutcome(
            status: status, updatedCount: updatedCount, missingCount: missing.count,
            missingNames: missing.compactMap { names[$0] })
    }

    /// Adds the default staples. When some were skipped, another member may have added
    /// them since the last load, so the pantry reloads.
    @discardableResult
    func addDefaultStaples() async throws -> PantryDefaultStaplesResponse {
        let (api, householdID) = try requirePantry()
        let started = generation
        let response = try await perform { token in
            try await api.addDefaultStaples(householdID: householdID, accessToken: token)
        }
        guard started == generation else { return response }
        if response.skipped > 0 {
            await load(showingProgress: false)
        } else {
            apply(response.items)
        }
        return response
    }

    /// The item an add of `name` would merge into, for a hint in the add form.
    func existingItem(named name: String, ingredientID: String?) -> PantryItem? {
        PantryList.existingItem(in: items, named: name, ingredientID: ingredientID)
    }

    // MARK: - Usage

    /// Records a purchase in `householdID`'s pantry (the shown pantry's when `nil`). A grocery
    /// list can record one before the Pantry tab was ever opened; the returned item is applied
    /// only when it belongs to the shown pantry.
    @discardableResult
    func recordPurchase(_ purchase: NewPantryPurchase, householdID: String? = nil) async throws
        -> PantryPurchaseResponse
    {
        guard let api else { throw AuthSessionError.notConfigured }
        guard let target = householdID ?? self.householdID else { throw AuthSessionError.signedOut }
        let started = generation
        let response: PantryPurchaseResponse
        if target == self.householdID {
            response = try await perform { token in
                try await api.recordPurchase(householdID: target, purchase: purchase, accessToken: token)
            }
            if started == generation { apply([response.item]) }
        } else {
            response = try await session.authorized { token in
                try await api.recordPurchase(householdID: target, purchase: purchase, accessToken: token)
            }
            onChange?()
        }
        Self.logger.info("Pantry purchase recorded (\(purchase.source.rawValue, privacy: .public))")
        return response
    }

    /// Records a bulk pack's remainder in the freezer, in `householdID`'s pantry (the shown
    /// pantry's when `nil`). The Shop tab calls it before the Pantry tab has ever been
    /// opened, so the returned item is applied only when it belongs to the shown pantry.
    ///
    /// It is idempotent per handoff line: sealing the same line again changes nothing.
    @discardableResult
    func freeze(request: FreezePantryItemRequest, householdID: String? = nil) async throws
        -> FreezePantryItemResponse
    {
        guard let api else { throw AuthSessionError.notConfigured }
        guard let target = householdID ?? self.householdID else { throw AuthSessionError.signedOut }
        let started = generation
        let response: FreezePantryItemResponse
        if target == self.householdID {
            response = try await perform { token in
                try await api.freeze(householdID: target, request: request, accessToken: token)
            }
            if started == generation { apply([response.item]) }
        } else {
            response = try await session.authorized { token in
                try await api.freeze(householdID: target, request: request, accessToken: token)
            }
            onChange?()
        }
        Self.logger.info("Pantry item frozen (alreadyFrozen: \(response.alreadyFrozen, privacy: .public))")
        return response
    }

    /// Shows an item another feature changed (a house-made batch recorded from specialty
    /// ingredients), when it belongs to the shown pantry.
    func applyChangedItem(_ item: PantryItem, householdID: String) {
        if householdID == self.householdID {
            apply([item])
        }
        onChange?()
    }

    /// The item's most recent purchases, newest first. Not cached.
    func purchases(ofItemWithID itemID: String) async throws -> [PantryPurchase] {
        let (api, householdID) = try requirePantry()
        return try await session.authorized { token in
            try await api.purchases(householdID: householdID, itemID: itemID, accessToken: token)
        }
    }

    @discardableResult
    func loadSettings() async throws -> PantrySettings {
        let (api, householdID) = try requirePantry()
        let started = generation
        let loaded = try await session.authorized { token in
            try await api.settings(householdID: householdID, accessToken: token)
        }
        if started == generation { settings = loaded }
        return loaded
    }

    /// Sets the household's threshold, then reloads so every estimate uses it.
    func updateSettings(lowThresholdPercent: Int) async throws {
        let (api, householdID) = try requirePantry()
        let started = generation
        let updated = try await perform { token in
            try await api.updateSettings(
                householdID: householdID, lowThresholdPercent: lowThresholdPercent, accessToken: token)
        }
        guard started == generation else { return }
        settings = updated
        await load(showingProgress: false)
    }

    // MARK: - Ingredient catalog

    func searchCatalog(_ query: String, limit: Int = IngredientSuggestions.resultLimit) async throws
        -> [CatalogIngredient]
    {
        guard let ingredientsAPI else { throw AuthSessionError.notConfigured }
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return [] }
        return try await session.authorized { token in
            try await ingredientsAPI.search(query: trimmed, limit: limit, accessToken: token)
        }
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
        settings = nil
    }

    // MARK: - Helpers

    /// Replaces items with the same ID or key (an add merges by key), keeping API order.
    private func apply(_ updated: [PantryItem]) {
        guard !updated.isEmpty, phase == .loaded else { return }
        let ids = Set(updated.map(\.id))
        let keys = Set(updated.map(\.key))
        items = PantryList.sorted(items.filter { !ids.contains($0.id) && !keys.contains($0.key) } + updated)
    }

    private func perform<Result: Sendable>(_ operation: (String) async throws -> Result) async throws -> Result {
        do {
            let result = try await session.authorized { token in try await operation(token) }
            onChange?()
            return result
        } catch let error as APIError where [403, 404, 409].contains(error.status) {
            Self.logger.notice("Pantry change rejected: \(Self.describe(error), privacy: .public)")
            await load(showingProgress: false)
            throw error
        }
    }

    private func requirePantry() throws -> (PantryAPI, String) {
        guard let api else { throw AuthSessionError.notConfigured }
        guard let householdID else { throw AuthSessionError.signedOut }
        return (api, householdID)
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

/// Records purchases in a household's pantry. `PantryStore` is the app's; grocery lists
/// depend on this instead of the whole store.
protocol PantryPurchaseRecording: AnyObject {
    @discardableResult
    func recordPurchase(_ purchase: NewPantryPurchase, householdID: String?) async throws -> PantryPurchaseResponse
}

extension PantryStore: PantryPurchaseRecording {}
