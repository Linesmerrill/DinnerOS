import Foundation

/// The one-time welcome a new member sees on the Menu tab: what the app does and a few calm
/// ways to get recipes in, instead of a setup wizard. Shown once per member and household,
/// and only to someone who signed up recently: a member who has used the app for weeks
/// doesn't need it after an update.
nonisolated enum Welcome {
    /// How recently an account must have been made for its member to be welcomed.
    static let newAccountWindow: TimeInterval = 14 * 24 * 3600

    static func shouldShow(accountCreated: Date, seen: Bool, now: Date = .now) -> Bool {
        !seen && now.timeIntervalSince(accountCreated) < newAccountWindow
    }

    /// What the member picked on the welcome.
    enum Choice: Equatable, Sendable {
        case importMealKit
        case browseShared
        case addRecipe
        case startPlanning
    }
}

/// Where "saw the welcome" is remembered. Injectable so tests needn't touch the device's own
/// defaults.
protocol WelcomeStorage: AnyObject {
    func hasSeen(userID: String, householdID: String) -> Bool
    func markSeen(userID: String, householdID: String)
}

final class UserDefaultsWelcomeStorage: WelcomeStorage {
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func hasSeen(userID: String, householdID: String) -> Bool {
        defaults.bool(forKey: Self.key(userID: userID, householdID: householdID))
    }

    func markSeen(userID: String, householdID: String) {
        defaults.set(true, forKey: Self.key(userID: userID, householdID: householdID))
    }

    static func key(userID: String, householdID: String) -> String {
        "welcomeSeen.\(userID).\(householdID)"
    }
}
