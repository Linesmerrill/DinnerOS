import Foundation
import Observation
import os

/// The "Try something else" browse surface and catalog search.
///
/// One store serves both: with no search text it asks for discovery, which
/// leaves out what the household already has; with search text it asks the
/// catalog, which marks those recipes instead of hiding them. Typing and
/// clearing therefore moves between the two without a second store to keep in
/// step.
@Observable
final class DiscoverStore {
    enum Phase: Equatable {
        case idle
        /// Nothing is shown while the first page loads.
        case loading
        case loaded
        /// The first page failed, so there's nothing to show.
        case failed(String)
    }

    static let pageSize = 24

    private(set) var householdID: String?
    private(set) var query = CatalogQuery()
    private(set) var phase: Phase = .idle
    private(set) var items: [CatalogSummary] = []
    private(set) var nextCursor: String?
    private(set) var isLoadingMore = false
    /// Set when the next page failed; loaded items stay on screen.
    private(set) var loadMoreError: String?
    /// Set when a refresh failed while items stayed on screen.
    private(set) var refreshError: String?
    /// Recipe IDs being added right now, so their buttons can show progress.
    private(set) var adding: Set<String> = []
    /// The last add that failed, for an alert.
    var addError: String?

    var hasMore: Bool { nextCursor != nil }
    var isSearching: Bool { query.isSearching }

    /// Called with the household's new recipe after a successful add, so the
    /// library and menu can show it without a reload.
    @ObservationIgnored var recipeWasAdded: ((String) async -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: CatalogAPI?
    @ObservationIgnored private let pageSize: Int
    /// Incremented whenever the list restarts, so a slow response for an old
    /// query or household can't overwrite newer state.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "discover")

    init(session: AuthSession, api: CatalogAPI?, pageSize: Int = DiscoverStore.pageSize) {
        self.session = session
        self.api = api
        self.pageSize = pageSize
    }

    /// A store frozen with `items`, for SwiftUI previews. It has no network access.
    static func preview(session: AuthSession, items: [CatalogSummary], phase: Phase = .loaded) -> DiscoverStore {
        let store = DiscoverStore(session: session, api: nil)
        store.householdID = "household-preview"
        store.items = items
        store.phase = phase
        return store
    }

    // MARK: - Loading

    /// Points the store at a household. Changing it clears everything, so one
    /// household's browse results can never be shown under another.
    func activate(householdID: String) async {
        if householdID != self.householdID {
            clear()
            query = CatalogQuery()
            self.householdID = householdID
        }
        await load()
    }

    /// Loads the first page unless it's loaded or loading.
    func load() async {
        guard phase == .idle else { return }
        await loadFirstPage(clearing: true)
    }

    /// Applies a new query, restarting from the first page when the server would see a change.
    func setQuery(_ newQuery: CatalogQuery) async {
        let needsLoad = !newQuery.isSameRequest(as: query) || phase == .idle
        query = newQuery
        guard needsLoad else { return }
        await loadFirstPage(clearing: true)
    }

    /// Convenience for a search field.
    func setSearch(_ text: String) async {
        var next = query
        next.search = text
        await setQuery(next)
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
        let limit = pageSize
        isLoadingMore = true
        loadMoreError = nil
        defer {
            if started == generation { isLoadingMore = false }
        }
        do {
            let page = try await Self.fetch(
                api: api, session: session, householdID: householdID, query: query, cursor: cursor, limit: limit)
            guard started == generation else { return }
            // A recipe that moved between pages can appear twice; keep the first.
            let known = Set(items.map(\.id))
            items.append(contentsOf: page.items.filter { !known.contains($0.id) })
            nextCursor = page.nextCursor
        } catch is CancellationError {
            return
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Discover page failed: \(Self.describe(error), privacy: .public)")
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

    /// One catalog recipe in full. Nothing is cached: a catalog detail is read
    /// once, on the screen that shows it.
    func recipe(id: String) async throws -> CatalogRecipe {
        guard let api, let householdID else { throw APIError.invalidResponse }
        return try await session.authorized { token in
            try await api.recipe(householdID: householdID, id: id, accessToken: token)
        }
    }

    // MARK: - Adding

    /// Copies a catalog recipe into the household's library.
    ///
    /// The row is marked immediately on success. While browsing, a recipe that
    /// has just been added no longer belongs on a list of things the household
    /// does not have, so it is removed; while searching, it stays and is marked,
    /// because it is still a result for what was typed.
    @discardableResult
    func add(_ summary: CatalogSummary) async -> Bool {
        guard let api, let householdID, !adding.contains(summary.id) else { return false }
        adding.insert(summary.id)
        defer { adding.remove(summary.id) }
        do {
            let result = try await session.authorized { token in
                try await api.add(householdID: householdID, id: summary.id, accessToken: token)
            }
            mark(id: summary.id, libraryRecipeID: result.recipeID)
            await recipeWasAdded?(result.recipeID)
            return true
        } catch is CancellationError {
            return false
        } catch {
            Self.logger.notice("Add to library failed: \(Self.describe(error), privacy: .public)")
            addError = HouseholdStore.message(for: error)
            return false
        }
    }

    private func mark(id: String, libraryRecipeID: String) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        if isSearching {
            let item = items[index]
            items[index] = CatalogSummary(
                id: item.id, catalogKey: item.catalogKey, source: item.source, name: item.name,
                headline: item.headline, imageURLString: item.imageURLString, isAddon: item.isAddon,
                totalMinutes: item.totalMinutes, cookMinutes: item.cookMinutes, timeBand: item.timeBand,
                calories: item.calories, proteinGrams: item.proteinGrams, cuisines: item.cuisines, tags: item.tags,
                inLibrary: true, libraryRecipeID: libraryRecipeID, reasons: item.reasons)
        } else {
            items.remove(at: index)
        }
    }

    // MARK: - Lifecycle

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
        query = CatalogQuery()
    }

    private func clear() {
        generation += 1
        phase = .idle
        items = []
        nextCursor = nil
        isLoadingMore = false
        loadMoreError = nil
        refreshError = nil
        adding = []
        addError = nil
    }

    private func loadFirstPage(clearing: Bool) async {
        guard let api, let householdID else { return }
        generation += 1
        let started = generation
        let query = query
        let limit = pageSize
        isLoadingMore = false
        loadMoreError = nil
        if clearing || items.isEmpty {
            items = []
            nextCursor = nil
            phase = .loading
        }
        do {
            let page = try await Self.fetch(
                api: api, session: session, householdID: householdID, query: query, cursor: nil, limit: limit)
            guard started == generation else { return }
            items = page.items
            nextCursor = page.nextCursor
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            if started == generation, phase == .loading { phase = .idle }
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Discover failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    /// Search text picks the endpoint: the catalog when there is any, discovery when there isn't.
    private static func fetch(
        api: CatalogAPI, session: AuthSession, householdID: String, query: CatalogQuery, cursor: String?, limit: Int
    ) async throws -> CatalogListPage {
        try await session.authorized { token in
            if query.isSearching {
                return try await api.search(
                    householdID: householdID, query: query, cursor: cursor, limit: limit, accessToken: token)
            }
            return try await api.discover(
                householdID: householdID, query: query, cursor: cursor, limit: limit, accessToken: token)
        }
    }

    /// Status, code, and request ID only — never a response body.
    private static func describe(_ error: any Error) -> String {
        guard let apiError = error as? APIError else { return "\(type(of: error))" }
        return apiError.code ?? "\(apiError.status ?? 0)"
    }
}
