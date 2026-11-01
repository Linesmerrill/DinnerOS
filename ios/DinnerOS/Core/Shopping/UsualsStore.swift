import Foundation
import Observation
import os

/// Add Item and "Your usuals" (decision 651): adding items to a week's list by hand, the
/// household's usuals for a week, pins and "not a usual", and "Add your usuals?".
///
/// The usuals themselves are worked out on the server from the household's own additions and
/// cart sends, keyed to the DinnerOS household, never a store account. An add made offline is
/// kept on this device and sent when the API answers again; adds are safe to repeat.
@Observable
final class UsualsStore {
    /// The week `usuals` belongs to, and its household.
    private(set) var householdID: String?
    private(set) var week: ISOWeek?
    private(set) var usuals: WeekUsuals?
    /// Adds made offline, oldest first, for every household this device used.
    private(set) var pending: [PendingGroceryAdd] = []
    /// Bumped after anything changes a week's list, so the grocery list and the Shop tab
    /// read it again.
    private(set) var revision = 0
    /// Weeks this device already asked about, so a prompt isn't shown twice while the Not Now
    /// is still on its way.
    private(set) var askedWeeks: Set<String> = []
    /// "Add your usuals?" being asked, by the screen that asked.
    private(set) var prompt: PromptRequest?

    /// A request to ask "Add your usuals?" for a week.
    struct PromptRequest: Identifiable, Equatable {
        enum Origin: Equatable { case shop, groceryList }
        let week: ISOWeek
        let householdID: String
        let origin: Origin
        var id: String { "\(householdID)/\(week.description)" }
    }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: ShoppingAPI?
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var isFlushing = false

    private static let logger = Logger(subsystem: "DinnerOS", category: "usuals")
    static let pendingKey = "usuals.pendingAdds"

    init(session: AuthSession, api: ShoppingAPI?, defaults: UserDefaults = .standard) {
        self.session = session
        self.api = api
        self.defaults = defaults
        if let data = defaults.data(forKey: Self.pendingKey),
            let saved = try? JSONDecoder().decode([PendingGroceryAdd].self, from: data)
        {
            pending = saved
        }
    }

    /// A store with fixed usuals and no network, for previews.
    static func preview(session: AuthSession, usuals: WeekUsuals? = nil) -> UsualsStore {
        let store = UsualsStore(
            session: session, api: nil, defaults: UserDefaults(suiteName: "usuals-preview") ?? .standard)
        store.usuals = usuals
        store.week = usuals.flatMap { ISOWeek($0.week) }
        store.householdID = "household-preview"
        return store
    }

    // MARK: - Reading

    /// Usuals for `week`, when they're the ones loaded.
    func usuals(for week: ISOWeek, householdID: String) -> WeekUsuals? {
        guard self.week == week, self.householdID == householdID else { return nil }
        return usuals
    }

    /// Whether to ask "Add your usuals?" for `week` now.
    func shouldPrompt(for week: ISOWeek, householdID: String) -> Bool {
        guard let usuals = usuals(for: week, householdID: householdID) else { return false }
        return usuals.shouldPrompt && !askedWeeks.contains(week.description)
    }

    /// Items added offline for `week` that haven't reached the API yet.
    func pendingItems(for week: ISOWeek, householdID: String) -> [NewGroceryItem] {
        pending.filter { $0.householdID == householdID && $0.week == week.description }.flatMap(\.request.items)
    }

    /// Asks "Add your usuals?" for `week` from `origin`, when it should be asked. Returns
    /// whether it was.
    @discardableResult
    func ask(week: ISOWeek, householdID: String, from origin: PromptRequest.Origin) -> Bool {
        guard prompt == nil, shouldPrompt(for: week, householdID: householdID) else { return false }
        prompt = PromptRequest(week: week, householdID: householdID, origin: origin)
        return true
    }

    /// The prompt closed, answered or not; this device doesn't ask about the week again.
    func promptClosed() {
        if let prompt { askedWeeks.insert(prompt.week.description) }
        prompt = nil
    }

    // MARK: - Loading

    /// Loads `week`'s usuals. Sends anything added offline first, so the list it's marked
    /// against has it.
    func load(week: ISOWeek, householdID: String) async {
        if householdID != self.householdID || week != self.week {
            generation += 1
            usuals = nil
        }
        self.householdID = householdID
        self.week = week
        await flush()
        guard let api else { return }
        let started = generation
        do {
            let loaded = try await session.authorized { token in
                try await api.usuals(householdID: householdID, week: week.description, accessToken: token)
            }
            guard started == generation else { return }
            usuals = loaded
        } catch is CancellationError {
            return
        } catch {
            // Usuals are a convenience: without them the list works as before.
            Self.logger.notice("Usuals didn't load: \(String(describing: type(of: error)), privacy: .public)")
        }
    }

    // MARK: - Changing

    enum AddOutcome: Equatable {
        case added([AddedGroceryItem])
        /// The API couldn't be reached; the items go when it can.
        case queued
    }

    /// Adds items to `week`'s list. Offline, they're kept and sent later.
    @discardableResult
    func add(
        _ items: [NewGroceryItem], week: ISOWeek, householdID: String, fromUsuals: Bool = false
    ) async throws -> AddOutcome {
        let cleaned = items.map {
            NewGroceryItem(name: $0.cleanName, count: min(max($0.count, 1), NewGroceryItem.maxCount))
        }
        .filter { !$0.name.isEmpty }
        guard !cleaned.isEmpty else { return .added([]) }
        let request = AddGroceryItemsRequest(items: cleaned, fromUsuals: fromUsuals)
        if fromUsuals { askedWeeks.insert(week.description) }
        guard let api else { throw AuthSessionError.notConfigured }
        do {
            let added = try await session.authorized { token in
                try await api.addItems(
                    householdID: householdID, week: week.description, request: request, accessToken: token)
            }
            Self.logger.info("Added \(added.count, privacy: .public) items")
            revision += 1
            await refreshIfShown(week: week, householdID: householdID)
            return .added(added)
        } catch let error as APIError where Self.isUnreachable(error) {
            enqueue(PendingGroceryAdd(id: UUID(), householdID: householdID, week: week.description, request: request))
            Self.logger.notice("Add queued until the API can be reached")
            revision += 1
            return .queued
        }
    }

    /// Takes an item added offline back out before it was sent.
    func removePending(_ item: NewGroceryItem, week: ISOWeek, householdID: String) {
        var changed = false
        pending = pending.compactMap { add in
            guard add.householdID == householdID, add.week == week.description else { return add }
            let items = add.request.items.filter { $0 != item }
            guard items.count != add.request.items.count else { return add }
            changed = true
            guard !items.isEmpty else { return nil }
            return PendingGroceryAdd(
                id: add.id, householdID: add.householdID, week: add.week,
                request: AddGroceryItemsRequest(items: items, fromUsuals: add.request.fromUsuals))
        }
        if changed {
            save()
            revision += 1
        }
    }

    /// Takes an added item off `week`'s list. One already gone counts as removed.
    func remove(itemID: String, week: ISOWeek, householdID: String) async throws {
        guard let api else { throw AuthSessionError.notConfigured }
        do {
            try await session.authorized { token in
                try await api.removeAddedItem(
                    householdID: householdID, week: week.description, itemID: itemID, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            Self.logger.notice("Added item was already removed")
        }
        revision += 1
        await refreshIfShown(week: week, householdID: householdID)
    }

    /// Takes the week's added item for `ingredientKey` off, from the Shop tab. One already gone
    /// counts as removed.
    func remove(ingredientKey: String, week: ISOWeek, householdID: String) async throws {
        guard let api else { throw AuthSessionError.notConfigured }
        do {
            try await session.authorized { token in
                try await api.removeAddedItem(
                    householdID: householdID, week: week.description, ingredientKey: ingredientKey, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            Self.logger.notice("Added item was already removed")
        }
        revision += 1
        await refreshIfShown(week: week, householdID: householdID)
    }

    /// Not Now: the week stops asking, here at once and for the household on the server.
    func dismiss(week: ISOWeek, householdID: String) async {
        askedWeeks.insert(week.description)
        guard let api else { return }
        do {
            try await session.authorized { token in
                try await api.dismissUsuals(householdID: householdID, week: week.description, accessToken: token)
            }
        } catch {
            // This device won't ask again this session either way.
            Self.logger.notice("Not Now didn't reach the API")
        }
    }

    /// Pins, marks "not a usual", or puts an item back, then reloads the shown week.
    func setUsual(_ request: SetUsualRequest, householdID: String) async throws {
        guard let api else { throw AuthSessionError.notConfigured }
        _ = try await session.authorized { token in
            try await api.setUsual(householdID: householdID, request: request, accessToken: token)
        }
        if let week, self.householdID == householdID {
            await load(week: week, householdID: householdID)
        }
    }

    /// Sends adds made offline, oldest first. Stops at the first one the API still can't take
    /// for lack of a connection; one it refuses outright is dropped.
    func flush() async {
        guard let api, !isFlushing, !pending.isEmpty else { return }
        isFlushing = true
        defer { isFlushing = false }
        var sent = false
        while let next = pending.first {
            do {
                _ = try await session.authorized { token in
                    try await api.addItems(
                        householdID: next.householdID, week: next.week, request: next.request, accessToken: token)
                }
                sent = true
            } catch let error as APIError where Self.isUnreachable(error) {
                break
            } catch is CancellationError {
                break
            } catch {
                Self.logger.notice("Dropped a queued add the API refused")
            }
            pending.removeFirst()
            save()
        }
        if sent { revision += 1 }
    }

    /// Forgets everything for a sign-out.
    func reset() {
        generation += 1
        householdID = nil
        week = nil
        usuals = nil
        askedWeeks = []
        prompt = nil
        pending = []
        save()
    }

    // MARK: - Private

    private func refreshIfShown(week: ISOWeek, householdID: String) async {
        guard self.week == week, self.householdID == householdID else { return }
        await load(week: week, householdID: householdID)
    }

    private func enqueue(_ add: PendingGroceryAdd) {
        pending.append(add)
        // A long offline stretch can't grow the queue without bound.
        if pending.count > 50 { pending.removeFirst(pending.count - 50) }
        save()
    }

    private func save() {
        if pending.isEmpty {
            defaults.removeObject(forKey: Self.pendingKey)
        } else if let data = try? JSONEncoder().encode(pending) {
            defaults.set(data, forKey: Self.pendingKey)
        }
    }

    static func isUnreachable(_ error: APIError) -> Bool {
        if case .transport = error { return true }
        return false
    }
}
