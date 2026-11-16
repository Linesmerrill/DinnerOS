import Foundation
import StoreKit
import Synchronization
import Testing

@testable import DinnerOS

/// Build 113 review fixes: Feedback & Bugs only outside the App Store, and Search the Web in
/// Choose Photo only when the server can search.
@MainActor
struct ReviewReadinessTests {
    // MARK: Feedback & Bugs

    @Test func theAppStoreHidesBetaFeedback() {
        let store = BuildChannel.from(environment: .production, receiptName: "receipt", debug: false)
        #expect(store == .appStore && !store.showsBetaFeedback)
    }

    @Test func testFlightAndXcodeShowBetaFeedback() {
        #expect(BuildChannel.from(environment: .sandbox, receiptName: "sandboxReceipt", debug: false) == .testFlight)
        #expect(BuildChannel.from(environment: .xcode, receiptName: nil, debug: false) == .development)
        #expect(BuildChannel.from(environment: .production, receiptName: "receipt", debug: true) == .development)
        #expect(BuildChannel.testFlight.showsBetaFeedback && BuildChannel.development.showsBetaFeedback)
    }

    @Test func withoutStoreKitTheReceiptDecidesAndUnknownMeansTheAppStore() {
        #expect(BuildChannel.from(environment: nil, receiptName: "sandboxReceipt", debug: false) == .testFlight)
        #expect(BuildChannel.from(environment: nil, receiptName: "receipt", debug: false) == .appStore)
        #expect(BuildChannel.from(environment: nil, receiptName: nil, debug: false) == .appStore)
    }

    // MARK: Photo search

    private let defaults: UserDefaults

    init() throws {
        let suite = "ReviewReadinessTests.\(UUID().uuidString)"
        defaults = try #require(UserDefaults(suiteName: suite))
        defaults.removePersistentDomain(forName: suite)
    }

    private func makeLibrary(_ handler: @escaping StubTransport.Handler) async throws -> (RecipeLibrary, StubTransport)
    {
        let transport = StubTransport(handler)
        let client = APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport)
        let stored = StoredSession(tokens: Fixtures.tokens(), user: Fixtures.user)
        let session = AuthSession(api: AuthAPI(client: client), store: InMemoryTokenStore(session: stored))
        await session.restore()
        let library = RecipeLibrary(session: session, api: RecipesAPI(client: client), defaults: defaults)
        await library.activate(householdID: "household-1")
        return (library, transport)
    }

    nonisolated private static func isStatus(_ request: URLRequest) -> Bool {
        request.url?.path() == "/api/v1/households/household-1/recipe-photos/search-status"
    }

    @Test func photoSearchIsOffUntilTheServerSaysItsOn() async throws {
        let available = Mutex(true)
        let (library, _) = try await makeLibrary { request in
            if Self.isStatus(request) {
                return (200, Data(#"{"available":\#(available.withLock { $0 })}"#.utf8))
            }
            return (200, RecipeFixtures.page([], nextCursor: nil))
        }
        #expect(!library.photoSearchAvailable)
        await library.loadPhotoSearchStatus()
        #expect(library.photoSearchAvailable)
        available.withLock { $0 = false }
        await library.loadPhotoSearchStatus()
        #expect(!library.photoSearchAvailable)
    }

    @Test func offlineKeepsTheLastAnswer() async throws {
        let offline = Mutex(false)
        let (library, _) = try await makeLibrary { request in
            if Self.isStatus(request) {
                if offline.withLock({ $0 }) { throw URLError(.notConnectedToInternet) }
                return (200, Data(#"{"available":true}"#.utf8))
            }
            return (200, RecipeFixtures.page([], nextCursor: nil))
        }
        await library.loadPhotoSearchStatus()
        offline.withLock { $0 = true }
        await library.loadPhotoSearchStatus()
        #expect(library.photoSearchAvailable)
        // A new launch starts from the saved answer.
        let (again, _) = try await makeLibrary { _ in throw URLError(.notConnectedToInternet) }
        #expect(again.photoSearchAvailable)
    }

    @Test func anOlderAPIWithoutTheRouteHidesSearch() async throws {
        defaults.set(true, forKey: RecipeLibrary.photoSearchKey)
        let (library, _) = try await makeLibrary { request in
            if Self.isStatus(request) { return (404, Fixtures.errorJSON(code: "not_found")) }
            return (200, RecipeFixtures.page([], nextCursor: nil))
        }
        await library.loadPhotoSearchStatus()
        #expect(!library.photoSearchAvailable)
    }

    @Test func aSearchTurnedDownAsOffHidesSearch() async throws {
        let (library, _) = try await makeLibrary { request in
            if Self.isStatus(request) { return (200, Data(#"{"available":true}"#.utf8)) }
            if request.url?.path().hasSuffix("/recipe-photos/search") == true {
                return (503, Fixtures.errorJSON(code: "photo_search_off"))
            }
            return (200, RecipeFixtures.page([], nextCursor: nil))
        }
        await library.loadPhotoSearchStatus()
        await #expect(throws: (any Error).self) { _ = try await library.searchPhotos("brownies", page: 1) }
        #expect(!library.photoSearchAvailable)
    }

    @Test func theChooserShowsOnlyTheLibraryWhenSearchIsOff() {
        let off = RecipePhotoChooser(
            initialQuery: "Tacos", searchAvailable: false,
            search: { _, _ in RecipePhotoSearchPage(photos: [], nextPage: nil) },
            onPicked: { _ in }, onFound: { _ in })
        #expect(off.shownPlace == .library)
        let on = RecipePhotoChooser(
            initialQuery: "Tacos", searchAvailable: true,
            search: { _, _ in RecipePhotoSearchPage(photos: [], nextPage: nil) },
            onPicked: { _ in }, onFound: { _ in })
        #expect(on.shownPlace == .web)
    }
}
