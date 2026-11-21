import XCTest

/// Flings the Menu's All Meals list up and down, through paging and while the list changes,
/// to catch a crash SwiftUI's render thread hit building a row off the main thread (build 113).
/// The app must still be running at the end.
final class MenuScrollStressQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 900
    }

    @MainActor
    func testFlingingAllMealsDoesNotCrash() {
        let app = launchSignedIn()
        app.buttons["Menu"].firstMatch.tap()
        let rounds = Int(ProcessInfo.processInfo.environment["QA_STRESS_ROUNDS"] ?? "") ?? 30
        for round in 0..<rounds {
            for _ in 0..<4 { app.swipeUp(velocity: .fast) }
            // Flick back a little, then on: lazy rows are made and dropped both ways.
            scrollBack(app)
            app.swipeUp(velocity: .fast)
            if round % 5 == 4 {
                // A pull back to the top while it's still moving.
                for _ in 0..<3 { scrollBack(app) }
            }
            XCTAssertEqual(app.state, .runningForeground, "The app stopped during round \(round)")
        }
        snap(app, "Menu after the scroll stress")
        XCTAssertTrue(app.buttons["Week Menu"].exists || app.buttons["Menu"].exists, "The Menu is gone")
    }

    /// Ten scrolls down and up the Menu with a pause after each, for a CPU sampler outside the
    /// test to check the app settles within 2 s (the iPad landscape freeze, build 122). Prints
    /// "QA-SCROLL <n> <down|up> <epoch>" when each scroll ends.
    @MainActor
    func testTenScrollsSettle() {
        let app = launchSignedIn()
        app.buttons["Menu"].firstMatch.tap()
        sleep(3)
        for round in 1...10 {
            for direction in ["down", "up"] {
                for _ in 0..<3 {
                    if direction == "down" { app.swipeUp() } else { app.swipeDown() }
                }
                print("QA-SCROLL \(round) \(direction) \(Date().timeIntervalSince1970)")
                Thread.sleep(forTimeInterval: 3)
                XCTAssertEqual(app.state, .runningForeground, "The app stopped during round \(round)")
            }
        }
    }
}
