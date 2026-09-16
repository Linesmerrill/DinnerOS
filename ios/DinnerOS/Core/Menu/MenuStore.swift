import Foundation
import Observation
import os

/// The Menu screen's state for the current household: the selected week's menu, the week
/// strip, All Meals, and filter options.
///
/// Main-actor state that lives as long as the app, like `PlanStore`. The plan itself stays in
/// `PlanStore`, which the Menu screen keeps on the same week; every plan `PlanStore` receives
/// is handed to `applyPlan`, which patches the cards' `inPlan` flags and is what the strip's
/// counts are read from, so adding, moving, and removing never waits for a menu reload.
@Observable
final class MenuStore {
    enum Phase: Equatable {
        case idle
        /// No sections are shown while the menu loads.
        case loading
        case loaded
        /// The menu failed to load, so there are no sections to show.
        case failed(String)
    }

    /// Weeks the strip asks for before and after the selected week, and per earlier page.
    static let weeksBefore = 8
    static let weeksAfter = 4
    /// How far back the strip goes when the server can't say where history starts.
    static let fallbackHistoryWeeks = 52

    private(set) var householdID: String?
    private(set) var selectedWeek: ISOWeek
    private(set) var menu: WeekMenu?
    private(set) var phase: Phase = .idle
    /// Set when a reload failed while the menu stayed on screen.
    private(set) var refreshError: String?
    /// What the server said about each week, by `YYYY-Www`.
    private(set) var weekSummaries: [String: WeekSummary] = [:]
    /// The strip's first week; earlier pages move it back.
    private(set) var oldestWeek: ISOWeek
    /// The household's first week with history; `nil` until the weeks load or when there's none.
    private(set) var earliestWeek: ISOWeek?
    private(set) var isLoadingEarlierWeeks = false
    /// Set when the week summaries couldn't load; the strip still shows weeks without counts.
    private(set) var weeksError: String?
    private(set) var hasLoadedWeeks = false
    /// `nil` until loaded, or when they couldn't load; views fall back to `MenuFilterOptions.fallback`.
    private(set) var filterOptions: MenuFilterOptions?
    /// The Menu screen's All Meals list.
    let allMeals: MenuRecipeList

    /// This week in the household's time zone.
    var currentWeek: ISOWeek { .current(in: timeZone, now: now()) }

    /// The selected week's timing, from the menu when it's loaded.
    var selectedTiming: WeekTiming {
        if let menu, menu.week == selectedWeek.description {
            return menu.timing
        }
        return timing(of: selectedWeek)
    }

    /// The strip's weeks, oldest first: from `oldestWeek` through a few weeks ahead.
    var stripItems: [WeekStripItem] {
        let current = currentWeek
        let newest = max(current.adding(weeks: Self.weeksAfter), selectedWeek)
        var items: [WeekStripItem] = []
        var week = min(oldestWeek, selectedWeek)
        while week <= newest, items.count < 520 {
            items.append(
                WeekStripItem(week: week, summary: summary(for: week), timing: timing(of: week)))
            week = week.next
        }
        return items
    }

    var canLoadEarlierWeeks: Bool {
        if let earliestWeek {
            return oldestWeek > earliestWeek
        }
        // Without a server answer, allow a year of weeks without counts.
        guard weeksError != nil else { return false }
        return oldestWeek > currentWeek.adding(weeks: -Self.fallbackHistoryWeeks)
    }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: MenuAPI?
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private var timeZone: TimeZone = .autoupdatingCurrent
    /// Incremented when the menu is replaced, so a slow response for an old week can't win.
    @ObservationIgnored private var menuGeneration = 0
    /// Incremented when the household changes, so old week and filter responses are dropped.
    @ObservationIgnored private var householdGeneration = 0
    /// The newest plan seen for each week, applied to menus that load after it.
    @ObservationIgnored private var knownPlans: [String: Plan] = [:]
    /// "Show More" lists, patched with plan changes while they're alive.
    @ObservationIgnored private var extraLists: [WeakList] = []

    private static let logger = Logger(subsystem: "DinnerOS", category: "menu")

    init(session: AuthSession, api: MenuAPI?, now: @escaping () -> Date = Date.init) {
        self.session = session
        self.api = api
        self.now = now
        let week = ISOWeek.current(in: .autoupdatingCurrent, now: now())
        selectedWeek = week
        oldestWeek = week.adding(weeks: -Self.weeksBefore)
        allMeals = MenuRecipeList(session: session, api: api)
    }

    /// A store frozen with the given state, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, menu: WeekMenu?, weeks: [WeekSummary] = [], allMeals: [MenuCard] = [],
        filters: MenuFilterOptions? = nil, phase: Phase = .loaded
    ) -> MenuStore {
        let store = MenuStore(session: session, api: nil)
        store.householdID = menu?.plan?.householdID ?? "household-preview"
        if let week = menu.flatMap({ ISOWeek($0.week) }) {
            store.selectedWeek = week
            store.oldestWeek = week.adding(weeks: -Self.weeksBefore)
        }
        store.menu = menu
        store.phase = phase
        store.weekSummaries = Dictionary(weeks.map { ($0.week, $0) }, uniquingKeysWith: { _, last in last })
        store.hasLoadedWeeks = true
        store.filterOptions = filters
        store.allMeals.configure(householdID: store.householdID ?? "", week: store.selectedWeek)
        if !allMeals.isEmpty {
            store.allMeals.replaceForPreview(items: allMeals)
        }
        return store
    }

    // MARK: - Loading

    /// Shows `householdID`'s menu, starting at this week in `timeZone`. Loads unless that
    /// household's menu is already loaded or loading.
    func activate(householdID: String, timeZone: TimeZone) async {
        self.timeZone = timeZone
        if householdID == self.householdID, phase != .idle {
            return
        }
        if householdID != self.householdID {
            clear()
            self.householdID = householdID
            selectedWeek = currentWeek
            oldestWeek = selectedWeek.adding(weeks: -Self.weeksBefore)
        }
        allMeals.configure(householdID: householdID, week: selectedWeek)
        async let weeks: Void = loadWeeks(around: selectedWeek)
        async let filters: Void = loadFilters()
        await loadMenu(clearing: true)
        await weeks
        await filters
    }

    /// Switches to `week`: loads its menu and marks All Meals for it.
    func select(week newWeek: ISOWeek) async {
        guard newWeek != selectedWeek || phase == .idle else { return }
        selectedWeek = newWeek
        async let meals: Void = allMeals.setWeek(newWeek)
        async let weeks: Void = loadWeeksIfOutside(newWeek)
        await loadMenu(clearing: true)
        await meals
        await weeks
    }

    /// Pull to refresh: the menu, the strip's counts, and All Meals, keeping what's on screen.
    func reload() async {
        async let menuLoad: Void = loadMenu(clearing: false)
        async let weeks: Void = loadWeeks(around: selectedWeek)
        async let meals: Void = allMeals.refresh()
        await menuLoad
        await weeks
        await meals
    }

    /// Reloads `week`'s menu when it's shown, for example after Autopilot's picks were added.
    func reloadWeek(_ week: ISOWeek) async {
        guard week == selectedWeek, householdID != nil else { return }
        async let weeks: Void = loadWeeks(around: week)
        await loadMenu(clearing: false)
        await weeks
    }

    /// Retries the menu after a failure.
    func retry() async {
        await loadMenu(clearing: true)
    }

    private func loadMenu(clearing: Bool) async {
        guard let api, let householdID else { return }
        menuGeneration += 1
        let started = menuGeneration
        let week = selectedWeek
        if clearing || menu?.week != week.description {
            menu = nil
            phase = .loading
        }
        do {
            var loaded = try await session.authorized { token in
                try await api.menu(householdID: householdID, week: week, accessToken: token)
            }
            guard started == menuGeneration, householdID == self.householdID else { return }
            if let known = knownPlans[loaded.week], Self.isNewer(known, than: loaded.plan) {
                loaded.apply(known)
            }
            menu = loaded
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if started == menuGeneration, phase == .loading { phase = .idle }
        } catch {
            guard started == menuGeneration else { return }
            Self.logger.notice("Menu load failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    /// Loads week summaries when `week` falls outside the loaded ones, such as a week picked
    /// from Past Weeks.
    private func loadWeeksIfOutside(_ week: ISOWeek) async {
        if week < oldestWeek {
            oldestWeek = week
        }
        guard weekSummaries[week.description] == nil, hasLoadedWeeks, weeksError == nil else { return }
        await loadWeeks(around: week)
    }

    private func loadWeeks(around week: ISOWeek) async {
        guard let api, let householdID else { return }
        let started = householdGeneration
        do {
            let response = try await session.authorized { token in
                try await api.weeks(
                    householdID: householdID, around: week, before: Self.weeksBefore, after: Self.weeksAfter,
                    accessToken: token)
            }
            guard started == householdGeneration else { return }
            merge(response)
            weeksError = nil
            hasLoadedWeeks = true
        } catch is CancellationError {
            return
        } catch {
            guard started == householdGeneration else { return }
            Self.logger.notice("Week list failed: \(Self.describe(error), privacy: .public)")
            weeksError = HouseholdStore.message(for: error)
            hasLoadedWeeks = true
        }
    }

    /// Loads the page of weeks before the strip's first week, down to `earliestWeek`.
    func loadEarlierWeeks() async {
        guard canLoadEarlierWeeks, !isLoadingEarlierWeeks, householdID != nil else { return }
        isLoadingEarlierWeeks = true
        defer { isLoadingEarlierWeeks = false }
        let around = oldestWeek.previous
        var target = around.adding(weeks: -Self.weeksBefore)
        if let api, let householdID, weeksError == nil {
            let started = householdGeneration
            do {
                let response = try await session.authorized { token in
                    try await api.weeks(
                        householdID: householdID, around: around, before: Self.weeksBefore, after: 0,
                        accessToken: token)
                }
                guard started == householdGeneration else { return }
                merge(response)
            } catch is CancellationError {
                return
            } catch {
                guard started == householdGeneration else { return }
                Self.logger.notice("Earlier weeks failed: \(Self.describe(error), privacy: .public)")
                weeksError = HouseholdStore.message(for: error)
            }
        }
        if let earliestWeek, target < earliestWeek {
            target = earliestWeek
        }
        oldestWeek = min(oldestWeek, target)
    }

    private func merge(_ response: WeekListResponse) {
        for summary in response.items {
            weekSummaries[summary.week] = summary
        }
        earliestWeek = response.earliestWeek.flatMap { ISOWeek($0) }
    }

    private func loadFilters() async {
        guard let api, let householdID else { return }
        let started = householdGeneration
        do {
            let options = try await session.authorized { token in
                try await api.filters(householdID: householdID, accessToken: token)
            }
            guard started == householdGeneration else { return }
            filterOptions = options
        } catch {
            // The chip bar falls back to sorts and times it knows.
            Self.logger.notice("Menu filters failed: \(Self.describe(error), privacy: .public)")
        }
    }

    // MARK: - Lists and lookups

    /// A separate All Meals list for "Show More", marked for the selected week. `pageSize`
    /// asks for a shorter page, for a caller that needs only a few rows.
    func makeList(query: MenuRecipeQuery, pageSize: Int = MenuRecipeList.pageSize) -> MenuRecipeList {
        let list = MenuRecipeList(
            session: session, api: api, query: query, householdID: householdID, week: selectedWeek,
            pageSize: pageSize)
        extraLists.removeAll { $0.list == nil }
        extraLists.append(WeakList(list))
        return list
    }

    /// The card for a recipe from the menu or All Meals, for its badges and facts.
    func card(forRecipeID recipeID: String) -> MenuCard? {
        if let card = menu?.sections.lazy.flatMap(\.items).first(where: { $0.recipe.id == recipeID }) {
            return card
        }
        return allMeals.items.first { $0.recipe.id == recipeID }
    }

    /// Whether a planned entry is an add-on rather than a main meal.
    ///
    /// A current server marks the entry itself; an older one doesn't, so the menu's own card
    /// for that recipe answers instead. Either way an add-on is never counted as a dinner.
    func isAddOn(_ entry: PlanEntry) -> Bool {
        entry.recipe.isAddon || card(forRecipeID: entry.recipe.id)?.recipe.isAddon == true
    }

    /// `entries` split into meals and add-ons.
    func mealCounts(of entries: [PlanEntry]) -> MealCounts {
        MealCounts.of(entries) { isAddOn($0) }
    }

    /// What the strip should say about `week`: what the server sent, with a plan already in hand
    /// applied over it.
    ///
    /// The split is worked out here rather than when the plan arrives, because whether an entry
    /// is an add-on can only be answered once the entry says so or the menu's cards are loaded,
    /// which may be after the plan. Reading it each time lets the pill settle on the same counts
    /// the bottom bar and Your Meals show, instead of keeping a guess made too early.
    func summary(for week: ISOWeek) -> WeekSummary? {
        guard var summary = weekSummaries[week.description] else { return nil }
        guard let plan = knownPlans[week.description] else { return summary }
        summary.status = WeekStatus(rawValue: plan.status.rawValue)
        let counts = mealCounts(of: plan.entries)
        // Nothing on hand names this week's add-ons, and the plan hasn't changed since the
        // server counted it, so keep the server's split rather than reading its add-ons as
        // extra dinners.
        guard counts.addOns < summary.addOnCount, counts.total == summary.plannedCount + summary.addOnCount
        else {
            summary.plannedCount = counts.meals
            summary.addOnCount = counts.addOns
            return summary
        }
        return summary
    }

    func timing(of week: ISOWeek) -> WeekTiming {
        weekSummaries[week.description]?.timing ?? WeekTiming.of(week, current: currentWeek)
    }

    // MARK: - Plan changes

    /// Shows a plan `PlanStore` received: patches the menu's cards, every All Meals list, and the
    /// week's planned count.
    func applyPlan(_ plan: Plan) {
        guard plan.householdID == householdID else { return }
        knownPlans[plan.week] = plan
        if var shown = menu, shown.week == plan.week {
            shown.apply(plan)
            menu = shown
        }
        allMeals.applyPlan(plan)
        extraLists.removeAll { $0.list == nil }
        for box in extraLists {
            box.list?.applyPlan(plan)
        }
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
    }

    private func clear() {
        menuGeneration += 1
        householdGeneration += 1
        menu = nil
        phase = .idle
        refreshError = nil
        weekSummaries = [:]
        earliestWeek = nil
        weeksError = nil
        hasLoadedWeeks = false
        isLoadingEarlierWeeks = false
        filterOptions = nil
        knownPlans = [:]
        extraLists = []
        allMeals.reset()
        selectedWeek = currentWeek
        oldestWeek = selectedWeek.adding(weeks: -Self.weeksBefore)
    }

    // MARK: - Helpers

    /// Whether `known` is at least as recent as the menu's plan, so it should replace it.
    private static func isNewer(_ known: Plan, than loaded: Plan?) -> Bool {
        guard let loaded else { return true }
        guard let knownDate = known.updatedAt else { return loaded.updatedAt == nil }
        guard let loadedDate = loaded.updatedAt else { return true }
        return knownDate >= loadedDate
    }

    private final class WeakList {
        weak var list: MenuRecipeList?

        init(_ list: MenuRecipeList) {
            self.list = list
        }
    }

    /// Status and error code only; never tokens or response bodies.
    static func describe(_ error: any Error) -> String {
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
