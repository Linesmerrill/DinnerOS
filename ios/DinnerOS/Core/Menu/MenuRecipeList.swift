import Foundation
import Observation
import os

/// One All Meals list: a query, the week its cards are marked for, and cursor paging.
///
/// `MenuStore` owns the Menu screen's list; "Show More" gets its own so its filters and
/// paging don't change the Menu screen's. Every list is patched when a plan changes.
@Observable
final class MenuRecipeList {
    enum Phase: Equatable {
        case idle
        /// No items are shown while the first page loads.
        case loading
        case loaded
        /// The first page failed, so there's nothing to show.
        case failed(String)
    }

    static let pageSize = 24

    private(set) var householdID: String?
    private(set) var week: ISOWeek?
    private(set) var query: MenuRecipeQuery
    private(set) var phase: Phase = .idle
    private(set) var items: [MenuCard] = []
    private(set) var nextCursor: String?
    private(set) var isLoadingMore = false
    /// Set when the next page failed; the loaded items stay on screen.
    private(set) var loadMoreError: String?
    /// Set when a refresh failed while items stayed on screen.
    private(set) var refreshError: String?

    var hasMore: Bool { nextCursor != nil }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: MenuAPI?
    @ObservationIgnored private let pageSize: Int
    /// Incremented whenever the list restarts, so a slow response for an old query, week, or
    /// household can't overwrite newer state.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "menu")

    init(
        session: AuthSession, api: MenuAPI?, query: MenuRecipeQuery = MenuRecipeQuery(), householdID: String? = nil,
        week: ISOWeek? = nil, pageSize: Int = MenuRecipeList.pageSize
    ) {
        self.session = session
        self.api = api
        self.query = query
        self.householdID = householdID
        self.week = week
        self.pageSize = pageSize
    }

    /// A list frozen with `items`, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, items: [MenuCard], query: MenuRecipeQuery = MenuRecipeQuery(), phase: Phase = .loaded,
        nextCursor: String? = nil
    ) -> MenuRecipeList {
        let list = MenuRecipeList(session: session, api: nil, query: query, householdID: "household-preview")
        list.items = items
        list.phase = phase
        list.nextCursor = nextCursor
        return list
    }

    // MARK: - Loading

    /// Points the list at a household and week without loading. Changing either clears it.
    func configure(householdID: String, week: ISOWeek) {
        guard householdID != self.householdID || week != self.week else { return }
        if householdID != self.householdID {
            query = MenuRecipeQuery()
        }
        clear()
        self.householdID = householdID
        self.week = week
    }

    /// Loads the first page unless it's loaded or loading.
    func load() async {
        guard phase == .idle else { return }
        await loadFirstPage(clearing: true)
    }

    /// Marks cards for `week`. Reloads when the list was already shown.
    func setWeek(_ newWeek: ISOWeek) async {
        guard newWeek != week else { return }
        week = newWeek
        guard phase != .idle else { return }
        await loadFirstPage(clearing: true)
    }

    /// Applies a new query. A change the server would see restarts from the first page without
    /// a cursor; so does any call while the list is idle.
    func setQuery(_ newQuery: MenuRecipeQuery) async {
        let needsLoad = !newQuery.isSameRequest(as: query) || phase == .idle
        query = newQuery
        guard needsLoad else { return }
        await loadFirstPage(clearing: true)
    }

    /// Pull to refresh: items stay on screen until the response arrives.
    func refresh() async {
        guard phase != .idle else { return }
        await loadFirstPage(clearing: false)
    }

    /// Loads the next page when there is one and nothing else is loading.
    func loadMore() async {
        guard let api, let householdID, let cursor = nextCursor, phase == .loaded, !isLoadingMore else { return }
        let started = generation
        let query = query
        let week = week
        let limit = pageSize
        isLoadingMore = true
        loadMoreError = nil
        defer {
            if started == generation { isLoadingMore = false }
        }
        do {
            let page = try await session.authorized { token in
                try await api.recipes(
                    householdID: householdID, query: query, week: week, cursor: cursor, limit: limit,
                    accessToken: token)
            }
            guard started == generation else { return }
            // A recipe that moved between pages can appear twice; keep the first.
            let known = Set(items.map(\.id))
            items.append(contentsOf: page.items.filter { !known.contains($0.id) })
            nextCursor = page.nextCursor
        } catch is CancellationError {
            return
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Menu recipe page failed: \(MenuStore.describe(error), privacy: .public)")
            loadMoreError = HouseholdStore.message(for: error)
        }
    }

    /// Retries whatever failed: the first page, or the next one.
    func retry() async {
        switch phase {
        case .failed, .idle:
            await loadFirstPage(clearing: true)
        case .loaded where loadMoreError != nil:
            await loadMore()
        default:
            break
        }
    }

    private func loadFirstPage(clearing: Bool) async {
        guard let api, let householdID else { return }
        generation += 1
        let started = generation
        let query = query
        let week = week
        let limit = pageSize
        isLoadingMore = false
        loadMoreError = nil
        if clearing || items.isEmpty {
            items = []
            nextCursor = nil
            phase = .loading
        }
        do {
            let page = try await session.authorized { token in
                try await api.recipes(
                    householdID: householdID, query: query, week: week, cursor: nil, limit: limit, accessToken: token)
            }
            guard started == generation else { return }
            items = page.items
            nextCursor = page.nextCursor
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if started == generation, phase == .loading { phase = .idle }
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Menu recipes failed: \(MenuStore.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    // MARK: - Changes

    /// Recomputes each card's plan state when `plan` is for this list's household and week.
    func applyPlan(_ plan: Plan) {
        guard plan.householdID == householdID, plan.week == week?.description else { return }
        items = MenuCard.patching(items, with: plan)
    }

    /// Shows `items` without a request, for SwiftUI previews.
    func replaceForPreview(items cards: [MenuCard]) {
        items = cards
        phase = .loaded
    }

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
        week = nil
        query = MenuRecipeQuery()
    }

    private func clear() {
        generation += 1
        phase = .idle
        items = []
        nextCursor = nil
        isLoadingMore = false
        loadMoreError = nil
        refreshError = nil
    }
}
