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

    /// Hiding a cook takes the recipe off the list, says so, and reloads so the cook's other
    /// recipes go too.
    @Test func hidingACookReloadsTheLists() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        let before = harness.server.requestedPaths.count
        #expect(await harness.store.hideCook(id: "c1"))
        let paths = harness.server.requestedPaths.dropFirst(before)
        #expect(paths.first == "POST /api/v1/households/household-1/catalog/recipes/c1/hide-cook")
        #expect(paths.contains { $0.contains("/discover/home") })
        #expect(paths.contains { $0.contains("/discover") && !$0.contains("/discover/home") })
        #expect(harness.store.reportTitle == "Hidden")
        #expect(harness.store.reportMessage == "You won't see recipes from this cook again.")
    }

    @Test func aCatalogRecipeSaysWhetherItsCookCanBeHidden() throws {
        let decoder = JSONCoding.makeDecoder()
        let plain = try decoder.decode(CatalogRecipe.self, from: CatalogFixtures.detail)
        #expect(!plain.hidesCook)
        var json = try #require(JSONSerialization.jsonObject(with: CatalogFixtures.detail) as? [String: Any])
        json["canHideCook"] = true
        let shared = try decoder.decode(CatalogRecipe.self, from: JSONSerialization.data(withJSONObject: json))
        #expect(shared.hidesCook)
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

    @Test func addingMarksTheRecipeEverywhereAndTellsTheLibrary() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        let added = Counter()
        harness.store.recipeWasAdded = { _ in added.increment() }
        let item = try #require(harness.store.items.first)

        let ok = await harness.store.add(item)
        #expect(ok)
        // It stays where it was, marked, in Browse All and in every row that has it.
        #expect(harness.store.items.map(\.id) == ["c1"])
        #expect(harness.store.items.first?.inLibrary == true)
        let rows = try #require(harness.store.home?.sections)
        #expect(rows.allSatisfy { row in row.items.filter { $0.id == "c1" }.allSatisfy(\.inLibrary) })
        #expect(rows[0].items.first { $0.id == "c9" }?.inLibrary == false)
        #expect(harness.store.isSaved(item))
        #expect(added.value == 1)
        #expect(harness.server.requestedPaths.contains { $0.hasPrefix("POST") && $0.hasSuffix("/add") })
    }

    @Test func theRowsComeFromTheServer() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        let home = try #require(harness.store.home)
        #expect(home.sections.map(\.id) == ["chefs-picks", "pasta-week"])
        #expect(home.sections[0].hasMore)
        #expect(home.sections[0].subtitle == "Our favorites right now")
        #expect(home.sections[1].sponsorLabel == "Sponsored by Test Pasta")
        #expect(harness.store.filterGroups.map(\.id) == ["cuisine", "time", "servings"])
        #expect(home.browseTitle == "Browse All")
        #expect(harness.server.requestedPaths.contains { $0.hasSuffix("/discover/home") })
    }

    @Test func anEmptyRowIsNeverShownEvenAPaidOne() throws {
        let json = Data(
            #"""
            {"sections":[
              {"id":"pasta-week","title":"Pasta Week","type":"sponsored","sponsorLabel":"Sponsored by Test Pasta",
               "items":[],"hasMore":false},
              {"id":"quick","title":"Quick","type":"algorithmic","items":[\#(CatalogFixtures.summary(id: "c5", name: "Fast Bowl"))],
               "hasMore":false}],
             "filters":[]}
            """#.utf8)
        let home = try JSONCoding.makeDecoder().decode(DiscoverHome.self, from: json)
        #expect(home.sections.map(\.id) == ["quick"])
    }

    @Test func rowsThatFailKeepWhatWasShownAndFallBackToBuiltInChips() async throws {
        let harness = try await makeHarness()
        harness.server.failHome()
        await harness.store.activate(householdID: "household-1")
        #expect(harness.store.home == nil)
        #expect(harness.store.homeError != nil)
        // Browse All still works, with time and servings chips.
        #expect(harness.store.items.map(\.name) == ["Alpha Bowl"])
        #expect(harness.store.filterGroups.map(\.param) == ["maxMinutes", "minServings"])

        harness.server.failHome(false)
        await harness.store.refresh()
        #expect(harness.store.home?.sections.count == 2)
        harness.server.failHome()
        await harness.store.refresh()
        #expect(harness.store.home?.sections.count == 2, "a failed refresh emptied the rows")
    }

    @Test func filterChipsAreSentAndCleared() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        await harness.store.setFilter(param: "maxMinutes", value: "30")
        await harness.store.setFilter(param: "cuisine", value: "thai")
        await harness.store.setFilter(param: "minServings", value: "6")
        let last = try #require(harness.server.requestedPaths.last)
        #expect(last.contains("/discover?"))
        #expect(last.contains("maxMinutes=30"))
        #expect(last.contains("cuisine=thai"))
        #expect(last.contains("minServings=6"))
        #expect(harness.store.query.hasFilters)

        await harness.store.setSearch("beta")
        let searched = try #require(harness.server.requestedPaths.last)
        #expect(
            searched.contains("/catalog/recipes?") && searched.contains("q=beta") && searched.contains("maxMinutes=30"))

        await harness.store.clearFilters()
        #expect(!harness.store.query.hasFilters)
        #expect(harness.store.query.search == "beta")
        #expect(!(try #require(harness.server.requestedPaths.last)).contains("maxMinutes"))
    }

    @Test func seeAllReadsTheWholeRow() async throws {
        let harness = try await makeHarness()
        await harness.store.activate(householdID: "household-1")
        let page = try await harness.store.sectionPage(id: "quick", cursor: nil)
        #expect(page.section.title == "Quick")
        #expect(page.section.items.map(\.name) == ["Fast Bowl"])
        #expect(page.nextCursor == nil)
        #expect(try #require(harness.server.requestedPaths.last).contains("/discover/sections/quick"))
    }

    @Test func aQueryReadsAndWritesItsChips() {
        var query = CatalogQuery()
        query.setValue("45", forParam: "maxMinutes")
        query.setValue("4", forParam: "minServings")
        query.setValue("mexican", forParam: "cuisine")
        #expect(query.maxMinutes == 45 && query.minServings == 4 && query.cuisine == "mexican")
        #expect(query.value(forParam: "maxMinutes") == "45")
        #expect(!query.isSameRequest(as: CatalogQuery()))
        query.setValue(nil, forParam: "maxMinutes")
        #expect(query.maxMinutes == nil)
        let items = CatalogAPI.queryItems(query: query, includeSearch: true, cursor: nil, limit: 5)
        #expect(items.contains(URLQueryItem(name: "minServings", value: "4")))
        #expect(!items.contains { $0.name == "maxMinutes" })
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
        #expect(harness.store.home == nil)
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
