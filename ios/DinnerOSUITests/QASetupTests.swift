import XCTest

/// One-time simulator setup before a QA run. iPadOS 26 can switch a simulator to windowed apps
/// (a test's downward swipe once did, build 121), after which the app runs in a small window for
/// every later test. This puts Settings › Multitasking & Gestures back on Full Screen Apps.
final class QASetupTests: XCTestCase {
    @MainActor
    func testFullScreenApps() throws {
        guard UIDevice.current.userInterfaceIdiom == .pad else { return }
        let settings = XCUIApplication(bundleIdentifier: "com.apple.Preferences")
        settings.launch()
        let row = settings.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Multitasking'")).firstMatch
        for _ in 0..<8 where !row.exists {
            settings.tables.firstMatch.exists ? settings.tables.firstMatch.swipeUp() : settings.swipeUp()
        }
        guard row.waitForExistence(timeout: 5) else {
            print("QA-SETUP tree:\n\(settings.debugDescription.prefix(6000))")
            XCTFail("No Multitasking row in Settings")
            return
        }
        row.tap()
        sleep(2)
        let fullScreen = settings.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH 'Full Screen'")).firstMatch
        if fullScreen.waitForExistence(timeout: 5) {
            fullScreen.tap()
            sleep(1)
        } else {
            XCTFail("No Full Screen Apps choice")
        }
        settings.terminate()
    }
}
