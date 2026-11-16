import Foundation
import StoreKit
import SwiftUI
import Synchronization
import Testing
import UIKit

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

    // MARK: Unsplash credits (decision 705)

    @Test func aPickCarriesItsAddressAndCredit() throws {
        let json = #"""
            {"imageUrl":"https://images.unsplash.com/photo-1?ixid=a&w=1080",
             "credit":{"name":"Ana Lima","profileUrl":"https://unsplash.com/@ana?utm_medium=referral&utm_source=dinneros",
                       "sourceName":"Unsplash","sourceUrl":"https://unsplash.com/?utm_medium=referral&utm_source=dinneros"}}
            """#
        let pick = try JSONCoding.makeDecoder().decode(RecipePhotoPick.self, from: Data(json.utf8))
        #expect(pick.imageURL == "https://images.unsplash.com/photo-1?ixid=a&w=1080")
        let credit = try #require(pick.credit)
        #expect(credit.text == "Photo by Ana Lima on Unsplash")
        #expect(String(credit.attributed.characters) == "Photo by Ana Lima on Unsplash")
        let links = credit.attributed.runs.compactMap(\.link)
        #expect(
            links.map(\.absoluteString) == [
                "https://unsplash.com/@ana?utm_medium=referral&utm_source=dinneros",
                "https://unsplash.com/?utm_medium=referral&utm_source=dinneros",
            ])
    }

    @Test func aSearchPageNamesItsSourceAndEachPhotographer() throws {
        let json = #"""
            {"photos":[{"id":"abc","thumbnailUrl":"https://images.unsplash.com/photo-1?w=400","credit":"Ana",
                        "creditUrl":"https://unsplash.com/@ana?utm_source=dinneros&utm_medium=referral"}],
             "nextPage":2,"sourceName":"Unsplash","sourceUrl":"https://unsplash.com/?utm_source=dinneros&utm_medium=referral"}
            """#
        let page = try JSONCoding.makeDecoder().decode(RecipePhotoSearchPage.self, from: Data(json.utf8))
        #expect(page.sourceName == "Unsplash" && page.nextPage == 2)
        let credit = try #require(page.photos[0].photoCredit(source: page.sourceName, sourceURL: page.sourceURL))
        #expect(credit.text == "Photo by Ana on Unsplash")
        #expect(credit.profileURL?.absoluteString.contains("utm_source=dinneros") == true)
        // An older API's page has no source, so no credit line is made up.
        #expect(page.photos[0].photoCredit(source: nil, sourceURL: nil) == nil)
    }

    @Test func aPickedPhotoKeepsItsCreditInTheFormAndLosesItWhenRemoved() async throws {
        let model = RecipePhotoModel()
        var url: String?
        let binding = Binding(get: { url }, set: { url = $0 })
        let credit = PhotoCredit(name: "Ana", profileURL: nil, sourceName: "Unsplash", sourceURL: nil)
        let source = RecipePhotoSource(
            upload: { _ in "https://api.test/api/v1/recipe-photos/0123456789abcdef0123456789abcdef" },
            search: { _, _ in RecipePhotoSearchPage(photos: [], nextPage: nil) },
            useFound: { _ in RecipePhotoPick(imageURL: "https://images.unsplash.com/photo-1", credit: credit) })
        await model.use(RecipeFoundPhoto(id: "abc", thumbnailURL: "x"), to: binding, source: source)
        #expect(url == "https://images.unsplash.com/photo-1" && model.credit == credit)
        model.remove(binding)
        #expect(url == nil && model.credit == nil)
        // A photo of their own has no credit.
        await model.send(UIImage(systemName: "star")!.withTintColor(.red), to: binding, source: source)
        #expect(model.credit == nil)
    }

    @Test func aRecipeDecodesItsPhotoCredit() throws {
        var json = String(decoding: RecipeFixtures.detail(), as: UTF8.self)
        json = json.replacingOccurrences(
            of: #""isAddon": false"#,
            with:
                #""imageCredit":{"name":"Ana","profileUrl":"https://unsplash.com/@ana","sourceName":"Unsplash","sourceUrl":"https://unsplash.com/"},"isAddon": false"#
        )
        let recipe = try JSONCoding.makeDecoder().decode(Recipe.self, from: Data(json.utf8))
        #expect(recipe.imageCredit?.text == "Photo by Ana on Unsplash")
    }
}
