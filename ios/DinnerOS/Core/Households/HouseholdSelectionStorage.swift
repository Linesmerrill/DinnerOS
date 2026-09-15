import Foundation

/// Remembers which household each user last selected on this device.
///
/// A household ID isn't a secret, so it lives in `UserDefaults`, not the Keychain. It's
/// keyed by user so a different account on the same device starts fresh.
protocol HouseholdSelectionStorage: AnyObject {
    func selectedHouseholdID(for userID: String) -> String?
    func setSelectedHouseholdID(_ householdID: String?, for userID: String)
}

final class UserDefaultsHouseholdSelection: HouseholdSelectionStorage {
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func selectedHouseholdID(for userID: String) -> String? {
        defaults.string(forKey: Self.key(for: userID))
    }

    func setSelectedHouseholdID(_ householdID: String?, for userID: String) {
        if let householdID {
            defaults.set(householdID, forKey: Self.key(for: userID))
        } else {
            defaults.removeObject(forKey: Self.key(for: userID))
        }
    }

    static func key(for userID: String) -> String {
        "selectedHouseholdID.\(userID)"
    }
}

/// A non-persistent selection for tests and previews.
final class InMemoryHouseholdSelection: HouseholdSelectionStorage {
    private(set) var selections: [String: String]

    init(selections: [String: String] = [:]) {
        self.selections = selections
    }

    func selectedHouseholdID(for userID: String) -> String? {
        selections[userID]
    }

    func setSelectedHouseholdID(_ householdID: String?, for userID: String) {
        selections[userID] = householdID
    }
}
