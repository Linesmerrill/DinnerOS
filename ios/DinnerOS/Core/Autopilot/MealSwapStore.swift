import Foundation
import Observation
import os

/// "Try Something Similar": one planned meal at a time, the alternatives Autopilot offers for
/// it, and the swap that applies one (`docs/autopilot.md#try-something-similar`).
///
/// Main-actor state that lives as long as the app, like `PlanStore`. The sheet is driven by
/// `target`: the Menu tab's carousel and By Day both call `start(_:)` from a meal's long-press
/// menu and present the same sheet. A swap returns the whole plan, handed to `planDidChange`,
/// so the week and its grocery list follow without a reload.
@Observable
final class MealSwapStore {
    /// The meal being re-picked, which also presents the sheet.
    struct Target: Identifiable, Equatable {
        let entryID: String
        let recipeID: String
        let recipeName: String
        let week: ISOWeek

        var id: String { entryID }
    }

    enum Phase: Equatable {
        case loading
        case loaded
        /// Nothing could be offered; the text says why.
        case failed(String)
    }

    private(set) var target: Target?
    private(set) var phase: Phase = .loading
    private(set) var alternatives: [MealAlternative] = []
    /// A note under the list, such as "Nothing in your library is much like this one".
    private(set) var notice: String?
    /// The recipe being applied, so its row can show progress.
    private(set) var applyingRecipeID: String?
    /// A failed swap, for an alert.
    var errorMessage: String?

    /// Receives the plan a swap returns.
    @ObservationIgnored var planDidChange: ((Plan) -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: AutopilotAPI?
    @ObservationIgnored private let households: HouseholdStore
    /// Recipes already offered for this meal, so "Show Me Others" brings different ones.
    @ObservationIgnored private var seen: [String] = []
    /// Incremented whenever the target changes, so a slow response can't fill another sheet.
    @ObservationIgnored private var generation = 0

    /// How many to show at a time, and how many asks to remember.
    static let pageSize = 3
    static let maxSeen = 40

    private static let logger = Logger(subsystem: "DinnerOS", category: "autopilot")

    init(session: AuthSession, api: AutopilotAPI?, households: HouseholdStore) {
        self.session = session
        self.api = api
        self.households = households
    }

    /// A store frozen with `alternatives`, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, households: HouseholdStore, target: Target? = nil,
        alternatives: [MealAlternative] = [], notice: String? = nil
    ) -> MealSwapStore {
        let store = MealSwapStore(session: session, api: nil, households: households)
        store.target = target
        store.alternatives = alternatives
        store.notice = notice
        store.phase = target == nil ? .loading : .loaded
        return store
    }

    /// Whether the meal can be re-picked: the role may plan, and the week is still a draft.
    /// Hiding the action is a convenience; the API refuses a finalized week and a cooked meal.
    func canSwap(_ entry: PlanEntry, isDraft: Bool, isCooked: Bool) -> Bool {
        households.access?.can(.planEdit) == true && isDraft && !isCooked && !entry.recipe.isAddon
            && entry.day != nil
    }

    // MARK: - The sheet

    /// Opens the sheet for `entry` in `week` and loads the first alternatives.
    func start(_ entry: PlanEntry, week: ISOWeek) {
        generation += 1
        seen = []
        alternatives = []
        notice = nil
        errorMessage = nil
        applyingRecipeID = nil
        phase = .loading
        target = Target(
            entryID: entry.id, recipeID: entry.recipe.id, recipeName: entry.recipe.name, week: week)
    }

    /// Closes the sheet.
    func cancel() {
        generation += 1
        target = nil
        alternatives = []
        notice = nil
        applyingRecipeID = nil
    }

    /// Loads the first set of alternatives for the open meal.
    func load() async {
        await fetch(replacing: true)
    }

    /// Asks for different ones, remembering everything shown so far.
    func showOthers() async {
        seen.append(contentsOf: alternatives.map(\.recipe.id))
        if seen.count > Self.maxSeen {
            seen.removeFirst(seen.count - Self.maxSeen)
        }
        await fetch(replacing: true)
    }

    private func fetch(replacing: Bool) async {
        guard let api, let target, let householdID = households.current?.household.id else { return }
        generation += 1
        let started = generation
        if replacing {
            phase = .loading
        }
        do {
            let loaded = try await session.authorized { token in
                try await api.mealAlternatives(
                    householdID: householdID, week: target.week, entryID: target.entryID,
                    limit: Self.pageSize, seen: seen, accessToken: token)
            }
            guard started == generation, self.target?.entryID == target.entryID else { return }
            alternatives = loaded.alternatives
            notice = loaded.noticeText
            phase = .loaded
        } catch is CancellationError {
            return
        } catch {
            guard started == generation, self.target?.entryID == target.entryID else { return }
            alternatives = []
            notice = nil
            phase = .failed(HouseholdStore.message(for: error))
            Self.logger.notice("Meal alternatives failed: \(HouseholdStore.message(for: error), privacy: .public)")
        }
    }

    // MARK: - Applying

    /// Replaces the planned meal with `alternative`, keeping its day and servings. Returns
    /// the name of the meal that was replaced on success, for a confirmation.
    @discardableResult
    func apply(_ alternative: MealAlternative) async -> String? {
        guard let api, let target, let householdID = households.current?.household.id,
            applyingRecipeID == nil
        else { return nil }
        applyingRecipeID = alternative.recipe.id
        defer { applyingRecipeID = nil }
        do {
            let result = try await session.authorized { token in
                try await api.swapPlannedMeal(
                    householdID: householdID, week: target.week, entryID: target.entryID,
                    recipeID: alternative.recipe.id, accessToken: token)
            }
            planDidChange?(result.plan)
            Self.logger.info("Planned meal swapped for a similar one")
            cancel()
            return result.previousRecipe.name
        } catch is CancellationError {
            return nil
        } catch {
            errorMessage = HouseholdStore.message(for: error)
            if (error as? APIError)?.status == 403 {
                Task { await households.load() }
            }
            // A finalized week or a cooked meal can't be swapped at all, so the sheet closes
            // and the alert explains it rather than leaving a list nothing can be picked from.
            if let status = (error as? APIError)?.status, status == 409 || status == 403 {
                cancel()
            }
            return nil
        }
    }

    /// Forgets everything, for sign-out or a household switch.
    func reset() {
        generation += 1
        target = nil
        alternatives = []
        seen = []
        notice = nil
        errorMessage = nil
        applyingRecipeID = nil
        phase = .loading
    }
}
