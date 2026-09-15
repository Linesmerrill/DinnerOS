import Foundation

/// Remembers which households already saw the Autopilot onboarding prompt on this device,
/// so opening the Week tab offers it once rather than every time.
///
/// Keyed by user and household. Not a secret, so it lives in `UserDefaults`.
protocol AutopilotPromptStorage: AnyObject {
    func hasPromptedOnboarding(userID: String, householdID: String) -> Bool
    func setPromptedOnboarding(userID: String, householdID: String)
}

final class UserDefaultsAutopilotPrompts: AutopilotPromptStorage {
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func hasPromptedOnboarding(userID: String, householdID: String) -> Bool {
        defaults.bool(forKey: Self.key(userID: userID, householdID: householdID))
    }

    func setPromptedOnboarding(userID: String, householdID: String) {
        defaults.set(true, forKey: Self.key(userID: userID, householdID: householdID))
    }

    static func key(userID: String, householdID: String) -> String {
        "autopilotOnboardingPrompted.\(userID).\(householdID)"
    }
}

/// A non-persistent record for tests and previews.
final class InMemoryAutopilotPrompts: AutopilotPromptStorage {
    private(set) var prompted: Set<String> = []

    func hasPromptedOnboarding(userID: String, householdID: String) -> Bool {
        prompted.contains("\(userID).\(householdID)")
    }

    func setPromptedOnboarding(userID: String, householdID: String) {
        prompted.insert("\(userID).\(householdID)")
    }
}
