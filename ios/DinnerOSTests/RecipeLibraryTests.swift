import Foundation
import Testing

@testable import DinnerOS

struct RecipeLibraryTests {
    private struct Harness {
        let library: RecipeLibrary
        let server: FakeRecipeServer
    }

    /// Five mains and two add-ons in `household-1`, two mains in `household-2`, and a page
    /// size of 2.
    private func makeHarness(pageSize: Int = 2) async throws -> Harness {
        let server = FakeRecipeServer(
            .init(recipes: [
                "household-1": [
                    .init(id: "r1", name: "Alpha Tacos"), .init(id: "r2", name: "Bravo Bowl"),
                    .init(id: "r3", name: "Charlie Tacos"), .init(id: "r4", name: "Delta Curry"),
                    .init(id: "r5", name: "Echo Stew"),
                    .init(id: "a1", name: "Foxtrot Cookies", isAddon: true),
                    .init(id: "a2", name: "Golf Garlic Bread", isAddon: true),
                ],
                "household-2": [.init(id: "x1", name: "Hotel Hash"), .init(id: "x2", name: "India Salad")],
            ]))
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let library = RecipeLibrary(session: session, api: RecipesAPI(client: client), pageSize: pageSize)
        return Harness(library: library, server: server)
    }

    @Test func loadsFirstPageThenMoreUntilTheEnd() async throws {
        let harness = try await makeHarness()
        let library = harness.library

        await library.activate(householdID: "household-1")

        #expect(library.phase == .loaded)
        #expect(library.items.map(\.id) == ["r1", "r2"])
        #expect(library.hasMore)
        #expect(harness.server.listQueries.last == ["sort": "recent", "limit": "2"])

        await library.setFilters(RecipeListFilters(kind: .mains))
        #expect(library.items.map(\.id) == ["r1", "r2"])

        await library.loadMore()
        #expect(library.items.map(\.id) == ["r1", "r2", "r3", "r4"])
        #expect(harness.server.listQueries.last?["cursor"] == "2")
        #expect(harness.server.listQueries.last?["addons"] == "false")

        await library.loadMore()
        #expect(library.items.map(\.id) == ["r1", "r2", "r3", "r4", "r5"])
        #expect(!library.hasMore)

        let requests = harness.server.listQueries.count
        await library.loadMore()
        #expect(harness.server.listQueries.count == requests)
        #expect(!library.isLoadingMore)
    }

    @Test func searchRestartsWithoutACursor() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")
        await library.loadMore()
        #expect(library.nextCursor == "4")

        await library.setSearch("tacos")

        #expect(harness.server.listQueries.last == ["sort": "recent", "q": "tacos", "limit": "2"])
        #expect(library.items.map(\.id) == ["r1", "r3"])
        #expect(library.nextCursor == nil)
        #expect(library.phase == .loaded)

        // Whitespace the server never sees doesn't refetch.
        let requests = harness.server.listQueries.count
        await library.setSearch("tacos ")
        #expect(harness.server.listQueries.count == requests)
        #expect(library.filters.search == "tacos ")
    }

    @Test func emptySearchResultIsLoadedAndNarrowed() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")

        await library.setSearch("zzz")

        #expect(library.phase == .loaded)
        #expect(library.items.isEmpty)
        #expect(library.filters.isNarrowed)
    }

    @Test func sortAndKindChangesRefetch() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")

        await library.setFilters(RecipeListFilters(sort: .name, kind: .addons))

        #expect(harness.server.listQueries.last == ["sort": "name", "addons": "true", "limit": "2"])
        #expect(library.items.map(\.id) == ["a1", "a2"])
        #expect(!library.hasMore)
    }

    @Test func firstPageErrorThenRetry() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        harness.server.failNext()

        await library.activate(householdID: "household-1")

        guard case .failed(let message) = library.phase else {
            Issue.record("expected a failure, got \(library.phase)")
            return
        }
        #expect(message.localizedCaseInsensitiveContains("server"))
        #expect(library.items.isEmpty)

        await library.retry()

        #expect(library.phase == .loaded)
        #expect(library.items.map(\.id) == ["r1", "r2"])
        #expect(harness.server.listQueries.count == 2)
    }

    @Test func loadMoreErrorKeepsItemsThenRetryContinues() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")
        harness.server.failNext()

        await library.loadMore()

        #expect(library.loadMoreError != nil)
        #expect(library.phase == .loaded)
        #expect(library.items.map(\.id) == ["r1", "r2"])
        #expect(library.nextCursor == "2")
        #expect(!library.isLoadingMore)

        await library.retry()

        #expect(library.loadMoreError == nil)
        #expect(library.items.map(\.id) == ["r1", "r2", "r3", "r4"])
        #expect(harness.server.listQueries.last?["cursor"] == "2")
    }

    @Test func refreshFailureKeepsItemsOnScreen() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")
        harness.server.failNext()

        await library.refresh()

        #expect(library.phase == .loaded)
        #expect(library.refreshError != nil)
        #expect(library.items.map(\.id) == ["r1", "r2"])

        await library.refresh()
        #expect(library.refreshError == nil)
    }

    @Test func reactivatingTheSameHouseholdUsesTheLoadedList() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")
        await library.loadMore()

        await library.activate(householdID: "household-1")

        #expect(harness.server.listQueries.count == 2)
        #expect(library.items.count == 4)
    }

    @Test func switchingHouseholdsClearsListFiltersAndCache() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")
        await library.setSearch("tacos")
        _ = try await library.recipe(id: "r1")
        #expect(library.cachedRecipe(id: "r1") != nil)

        await library.activate(householdID: "household-2")

        #expect(library.householdID == "household-2")
        #expect(library.filters == RecipeListFilters())
        #expect(library.items.map(\.id) == ["x1", "x2"])
        #expect(library.cachedRecipe(id: "r1") == nil)
        #expect(harness.server.listQueries.last?["cursor"] == nil)
    }

    @Test func recipesAreCachedUntilReloadOrRefresh() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")

        let first = try await library.recipe(id: "r1")
        let second = try await library.recipe(id: "r1")
        #expect(first == second)
        #expect(harness.server.detailRequests == 1)

        _ = try await library.recipe(id: "r1", reload: true)
        #expect(harness.server.detailRequests == 2)

        await library.refresh()
        #expect(library.cachedRecipe(id: "r1") == nil)
        _ = try await library.recipe(id: "r1")
        #expect(harness.server.detailRequests == 3)
    }

    @Test func resetForgetsEverything() async throws {
        let harness = try await makeHarness()
        let library = harness.library
        await library.activate(householdID: "household-1")

        library.reset()

        #expect(library.householdID == nil)
        #expect(library.phase == .idle)
        #expect(library.items.isEmpty)
        #expect(!library.hasMore)
    }
}
