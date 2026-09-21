import Foundation
import Testing

@testable import DinnerOS

struct DiscoverStoreTests {
    private struct Harness {
        let store: DiscoverStore
        let server: FakeCatalogServer
    }

    private func makeHarness(
        discoverPages: [Data] = [CatalogFixtures.page([CatalogFixtures.summary(id: "c1", name: "Alpha Bowl")])],
        searchPages: [Data] = [CatalogFixtures.page([CatalogFixtures.summary(id: "c2", name: "Beta Bowl")])],
        pageSize: Int = 2
    ) async throws -> Harness {
        let server = FakeCatalogServer(
            .init(
                discoverPages: discoverPages, searchPages: searchPages, detail: CatalogFixtures.detail,
                addResult: CatalogFixtures.addResult(recipeID: "recipe-new", created: true)))
        let transport = StubTransport { request in server.handle(request) }
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let session = AuthSession(
            api: AuthAPI(client: client),
            store: InMemoryTokenStore(session: StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)))
        await session.restore()
        let store = DiscoverStore(session: session, api: CatalogAPI(client: client), pageSize: pageSize)
        return Harness(store: store, server: server)
    }

    @Test func browsingAsksDiscoveryAndSearchingAsksTheCatalog() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        #expect(harness.store.phase == .loaded)
        #expect(harness.store.items.map(\.name) == ["Alpha Bowl"])
        #expect(harness.server.requestedPaths.contains { $0.contains("/discover") })

        await harness.store.setSearch("beta")
        #expect(harness.store.isSearching)
        #expect(harness.store.items.map(\.name) == ["Beta Bowl"])
        let searched = try #require(harness.server.requestedPaths.last)
        #expect(searched.contains("/catalog/recipes"))
        #expect(searched.contains("q=beta"))

        // Clearing the field goes back to discovery rather than an empty search.
        await harness.store.setSearch("  ")
        #expect(!harness.store.isSearching)
        #expect(try #require(harness.server.requestedPaths.last).contains("/discover"))
    }

    @Test func pagesWithTheCursorUntilTheEnd() async throws {
        let harness = try await makeHarness(discoverPages: [
            CatalogFixtures.page(
                [CatalogFixtures.summary(id: "c1", name: "A"), CatalogFixtures.summary(id: "c2", name: "B")],
                nextCursor: "1", total: 3),
            CatalogFixtures.page([CatalogFixtures.summary(id: "c3", name: "C")], total: 3),
        ])
        await harness.store.activate(householdID: "household-1")
        #expect(harness.store.hasMore)

        await harness.store.loadMore()
        #expect(harness.store.items.map(\.name) == ["A", "B", "C"])
        #expect(!harness.store.hasMore)
    }

    @Test func aFailedFirstPageIsRetryable() async throws {
        let harness = try await makeHarness()
        harness.server.failNext()
        await harness.store.activate(householdID: "household-1")
        guard case .failed = harness.store.phase else {
            Issue.record("expected a failed phase, got \(harness.store.phase)")
            return
        }
        await harness.store.retry()
        #expect(harness.store.phase == .loaded)
    }

    @Test func addingWhileBrowsingRemovesTheRowAndTellsTheLibrary() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        let added = Counter()
        harness.store.recipeWasAdded = { _ in added.increment() }
        let item = try #require(harness.store.items.first)

        let ok = await harness.store.add(item)
        #expect(ok)
        #expect(harness.store.items.isEmpty, "a recipe you now own doesn't belong in 'things you don't have'")
        #expect(added.value == 1)
        #expect(harness.server.requestedPaths.contains { $0.hasPrefix("POST") && $0.hasSuffix("/add") })
    }

    @Test func addingWhileSearchingMarksTheRowInstead() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        await harness.store.setSearch("beta")
        let item = try #require(harness.store.items.first)

        #expect(await harness.store.add(item))
        let marked = try #require(harness.store.items.first)
        #expect(marked.inLibrary)
        #expect(marked.libraryRecipeID == "recipe-new")
    }

    @Test func aFailedAddSurfacesAMessageAndLeavesTheRow() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        let item = try #require(harness.store.items.first)
        harness.server.failNext()

        #expect(await harness.store.add(item) == false)
        #expect(harness.store.addError != nil)
        #expect(harness.store.items.count == 1)
    }

    @Test func switchingHouseholdsClearsWhatTheOtherOneSaw() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        #expect(!harness.store.items.isEmpty)

        await harness.store.setSearch("beta")
        await harness.store.activate(householdID: "household-2")
        #expect(!harness.store.isSearching, "a search from another household carried over")
        #expect(harness.store.items.map(\.name) == ["Alpha Bowl"])
    }

    @Test func signOutForgetsEverything() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        harness.store.reset()
        #expect(harness.store.items.isEmpty)
        #expect(harness.store.phase == .idle)
    }

    @Test func aCatalogEntryWithOnlyTheRequiredFieldsStillDecodes() async throws {
        let harness = try await makeHarness(discoverPages: [CatalogFixtures.minimalPage])
        await harness.store.activate(householdID: "household-1")
        let item = try #require(harness.store.items.first)
        #expect(item.name == "Plain Toast")
        #expect(!item.inLibrary)
        #expect(item.reasons.isEmpty)
    }

    @Test func aCatalogDetailLoads() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        let recipe = try await harness.store.recipe(id: "catalog-1")
        #expect(recipe.name == "Thai Green Curry")
        #expect(recipe.ingredients.count == 1)
        #expect(recipe.steps.count == 1)
    }
}

struct CatalogAPIQueryTests {
    @Test func discoverySendsNoSearchTerm() {
        let items = CatalogAPI.queryItems(
            query: CatalogQuery(search: "tacos", cuisine: "Thai", tag: nil), includeSearch: false, cursor: nil,
            limit: 24)
        #expect(items.map(\.name) == ["cuisine", "limit"])
    }

    @Test func searchSendsTheTrimmedTermAndTheCursor() {
        let items = CatalogAPI.queryItems(
            query: CatalogQuery(search: "  salt & pepper  "), includeSearch: true, cursor: "abc", limit: 10)
        #expect(items.first(where: { $0.name == "q" })?.value == "salt & pepper")
        #expect(items.first(where: { $0.name == "cursor" })?.value == "abc")
        #expect(items.first(where: { $0.name == "limit" })?.value == "10")
    }

    @Test func emptyFiltersAreLeftOut() {
        let items = CatalogAPI.queryItems(
            query: CatalogQuery(search: "", cuisine: "", tag: ""), includeSearch: true, cursor: "", limit: 24)
        #expect(items.map(\.name) == ["limit"])
    }

    @Test func aLongSearchIsCappedBeforeItIsSent() {
        let query = CatalogQuery(search: String(repeating: "a", count: 200))
        #expect(query.normalizedSearch.count == CatalogQuery.maxSearchLength)
    }
}
