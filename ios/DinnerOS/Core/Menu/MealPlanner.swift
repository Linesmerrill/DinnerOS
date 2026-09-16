import Foundation
import Observation
import os

/// One-tap planning from cards and the recipe screen: add with the usual serving size, step
/// servings, move, and remove, each with an undo toast.
///
/// A thin layer over `PlanStore`, which applies every change and tells `MenuStore`, so cards,
/// the recipe screen, and By Day stay in sync.
@Observable
final class MealPlanner {
    /// A short confirmation with an optional undo, shown above the bottom bar.
    struct Toast: Identifiable, Equatable {
        enum Undo: Equatable {
            /// Undoes an add.
            case remove(entryID: String)
            /// Undoes a removal by adding the meal back.
            case restore(NewPlanEntry, customizations: [PlanEntryCustomization])
        }

        let id = UUID()
        let message: String
        let week: ISOWeek
        let undo: Undo?
    }

    private(set) var toast: Toast?
    /// Recipes being added.
    private(set) var busyRecipeIDs: Set<String> = []
    /// Entries being changed or removed.
    private(set) var busyEntryIDs: Set<String> = []
    /// A failed change, for an alert.
    var errorMessage: String?

    @ObservationIgnored private let plans: PlanStore
    @ObservationIgnored private let library: RecipeLibrary
    @ObservationIgnored private let households: HouseholdStore

    private static let logger = Logger(subsystem: "DinnerOS", category: "planning")

    init(plans: PlanStore, library: RecipeLibrary, households: HouseholdStore) {
        self.plans = plans
        self.library = library
        self.households = households
    }

    /// Whether the role may change plans. Hiding controls is a convenience; the API enforces `plan.edit`.
    var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    /// The shown week's entries for a recipe, in the order they were added.
    func entries(recipeID: String) -> [PlanEntry] {
        plans.plan?.entries.filter { $0.recipe.id == recipeID } ?? []
    }

    /// The recipe's serving sizes, from the library's cache when it was loaded before.
    func servingOptions(recipeID: String) async throws -> [Int] {
        try await library.recipe(id: recipeID).servingOptions
    }

    // MARK: - Changes

    /// Adds a recipe to `week` with `servings`, or the household's usual size when `nil`, then
    /// applies `selections` to the new entry. Returns the entry, or `nil` after a failure.
    @discardableResult
    func add(
        recipeID: String, name: String, to week: ISOWeek, day: PlanDay? = nil, servings: Int? = nil,
        selections: [PlanCustomizationRequest.Selection] = [], showsToast: Bool = true
    ) async -> PlanEntry? {
        guard !busyRecipeIDs.contains(recipeID) else { return nil }
        busyRecipeIDs.insert(recipeID)
        defer { busyRecipeIDs.remove(recipeID) }
        do {
            let size: Int
            if let servings {
                size = servings
            } else {
                let recipe = try await library.recipe(id: recipeID)
                guard
                    let preferred = recipe.preferredServings(
                        householdDefault: households.current?.household.defaultServings)
                else {
                    errorMessage = String(localized: "\(name) has no serving sizes, so it can't be planned.")
                    return nil
                }
                size = preferred
            }
            let entry = try await plans.addEntry(NewPlanEntry(recipeID: recipeID, day: day, servings: size), to: week)
            if !selections.isEmpty, week == plans.week {
                do {
                    try await plans.setCustomization(entryID: entry.id, selections: selections)
                } catch is CancellationError {
                } catch {
                    errorMessage = String(
                        localized:
                            "\(name) was added, but its changes weren't saved. \(HouseholdStore.message(for: error))")
                }
            }
            if showsToast {
                toast = Toast(
                    message: String(localized: "Added \(name)"), week: week, undo: .remove(entryID: entry.id))
            }
            return entry
        } catch is CancellationError {
            return nil
        } catch {
            report(error)
            return nil
        }
    }

    /// Removes an entry from the shown week, with undo.
    func remove(_ entry: PlanEntry) async {
        guard !busyEntryIDs.contains(entry.id) else { return }
        busyEntryIDs.insert(entry.id)
        defer { busyEntryIDs.remove(entry.id) }
        let week = plans.week
        do {
            try await plans.deleteEntry(id: entry.id)
            let restore = NewPlanEntry(
                recipeID: entry.recipe.id, day: entry.day, servings: entry.servings, note: entry.note)
            toast = Toast(
                message: String(localized: "Removed \(entry.recipe.name)"), week: week,
                undo: .restore(restore, customizations: entry.customizations))
        } catch is CancellationError {
            return
        } catch {
            report(error)
        }
    }

    /// Moves servings to the next supported size up or down. Going below the smallest size
    /// removes the meal, with undo.
    func changeServings(_ entry: PlanEntry, by delta: Int) async {
        guard delta != 0, !busyEntryIDs.contains(entry.id) else { return }
        let options: [Int]
        do {
            options = try await servingOptions(recipeID: entry.recipe.id)
        } catch is CancellationError {
            return
        } catch {
            report(error)
            return
        }
        guard let next = ServingSizes.step(from: entry.servings, options: options, by: delta) else {
            if delta < 0 {
                await remove(entry)
            }
            return
        }
        busyEntryIDs.insert(entry.id)
        defer { busyEntryIDs.remove(entry.id) }
        do {
            try await plans.updateEntry(id: entry.id, changes: PlanEntryChanges(servings: next))
        } catch is CancellationError {
            return
        } catch {
            report(error)
        }
    }

    /// Moves an entry to `day`, or unschedules it when `day` is `nil`.
    func move(_ entry: PlanEntry, to day: PlanDay?) async {
        guard !busyEntryIDs.contains(entry.id) else { return }
        busyEntryIDs.insert(entry.id)
        defer { busyEntryIDs.remove(entry.id) }
        do {
            try await plans.moveEntry(id: entry.id, to: day)
        } catch is CancellationError {
            return
        } catch {
            report(error)
        }
    }

    // MARK: - Toast

    /// Reverses the change the toast describes.
    func undo() async {
        guard let toast, let undo = toast.undo else { return }
        self.toast = nil
        do {
            switch undo {
            case .remove(let entryID):
                guard toast.week == plans.week else { return }
                try await plans.deleteEntry(id: entryID)
            case .restore(let entry, let customizations):
                let restored = try await plans.addEntry(entry, to: toast.week)
                if !customizations.isEmpty, toast.week == plans.week {
                    try await plans.setCustomization(
                        entryID: restored.id,
                        selections: customizations.map {
                            PlanCustomizationRequest.Selection(ingredientKey: $0.ingredientKey, choiceID: $0.choiceID)
                        })
                }
            }
        } catch is CancellationError {
            return
        } catch {
            report(error)
        }
    }

    /// Reports a failed change. A `403` means the role changed elsewhere, so the household
    /// reloads and the controls this class hides match it again, rather than every tap failing
    /// until the next launch.
    private func report(_ error: any Error) {
        errorMessage = HouseholdStore.message(for: error)
        if (error as? APIError)?.status == 403 {
            Task { await households.load() }
        }
    }

    /// Hides the toast when it's still `id`, so a newer one isn't dismissed early.
    func dismissToast(_ id: UUID) {
        if toast?.id == id {
            toast = nil
        }
    }

    /// Forgets everything, for sign-out and household switches.
    func reset() {
        toast = nil
        busyRecipeIDs = []
        busyEntryIDs = []
        errorMessage = nil
    }
}
