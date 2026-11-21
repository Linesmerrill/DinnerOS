import CoreLocation
import Testing

@testable import DinnerOS

/// Location permission is kept from CoreLocation's reports, never asked for on demand: asking
/// blocked launch on a freshly booted simulator (build 122).
struct LocationPermissionTests {
    @Test func beforeAnyReportItIsUnknownAndNotAskedYet() {
        let permission = LocationPermission()
        #expect(!permission.isKnown)
        #expect(permission.authorization == .notDetermined)
    }

    @Test func aReportIsKeptAndSaysWhetherItChanged() {
        var permission = LocationPermission()
        let first = permission.report(.notDetermined)
        #expect(first)
        #expect(permission.isKnown)
        let same = permission.report(.notDetermined)
        #expect(!same)
        let granted = permission.report(.granted)
        #expect(granted)
        #expect(permission.authorization == .granted)
        let denied = permission.report(.denied)
        #expect(denied)
        #expect(permission.authorization == .denied)
    }

    @Test func coreLocationStatusesMapToTheAppsTerms() {
        #expect(LocationPermission.authorization(for: .notDetermined) == .notDetermined)
        #expect(LocationPermission.authorization(for: .authorizedWhenInUse) == .granted)
        #expect(LocationPermission.authorization(for: .authorizedAlways) == .granted)
        #expect(LocationPermission.authorization(for: .denied) == .denied)
        #expect(LocationPermission.authorization(for: .restricted) == .denied)
    }
}
