import Foundation
import Observation
import os

/// The shown week's add-on pairings: what Autopilot offers with each planned meal, and the
/// paired grocery items already on the week's list.
///
/// Main-actor state that lives as long as the app, like `PlanStore`. Every change the API
/// makes to the plan is handed to `planDidChange`, so the Menu and the week follow without a
/// reload; a new rule is handed to `profileDidChange`.
///
/// A pairing always belongs to a **planned meal**. Accepting from the recipe screen when the
/// main recipe isn't in the week plans it first, then accepts against the new entry (#312).
@Observable
final class PairingsStore {
    private(set) var householdID: String?
    /// The week `pairings` belongs to.
    private(set) var week: ISOWeek?
    private(set) var pairings = WeekPairings.empty
    private(set) var isLoading = false
    /// Suggestions being accepted or dismissed, keyed by `<entryId>/<key>`.
    private(set) var busyKeys: Set<String> = []
    /// Grocery items being removed from the week's list.
    private(set) var busyItemIDs: Set<String> = []
    /// A failed change, for an alert.
    var errorMessage: String?
    /// The role may not change plans, so every control is hidden and nothing is loaded.
    private(set) var isForbidden = false

    /// Receives the plan a pairing change returns.
    @ObservationIgnored var planDidChange: ((Plan) -> Void)?
    /// Receives the profile a new rule returns.
    @ObservationIgnored var profileDidChange: ((AutopilotProfile) -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: AutopilotAPI?
    @ObservationIgnored private let plans: PlanStore
    /// Plans the main recipe before a carousel accept. `nil` in previews.
    @ObservationIgnored private let planner: MealPlanner?
    /// Incremented whenever the shown week is replaced, so a slow response can't overwrite it.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "pairings")

    init(session: AuthSession, api: AutopilotAPI?, plans: PlanStore, planner: MealPlanner? = nil) {
        self.session = session
        self.api = api
        self.plans = plans
        self.planner = planner
    }

    /// A store frozen with `pairings`, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, plans: PlanStore, pairings: WeekPairings = .empty, week: ISOWeek? = nil
    ) -> PairingsStore {
        let store = PairingsStore(session: session, api: nil, plans: plans)
        store.householdID = "household-preview"
        store.week = week ?? ISOWeek(pairings.week) ?? plans.week
        store.pairings = pairings
        return store
    }

    // MARK: - Reading

    /// The week's open suggestions, in plan order. Accepted and dismissed ones are already
    /// left out by the API.
    var openSuggestions: [(meal: MealPairings, pairing: Pairing)] {
        pairings.openSuggestions
    }

    /// The suggestions offered with one planned meal.
    func suggestions(forEntry entryID: String) -> [Pairing] {
        pairings.meal(entryID: entryID)?.pairings ?? []
    }

    /// The paired grocery item behind a grocery list extra, so it can be taken off the list.
    func groceryLine(id: String) -> PairingGroceryLine? {
        pairings.groceryItems.first { $0.id == id }
    }

    /// The paired grocery item for `key`, for unchecking one on the recipe screen.
    func groceryLine(key: String) -> PairingGroceryLine? {
        pairings.groceryItems.first { $0.key == key }
    }

    func isBusy(entryID: String, key: String) -> Bool {
        busyKeys.contains(Self.busyKey(entryID: entryID, key: key))
    }

    private static func busyKey(entryID: String, key: String) -> String {
        "\(entryID)/\(key)"
    }

    // MARK: - Loading

    /// Shows `week`'s pairings for `householdID`, loading them. Members without `plan.edit`
    /// still read them: the week endpoint needs only `household.view`.
    func showWeek(_ newWeek: ISOWeek, householdID: String) async {
        if householdID != self.householdID {
            clear()
            self.householdID = householdID
        }
        if newWeek != week {
            generation += 1
            week = newWeek
            pairings = .empty
        }
        await load()
    }

    func reload() async {
        await load()
    }

    private func load() async {
        guard let api, let householdID, let target = week else { return }
        generation += 1
        let started = generation
        isLoading = true
        defer {
            if started == generation { isLoading = false }
        }
        do {
            let loaded = try await session.authorized { token in
                try await api.weekPairings(householdID: householdID, week: target, accessToken: token)
            }
            guard started == generation, householdID == self.householdID else { return }
            pairings = loaded
            isForbidden = false
        } catch let error as APIError where error.status == 403 {
            guard started == generation else { return }
            isForbidden = true
            pairings = .empty
        } catch is CancellationError {
            return
        } catch let error as APIError where error.status == 404 {
            // A server without pairings, or a week with no plan.
            guard started == generation else { return }
            pairings = .empty
        } catch {
            guard started == generation else { return }
            // Suggestions are an extra: a failure leaves the rest of the screen alone.
            Self.logger.notice("Week pairings failed: \(Self.describe(error), privacy: .public)")
        }
    }

    // MARK: - Changes

    /// Adds a pairing to the week. An add-on becomes a plan entry on the meal's day; a
    /// grocery item joins the week's list. Accepting one already there changes nothing and
    /// comes back as `alreadyAdded`.
    @discardableResult
    func accept(entryID: String, key: String) async throws -> PairingAcceptResult? {
        try await change(entryID: entryID, key: key) { api, householdID, target, token in
            try await api.acceptPairing(
                householdID: householdID, week: target, entryID: entryID, key: key, accessToken: token)
        } handle: { [weak self] result in
            self?.planDidChange?(result.plan)
        }
    }

    /// Hides a pairing for that meal this week. The response is the week's pairings.
    func dismiss(entryID: String, key: String) async throws {
        _ = try await change(entryID: entryID, key: key) { api, householdID, target, token in
            try await api.dismissPairing(
                householdID: householdID, week: target, entryID: entryID, key: key, accessToken: token)
        } handle: { [weak self] updated in
            self?.apply(updated)
        }
    }

    /// Keeps a learned pairing as a household rule for its meal category.
    @discardableResult
    func makeRule(entryID: String, key: String, frequency: PairingFrequency? = nil) async throws
        -> MakePairingRuleResult?
    {
        try await change(entryID: entryID, key: key) { api, householdID, target, token in
            try await api.makePairingRule(
                householdID: householdID, week: target,
                request: .entry(entryID, key: key, frequency: frequency), accessToken: token)
        } handle: { [weak self] result in
            self?.profileDidChange?(result.profile)
        }
    }

    /// Keeps a proposal slot's learned pairing as a rule, from the review sheet.
    @discardableResult
    func makeRule(slotID: String, key: String, frequency: PairingFrequency? = nil) async throws
        -> MakePairingRuleResult?
    {
        try await change(entryID: slotID, key: key) { api, householdID, target, token in
            try await api.makePairingRule(
                householdID: householdID, week: target,
                request: .slot(slotID, key: key, frequency: frequency), accessToken: token)
        } handle: { [weak self] result in
            self?.profileDidChange?(result.profile)
        }
    }

    /// Takes a paired grocery item off the week's list, then reloads so the list and the
    /// suggestions agree.
    func removeGroceryItem(id: String) async throws {
        guard let api, let householdID, let target = week else { throw AuthSessionError.notConfigured }
        guard !busyItemIDs.contains(id) else { return }
        busyItemIDs.insert(id)
        defer { busyItemIDs.remove(id) }
        do {
            try await session.authorized { token in
                try await api.deletePairingGroceryItem(
                    householdID: householdID, week: target, itemID: id, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            // Someone else already took it off; the reload below agrees.
        }
        Self.logger.info("Paired grocery item removed")
        await load()
    }

    /// Accepts a pairing from the recipe screen.
    ///
    /// `entryID` is the main recipe's plan entry in this week, from the carousel's response.
    /// When it is `nil` the recipe isn't planned, so it is **planned first** and the pairing
    /// accepted against the new entry. Returns `nil` when the main recipe couldn't be planned.
    @discardableResult
    func acceptForRecipe(
        key: String, mainRecipeID: String, mainRecipeName: String, mainEntryID: String?, servings: Int? = nil
    ) async throws -> PairingAcceptResult? {
        var entryID = mainEntryID
        if entryID == nil {
            guard let planner, let target = week else { return nil }
            // Planning the meal is the member's change; its own toast would duplicate the
            // carousel's checkmark, so it's added quietly.
            guard
                let entry = await planner.add(
                    recipeID: mainRecipeID, name: mainRecipeName, to: target, servings: servings,
                    showsToast: false)
            else { return nil }
            entryID = entry.id
        }
        guard let entryID else { return nil }
        return try await accept(entryID: entryID, key: key)
    }

    /// Takes an accepted pairing back off the week: an add-on's plan entry is deleted, a
    /// grocery item is taken off the list.
    func remove(_ pairing: Pairing, mainEntryID: String?) async throws {
        switch pairing.target {
        case .recipe(let recipe):
            guard let entry = addonEntry(recipeID: recipe.id, mainEntryID: mainEntryID) else { return }
            try await plans.deleteEntry(id: entry.id)
            await load()
        case .groceryItem:
            guard let line = groceryLine(key: pairing.key) else { return }
            try await removeGroceryItem(id: line.id)
        }
    }

    /// The add-on's plan entry for `recipeID`, preferring the one on the main meal's day —
    /// two pasta nights can both have garlic bread, and only this meal's is removed.
    func addonEntry(recipeID: String, mainEntryID: String?) -> PlanEntry? {
        let entries = plans.plan?.entries.filter { $0.recipe.id == recipeID } ?? []
        guard let mainEntryID, let main = plans.plan?.entries.first(where: { $0.id == mainEntryID }) else {
            return entries.last
        }
        return entries.last { $0.day == main.day } ?? entries.last
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
    }

    private func clear() {
        generation += 1
        week = nil
        pairings = .empty
        isLoading = false
        busyKeys = []
        busyItemIDs = []
        errorMessage = nil
        isForbidden = false
    }

    // MARK: - Helpers

    /// Shows pairings the API returned when they're for the shown week.
    private func apply(_ updated: WeekPairings) {
        guard updated.week == week?.description else { return }
        generation += 1
        pairings = updated
    }

    /// Runs a change for one suggestion, marking it busy and turning a `403` into a hidden
    /// control rather than an error the member can't act on.
    private func change<Result: Sendable>(
        entryID: String, key: String,
        _ operation: (AutopilotAPI, String, ISOWeek, String) async throws -> Result,
        handle: @escaping (Result) -> Void
    ) async throws -> Result? {
        guard let api, let householdID, let target = week else { throw AuthSessionError.notConfigured }
        let busy = Self.busyKey(entryID: entryID, key: key)
        guard !busyKeys.contains(busy) else { return nil }
        busyKeys.insert(busy)
        defer { busyKeys.remove(busy) }
        do {
            let result = try await session.authorized { token in
                try await operation(api, householdID, target, token)
            }
            guard householdID == self.householdID, target == week else { return nil }
            handle(result)
            // Accepting or making a rule changes what's still offered.
            await load()
            return result
        } catch let error as APIError where error.status == 403 {
            isForbidden = true
            throw error
        } catch let error as APIError where [404, 409].contains(error.status) {
            // Someone else planned, finalized, or dismissed it first; the reload agrees.
            Self.logger.notice("Pairing change rejected: \(Self.describe(error), privacy: .public)")
            await load()
            throw error
        }
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
