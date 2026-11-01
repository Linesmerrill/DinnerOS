import CoreLocation
import Foundation
import MapKit

/// A grocery store near the household, from Apple Maps.
nonisolated struct NearbyGroceryStore: Hashable, Sendable, Identifiable {
    let name: String
    let address: String
    /// Meters from the household's ZIP code, when Maps placed it.
    let distance: CLLocationDistance?

    var id: String { "\(name)|\(address)" }

    /// "5.2 mi" where miles are used, "8.4 km" elsewhere; never feet.
    var distanceText: String? {
        distanceText(locale: .current)
    }

    func distanceText(locale: Locale) -> String? {
        guard let distance else { return nil }
        let unit: UnitLength = locale.measurementSystem == .metric ? .kilometers : .miles
        let value = Measurement(value: distance, unit: UnitLength.meters).converted(to: unit).value
        let number = max(value, 0.1).formatted(.number.precision(.fractionLength(1)).locale(locale))
        return unit == .miles ? String(localized: "\(number) mi") : String(localized: "\(number) km")
    }
}

/// Grocery stores near a ZIP code, closest first. `MapKitNearbyGroceries` is the real one;
/// tests use a fake.
@MainActor
protocol NearbyGroceryFinding {
    /// Stores near `zip`. An empty `query` lists every grocery store nearby; otherwise only
    /// those Maps finds for it.
    func stores(near zip: String, matching query: String) async throws -> [NearbyGroceryStore]
}

/// Apple Maps' grocery stores around a ZIP code. No API key; only the ZIP code and what the
/// member typed go to Apple.
@MainActor
final class MapKitNearbyGroceries: NearbyGroceryFinding {
    /// How far around the ZIP code to look: about 10 miles.
    private let radius: CLLocationDistance = 16_000
    private var centers: [String: CLLocation] = [:]

    func stores(near zip: String, matching query: String) async throws -> [NearbyGroceryStore] {
        let center = try await center(of: zip)
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        // Before typing, ask for grocery stores and supermarkets by name: the plain category
        // lookup returns the nearest market stalls and delis instead.
        let queries = trimmed.isEmpty ? ["grocery store", "supermarket"] : [trimmed]
        var items: [MKMapItem] = []
        for text in queries {
            items += try await search(text, around: center)
        }
        let stores = items.compactMap { item -> NearbyGroceryStore? in
            guard let name = item.name, !name.isEmpty else { return nil }
            let location = Self.location(of: item)
            return NearbyGroceryStore(
                name: name, address: Self.address(of: item), distance: location.map { center.distance(from: $0) })
        }
        return NearbyGroceries.sorted(stores)
    }

    private func search(_ text: String, around center: CLLocation) async throws -> [MKMapItem] {
        let request = MKLocalSearch.Request()
        request.naturalLanguageQuery = text
        request.resultTypes = .pointOfInterest
        request.pointOfInterestFilter = MKPointOfInterestFilter(including: [.foodMarket])
        request.region = MKCoordinateRegion(
            center: center.coordinate, latitudinalMeters: radius * 2, longitudinalMeters: radius * 2)
        do {
            return try await MKLocalSearch(request: request).start().mapItems
        } catch let error as MKError where error.code == .placemarkNotFound {
            // Maps answers "nothing here" with an error; it's an empty list.
            return []
        }
    }

    private func center(of zip: String) async throws -> CLLocation {
        if let cached = centers[zip] { return cached }
        let found: CLLocation?
        if #available(iOS 26.0, *) {
            let request = MKGeocodingRequest(addressString: "\(zip), United States")
            found = try await request?.mapItems.first.map { $0.location }
        } else {
            found = try await CLGeocoder().geocodeAddressString("\(zip), United States").first?.location
        }
        guard let found else { throw ZIPLocatorError.unavailable }
        centers[zip] = found
        return found
    }

    private static func location(of item: MKMapItem) -> CLLocation? {
        if #available(iOS 26.0, *) {
            return item.location
        }
        return item.placemark.location
    }

    private static func address(of item: MKMapItem) -> String {
        if #available(iOS 26.0, *) {
            return item.address?.shortAddress ?? item.address?.fullAddress ?? ""
        }
        let placemark = item.placemark
        let street = [placemark.subThoroughfare, placemark.thoroughfare].compactMap { $0 }.joined(separator: " ")
        return [street, placemark.locality ?? ""].filter { !$0.isEmpty }.joined(separator: ", ")
    }
}

/// Sorting and matching nearby stores against the catalog. The API makes the final call on
/// which chain a store belongs to; this is what the list shows meanwhile.
enum NearbyGroceries {
    /// Closest first; stores Maps didn't place go last, by name. Duplicates (same name and
    /// address) appear once.
    static func sorted(_ stores: [NearbyGroceryStore]) -> [NearbyGroceryStore] {
        var seen = Set<String>()
        return
            stores
            .filter { seen.insert($0.id).inserted }
            .sorted {
                switch ($0.distance, $1.distance) {
                case let (a?, b?): a == b ? $0.name < $1.name : a < b
                case (.some, nil): true
                case (nil, .some): false
                case (nil, nil): $0.name < $1.name
                }
            }
    }

    /// The catalog entry a store belongs to: the longest catalog name or alias the store's
    /// name starts with, word for word. "Fry's Food Store #622" is Fry's.
    static func catalogItem(for name: String, in catalog: [ShoppingCatalogItem]) -> ShoppingCatalogItem? {
        let chain = StoreNameText.normalized(StoreNameText.chain(name)) + " "
        var best: (item: ShoppingCatalogItem, length: Int)?
        for item in catalog {
            for candidate in [item.name, item.key] + item.aliases {
                let alias = StoreNameText.normalized(candidate)
                guard !alias.isEmpty, chain.hasPrefix(alias + " ") else { continue }
                if alias.count > best?.length ?? 0 { best = (item, alias.count) }
            }
        }
        return best?.item
    }

    /// The key the API files a store under when the catalog doesn't list it.
    static func requestKey(for name: String) -> String {
        StoreNameText.normalized(StoreNameText.chain(name)).replacingOccurrences(of: " ", with: "-")
    }
}

/// The API's store-name folding (`normalizeStoreText` and `chainName` in
/// api/internal/shopping/catalog.go), so the app groups stores the same way.
enum StoreNameText {
    /// Lower case, "&" as "and", apostrophes and periods dropped, other punctuation a space.
    static func normalized(_ text: String) -> String {
        var out = ""
        var space = false
        for character in text.lowercased() {
            switch character {
            case "'", "’", ".":
                continue
            case "&":
                if space, !out.isEmpty { out.append(" ") }
                out.append("and")
                space = true
            case _ where character.isLetter || character.isNumber:
                if space, !out.isEmpty { out.append(" ") }
                out.append(character)
                space = false
            default:
                space = !out.isEmpty
            }
        }
        return out.trimmingCharacters(in: .whitespaces)
    }

    /// The chain part of one store's name: no store number, nothing after " - ", " | ", "(",
    /// or ",".
    static func chain(_ name: String) -> String {
        var text = name.trimmingCharacters(in: .whitespaces)
        for separator in [" - ", " – ", " | ", "(", ","] {
            if let range = text.range(of: separator), range.lowerBound > text.startIndex {
                text = String(text[..<range.lowerBound])
            }
        }
        var words = text.split(separator: " ").map(String.init)
        let count = words.count
        func isDigits(_ s: String) -> Bool { !s.isEmpty && s.allSatisfy { $0.isASCII && $0.isNumber } }
        if count > 1, words[count - 1].hasPrefix("#"), isDigits(String(words[count - 1].dropFirst())) {
            words.removeLast()
        } else if count > 2, words[count - 2] == "#", isDigits(words[count - 1]) {
            words.removeLast(2)
        } else if count > 2, isDigits(words[count - 1]) {
            let word = words[count - 2].lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "."))
            if word == "store" || word == "no" { words.removeLast(2) }
        }
        return words.joined(separator: " ")
    }
}
