import Foundation
import Testing

@testable import DinnerOS

/// The store finder: ZIP codes typed or filled in from location, the household's saved ZIP
/// code, and the list folding down to the picked store.
@MainActor
struct StoreFinderTests {
    private final class FakeLocator: ZIPLocating {
        var result: Result<String, ZIPLocatorError>
        init(_ result: Result<String, ZIPLocatorError>) { self.result = result }
        func currentZIP() async throws -> String { try result.get() }
    }

    private struct Failure: Error {}

    private let phoenix = ShoppingStoreLocation(storeID: "66000123", name: "Fry's Test Store", address: "1 Test St")

    @Test func zipCodesAreFiveDigits() {
        #expect(ZIPCode.isValid("85004"))
        #expect(!ZIPCode.isValid("8500") && !ZIPCode.isValid("850041") && !ZIPCode.isValid("8500a"))
        #expect(ZIPCode.normalized("85004-1234") == "85004")
        #expect(ZIPCode.normalized(" 85004 ") == "85004")
        #expect(ZIPCode.normalized("V6B 1A1") == nil && ZIPCode.normalized(nil) == nil)
        #expect(ZIPCode.filtered("85a0-04 12") == "85004")
    }

    @Test func findsTheZIPCodeInAWrittenAddress() {
        #expect(ZIPCode.find(in: "1 Test St, Phoenix, AZ 85004-1234, United States") == "85004")
        #expect(ZIPCode.find(in: "12345 Test Rd, Phoenix, AZ 85004") == "85004")
        #expect(ZIPCode.find(in: "85004") == "85004")
        #expect(ZIPCode.find(in: "Unit 123456, Somewhere") == nil)
        #expect(ZIPCode.find(in: nil) == nil)
    }

    @Test func typingKeepsOnlyFiveDigits() {
        let finder = StoreFinderState()
        finder.zip = "85004999"
        #expect(finder.zip == "85004" && finder.isValidZIP)
        finder.zip = "8a5"
        #expect(finder.zip == "85" && !finder.isValidZIP)
    }

    @Test func startsWithTheSavedZIPAndFoldsToASavedStore() {
        let fresh = StoreFinderState()
        fresh.start(savedZIP: "85004", hasStore: false)
        #expect(fresh.zip == "85004" && fresh.isExpanded)

        let saved = StoreFinderState()
        saved.start(savedZIP: "85004", hasStore: true)
        #expect(!saved.isExpanded)

        let typed = StoreFinderState()
        typed.zip = "10001"
        typed.start(savedZIP: "85004", hasStore: false)
        #expect(typed.zip == "10001")

        let none = StoreFinderState()
        none.start(savedZIP: "bad", hasStore: false)
        #expect(none.zip.isEmpty)
    }

    @Test func aSearchThatFindsStoresSavesTheZIPAndPickingFoldsTheList() async {
        let finder = StoreFinderState()
        finder.zip = "85004"
        var remembered: [String] = []
        await finder.find(
            using: { zip in
                #expect(zip == "85004")
                return [phoenix]
            }, remember: { remembered.append($0) }, storeName: "Fry's", message: { _ in "x" })
        #expect(finder.stores == [phoenix] && finder.isExpanded && finder.hasSearched)
        #expect(remembered == ["85004"] && finder.errorMessage == nil)

        finder.didPick()
        #expect(!finder.isExpanded)
    }

    @Test func noStoresOrAnErrorSavesNothing() async {
        let finder = StoreFinderState()
        finder.zip = "99999"
        var remembered: [String] = []
        await finder.find(
            using: { _ in [] }, remember: { remembered.append($0) }, storeName: "Fry's", message: { _ in "x" })
        #expect(finder.errorMessage == "No Fry's stores near 99999.")
        await finder.find(
            using: { _ in throw Failure() }, remember: { remembered.append($0) }, storeName: "Fry's",
            message: { _ in "Couldn't reach the store." })
        #expect(finder.errorMessage == "Couldn't reach the store.")
        #expect(remembered.isEmpty)
    }

    @Test func anIncompleteZIPDoesNotSearch() async {
        let finder = StoreFinderState()
        finder.zip = "850"
        var searched = false
        await finder.find(
            using: { _ in
                searched = true
                return []
            }, remember: { _ in }, storeName: "Fry's", message: { _ in "x" })
        #expect(!searched && !finder.hasSearched)
    }

    @Test func locationFillsInTheZIP() async {
        let finder = StoreFinderState()
        #expect(await finder.fillZIP(with: FakeLocator(.success("85004"))))
        #expect(finder.zip == "85004" && finder.errorMessage == nil && !finder.isLocating)
    }

    @Test func deniedLocationOffersSettingsAndTypingStillWorks() async {
        let finder = StoreFinderState()
        finder.zip = "850"
        #expect(!(await finder.fillZIP(with: FakeLocator(.failure(.denied)))))
        #expect(finder.locationDenied && finder.zip == "850")
        #expect(finder.errorMessage == "Location is off for this app. Turn it on in Settings, or type your ZIP code.")

        #expect(!(await finder.fillZIP(with: FakeLocator(.failure(.unavailable)))))
        #expect(!finder.locationDenied && finder.errorMessage == "Couldn't find your ZIP code. Type it instead.")

        finder.zip = "85004"
        await finder.find(using: { _ in [phoenix] }, remember: { _ in }, storeName: "Fry's", message: { _ in "x" })
        #expect(finder.errorMessage == nil && finder.stores.count == 1)
    }

    @Test func walmartsStoreFinderOpensAtTheZIP() {
        #expect(
            WalmartStoreFinder.url(zip: "85004")?.absoluteString
                == "https://www.walmart.com/store-finder?location=85004")
        #expect(WalmartStoreFinder.url(zip: "850") == nil)
    }

    @Test func theHouseholdCarriesItsZIP() throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let json = #"""
            {"id":"h1","name":"Home","defaultServings":2,"timeZone":"America/Phoenix","orderDay":null,"zip":"85004",
             "createdBy":"u1","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}
            """#
        #expect(try decoder.decode(Household.self, from: Data(json.utf8)).zip == "85004")
        let older = json.replacingOccurrences(of: #""zip":"85004","#, with: "")
        #expect(try decoder.decode(Household.self, from: Data(older.utf8)).zip == nil)
    }

    @Test func savingTheZIPSendsItOrNull() throws {
        let set =
            try JSONSerialization.jsonObject(with: JSONEncoder().encode(HouseholdZIPBody(zip: "85004")))
            as? [String: Any]
        #expect(set?["zip"] as? String == "85004")
        let cleared = String(decoding: try JSONEncoder().encode(HouseholdZIPBody(zip: nil)), as: UTF8.self)
        #expect(cleared == #"{"zip":null}"#)
    }
}
