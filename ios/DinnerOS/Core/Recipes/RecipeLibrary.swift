import Foundation
import Observation
import os

/// The current household's recipe list and a cache of loaded recipes.
///
/// Main-actor state that lives as long as the app, so returning to the Recipes tab or
/// popping back from a recipe shows what was already loaded. Switching households or
/// signing out clears it.
@Observable
final class RecipeLibrary {
    /// The status of the first page for the current filters.
    enum Phase: Equatable {
        case idle
        /// No items are shown while the first page loads.
        case loading
        case loaded
        /// The first page failed, so there's nothing to show.
        case failed(String)
    }

    static let pageSize = 30

    private(set) var householdID: String?
    private(set) var filters = RecipeListFilters()
    private(set) var phase: Phase = .idle
    private(set) var items: [RecipeSummary] = []
    private(set) var nextCursor: String?
    private(set) var isLoadingMore = false
    /// Set when the next page failed; the loaded items stay on screen.
    private(set) var loadMoreError: String?
    /// Set when a pull to refresh failed while items stayed on screen.
    private(set) var refreshError: String?

    var hasMore: Bool { nextCursor != nil }

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: RecipesAPI?
    @ObservationIgnored private let pageSize: Int
    @ObservationIgnored private var details: [String: Recipe] = [:]
    /// Incremented whenever the list restarts, so a slow response for old filters or an
    /// old household can't overwrite newer state.
    @ObservationIgnored private var generation = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "recipes")

    init(session: AuthSession, api: RecipesAPI?, pageSize: Int = RecipeLibrary.pageSize) {
        self.session = session
        self.api = api
        self.pageSize = pageSize
    }

    /// A library frozen with the given items, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, phase: Phase = .loaded, items: [RecipeSummary] = [], recipes: [Recipe] = []
    ) -> RecipeLibrary {
        let library = RecipeLibrary(session: session, api: nil)
        library.householdID = recipes.first?.householdID ?? "household-preview"
        library.phase = phase
        library.items = items
        library.details = Dictionary(recipes.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return library
    }

    /// A separate library over the same API, for a picker whose search and paging must
    /// not change this list. A preview library's copy starts with the same items.
    func makeIndependentCopy() -> RecipeLibrary {
        let copy = RecipeLibrary(session: session, api: api, pageSize: pageSize)
        if api == nil {
            copy.householdID = householdID
            copy.phase = phase
            copy.items = items
            copy.details = details
        }
        return copy
    }

    // MARK: - List

    /// Shows `householdID`'s recipes. Loads the first page unless that household's list
    /// is already loaded or loading.
    func activate(householdID: String) async {
        if householdID == self.householdID, phase != .idle {
            return
        }
        if householdID != self.householdID {
            clear()
            self.householdID = householdID
        }
        await loadFirstPage(clearing: true)
    }

    /// Applies new filters. A change the server would see restarts from the first page
    /// without a cursor; so does any call while the list is idle (a cancelled load).
    func setFilters(_ newFilters: RecipeListFilters) async {
        let needsLoad = !newFilters.isSameQuery(as: filters) || phase == .idle
        filters = newFilters
        guard needsLoad else { return }
        await loadFirstPage(clearing: true)
    }

    func setSearch(_ search: String) async {
        var updated = filters
        updated.search = search
        await setFilters(updated)
    }

    /// Pull to refresh: reloads the first page and forgets cached recipes. Items stay on
    /// screen until the response arrives.
    func refresh() async {
        guard householdID != nil else { return }
        details = [:]
        await loadFirstPage(clearing: false)
    }

    /// Loads the next page when there is one and nothing else is loading.
    func loadMore() async {
        guard
            let api, let householdID, let cursor = nextCursor,
            phase == .loaded, !isLoadingMore
        else { return }
        let started = generation
        let filters = filters
        let limit = pageSize
        isLoadingMore = true
        loadMoreError = nil
        defer {
            if started == generation { isLoadingMore = false }
        }
        do {
            let page = try await session.authorized { token in
                try await api.listRecipes(
                    householdID: householdID, filters: filters, cursor: cursor, limit: limit, accessToken: token)
            }
            guard started == generation else { return }
            // A recipe renamed between pages can appear twice; keep the first.
            let known = Set(items.map(\.id))
            items.append(contentsOf: page.items.filter { !known.contains($0.id) })
            nextCursor = page.nextCursor
        } catch is CancellationError {
            return
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Recipe page failed: \(Self.describe(error), privacy: .public)")
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
        let filters = filters
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
                try await api.listRecipes(
                    householdID: householdID, filters: filters, cursor: nil, limit: limit, accessToken: token)
            }
            guard started == generation else { return }
            items = page.items
            nextCursor = page.nextCursor
            refreshError = nil
            phase = .loaded
        } catch is CancellationError {
            // A cancelled view task; the next activation loads again.
            if started == generation, phase == .loading { phase = .idle }
        } catch {
            guard started == generation else { return }
            Self.logger.notice("Recipe list failed: \(Self.describe(error), privacy: .public)")
            let message = HouseholdStore.message(for: error)
            if phase == .loaded {
                refreshError = message
            } else {
                phase = .failed(message)
            }
        }
    }

    // MARK: - Recipes

    func cachedRecipe(id: String) -> Recipe? {
        details[id]
    }

    /// The full recipe, from the cache unless `reload` is set.
    func recipe(id: String, reload: Bool = false) async throws -> Recipe {
        if !reload, let cached = details[id] {
            return cached
        }
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        let started = generation
        let recipe = try await session.authorized { token in
            try await api.recipe(householdID: householdID, id: id, accessToken: token)
        }
        if householdID == self.householdID, started == generation || details[id] == nil {
            details[id] = recipe
        }
        return recipe
    }

    // MARK: - Customizations and pairings

    /// The recipe's swappable ingredients. Empty when it has none, including a `404` from a
    /// server without customizations. Not cached: the screen loads them when it opens.
    func customizations(recipeID: String) async throws -> [CustomizationGroup] {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        do {
            return try await session.authorized { token in
                try await api.customizations(householdID: householdID, recipeID: recipeID, accessToken: token)
            }.groups
        } catch let error as APIError where error.status == 404 {
            return []
        }
    }

    /// Add-ons that go with the recipe, marked for `week`. The whole response is returned, not
    /// just the items: `entryId` says which planned meal an accept belongs to. Empty when there
    /// are none, including a `404` from a server without pairings. Not cached.
    func pairings(recipeID: String, week: ISOWeek?) async throws -> RecipePairings {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        do {
            return try await session.authorized { token in
                try await api.pairings(householdID: householdID, recipeID: recipeID, week: week, accessToken: token)
            }
        } catch let error as APIError where error.status == 404 {
            return .empty
        }
    }

    // MARK: - Ratings

    /// Saves the signed-in user's rating of a recipe, then refreshes that recipe in the
    /// cache and its row in the list so the household average matches the server.
    @discardableResult
    func saveRating(_ draft: RatingDraft, recipeID: String) async throws -> RecipeRating {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        let body = draft.request
        let saved = try await session.authorized { token in
            try await api.rate(householdID: householdID, recipeID: recipeID, request: body, accessToken: token)
        }
        Self.logger.info("Rating saved")
        await refreshRating(recipeID: recipeID, householdID: householdID, mine: saved)
        return saved
    }

    /// Removes the signed-in user's rating, then refreshes the recipe like `saveRating`.
    func removeRating(recipeID: String) async throws {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        try await session.authorized { token in
            try await api.removeRating(householdID: householdID, recipeID: recipeID, accessToken: token)
        }
        Self.logger.info("Rating removed")
        await refreshRating(recipeID: recipeID, householdID: householdID, mine: nil)
    }

    /// Every member's rating of a recipe. Not cached: it's shown on its own screen.
    func ratings(recipeID: String) async throws -> RatingListResponse {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        return try await session.authorized { token in
            try await api.ratings(householdID: householdID, recipeID: recipeID, accessToken: token)
        }
    }

    /// Reloads the rated recipe so the cache and list row carry the new household average.
    /// When the reload fails, the rating is already saved, so only the user's own rating
    /// is patched in and the average waits for the next load.
    private func refreshRating(recipeID: String, householdID: String, mine: RecipeRating?) async {
        do {
            let recipe = try await recipe(id: recipeID, reload: true)
            guard householdID == self.householdID else { return }
            details[recipeID] = recipe
            updateSummary(id: recipeID) {
                $0.householdRating = recipe.householdRating
                $0.myRating = recipe.myRating
            }
        } catch {
            guard householdID == self.householdID else { return }
            Self.logger.notice("Rated recipe reload failed: \(Self.describe(error), privacy: .public)")
            details[recipeID]?.myRating = mine
            updateSummary(id: recipeID) { $0.myRating = mine }
        }
    }

    private func updateSummary(id: String, _ change: (inout RecipeSummary) -> Void) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        change(&items[index])
    }

    // MARK: - Manual entry

    /// A store for one "Add a Recipe" sheet, sharing this library's session and
    /// API client. A draft belongs to the sheet the member opened, so it is
    /// created per screen rather than held here.
    func makeDraftStore(householdID: String) -> RecipeDraftStore {
        RecipeDraftStore(session: session, api: api, householdID: householdID)
    }

    /// Shows a recipe the member just added, so the library reflects it without
    /// a round trip. The list reloads on its next refresh either way.
    func recipeWasAdded() async {
        guard phase == .loaded else { return }
        await refresh()
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
    }

    private func clear() {
        generation += 1
        filters = RecipeListFilters()
        phase = .idle
        items = []
        nextCursor = nil
        isLoadingMore = false
        loadMoreError = nil
        refreshError = nil
        details = [:]
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
