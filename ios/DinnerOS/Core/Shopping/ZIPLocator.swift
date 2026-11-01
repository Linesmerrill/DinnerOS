import CoreLocation
import Foundation
import MapKit

/// Why the store finder couldn't fill in a ZIP code from where the member is.
enum ZIPLocatorError: Error, Equatable {
    /// Location is off for the app (or for the whole phone). Only Settings can change it.
    case denied
    /// No fix, or no ZIP code for the place, in time.
    case unavailable
}

/// The ZIP code where the member is, for the store finders. `CoreLocationZIPLocator` is the
/// real one; tests use a fake.
@MainActor
protocol ZIPLocating {
    func currentZIP() async throws -> String
}

/// Asks for When In Use location, takes one fix, and turns it into a 5-digit ZIP code. The
/// coordinates stay on the phone; only the ZIP code is sent, to search for stores.
@MainActor
final class CoreLocationZIPLocator: ZIPLocating {
    private let timeout: Duration

    init(timeout: Duration = .seconds(15)) {
        self.timeout = timeout
    }

    func currentZIP() async throws -> String {
        let location = try await withTimeout { try await Self.oneLocation() }
        guard let zip = try await withTimeout({ try await Self.zip(at: location) }) else {
            throw ZIPLocatorError.unavailable
        }
        return zip
    }

    private static func oneLocation() async throws -> CLLocation {
        // Holding a session asks for When In Use access the first time.
        let session = CLServiceSession(authorization: .whenInUse)
        defer { session.invalidate() }
        for try await update in CLLocationUpdate.liveUpdates() {
            if update.authorizationDenied || update.authorizationDeniedGlobally || update.authorizationRestricted {
                throw ZIPLocatorError.denied
            }
            if let location = update.location {
                return location
            }
        }
        throw ZIPLocatorError.unavailable
    }

    private static func zip(at location: CLLocation) async throws -> String? {
        if #available(iOS 26.0, *) {
            guard let request = MKReverseGeocodingRequest(location: location) else { return nil }
            let items = try await request.mapItems
            return items.lazy.compactMap { ZIPCode.find(in: $0.address?.fullAddress) }.first
        }
        let placemarks = try await CLGeocoder().reverseGeocodeLocation(location)
        return placemarks.lazy.compactMap { ZIPCode.normalized($0.postalCode) }.first
    }

    private func withTimeout<T: Sendable>(_ operation: @escaping @Sendable () async throws -> T) async throws -> T {
        let timeout = timeout
        return try await withThrowingTaskGroup(of: T.self) { group in
            group.addTask { try await operation() }
            group.addTask {
                try await Task.sleep(for: timeout)
                throw ZIPLocatorError.unavailable
            }
            defer { group.cancelAll() }
            guard let first = try await group.next() else { throw ZIPLocatorError.unavailable }
            return first
        }
    }
}

/// US ZIP codes as the store finders take them: five digits.
enum ZIPCode {
    /// `text` as a 5-digit ZIP code, dropping a ZIP+4 suffix; `nil` when it isn't one.
    static func normalized(_ text: String?) -> String? {
        guard let text else { return nil }
        let five = String(text.trimmingCharacters(in: .whitespaces).prefix(5))
        return isValid(five) ? five : nil
    }

    static func isValid(_ text: String) -> Bool {
        text.count == 5 && text.allSatisfy { $0.isASCII && $0.isNumber }
    }

    /// The last 5-digit ZIP code in a written address ("…Phoenix, AZ 85004-1234, United States").
    static func find(in address: String?) -> String? {
        guard let address else { return nil }
        let matches = address.matches(of: /(?:^|[^0-9])([0-9]{5})(?:-[0-9]{4})?(?![0-9])/)
        return matches.last.map { String($0.output.1) }
    }

    /// Keeps only digits, at most five, as the member types.
    static func filtered(_ text: String) -> String {
        String(text.filter { $0.isASCII && $0.isNumber }.prefix(5))
    }
}
