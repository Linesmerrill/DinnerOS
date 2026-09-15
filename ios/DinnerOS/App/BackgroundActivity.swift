import UIKit

/// Asks the system for time to finish short work after the app leaves the foreground.
enum BackgroundActivity {
    /// Runs `work`, holding a background task assertion until it finishes or the system
    /// takes the time back. Work cut short must be safe to repeat later.
    static func run(named name: String, _ work: @escaping @MainActor () async -> Void) {
        let assertion = Assertion()
        assertion.identifier = UIApplication.shared.beginBackgroundTask(withName: name) {
            assertion.end()
        }
        Task {
            await work()
            assertion.end()
        }
    }

    private final class Assertion {
        var identifier = UIBackgroundTaskIdentifier.invalid

        func end() {
            guard identifier != .invalid else { return }
            UIApplication.shared.endBackgroundTask(identifier)
            identifier = .invalid
        }
    }
}
