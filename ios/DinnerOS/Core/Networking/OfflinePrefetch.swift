import Foundation

/// Loads what the week needs while there's a connection, so the week, each meal's recipe, and
/// its cooking steps (at the planned servings and protein swaps) are saved for use offline.
///
/// Every request goes through the normal stores and client, so the saved copies are exactly
/// what the screens ask for. It runs at most every `interval`, and never blocks a screen.
@MainActor
final class OfflinePrefetch {
    private let plans: PlanStore
    private let library: RecipeLibrary
    private let pantry: PantryStore
    private let thaw: ThawStore
    private let offline: OfflineStatus
    private let interval: TimeInterval
    private var lastRun: Date?
    private var running = false

    init(
        plans: PlanStore, library: RecipeLibrary, pantry: PantryStore, thaw: ThawStore, offline: OfflineStatus,
        interval: TimeInterval = 30 * 60
    ) {
        self.offline = offline
        self.plans = plans
        self.library = library
        self.pantry = pantry
        self.thaw = thaw
        self.interval = interval
    }

    /// This week and next: each plan, each meal's recipe and steps, plus the pantry and thaw list.
    func run(now: Date = .now) async {
        // Offline there's nothing new to save.
        if running || offline.isShowingSaved { return }
        if let lastRun, now.timeIntervalSince(lastRun) < interval { return }
        running = true
        defer { running = false }
        let current = ISOWeek.current(in: plans.planningTimeZone, weekStartsOn: plans.weekStartsOn, now: now)
        var fetched = false
        for week in [current, current.next] {
            guard let plan = try? await plans.fetch(week: week) else { continue }
            fetched = true
            var seen = Set<String>()
            for entry in plan.entries where seen.insert(entry.recipe.id).inserted {
                _ = try? await library.recipe(id: entry.recipe.id, reload: true)
                _ = try? await library.instructions(
                    recipeID: entry.recipe.id, servings: entry.servings, swaps: entry.customizations)
            }
        }
        await pantry.refresh()
        await thaw.load()
        if fetched { lastRun = now }
    }
}
