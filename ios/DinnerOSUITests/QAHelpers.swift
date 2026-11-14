import XCTest

/// Shared steps for the QA runs: sign in on the development server, wait for things, and keep a
/// named screenshot of every screen for the checklist.
extension XCTestCase {
    @MainActor
    func launchSignedIn(dark: Bool = false) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-AppleInterfaceStyle", dark ? "Dark" : "Light"]
        for (key, value) in ProcessInfo.processInfo.environment where key.hasPrefix("QA_APP_") {
            app.launchEnvironment[String(key.dropFirst("QA_APP_".count))] = value
        }
        app.launch()
        let devSignIn = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch
        if devSignIn.waitForExistence(timeout: 8) {
            devSignIn.tap()
        }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The Menu tab didn't open after sign-in")
        return app
    }

    /// Keeps a screenshot in the test results, named for the checklist.
    @MainActor
    func snap(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor
    func waitGone(_ element: XCUIElement, timeout: TimeInterval = 5) -> Bool {
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: element)
        return XCTWaiter.wait(for: [gone], timeout: timeout) == .completed
    }

    /// The keyboard is up; tapping outside the field must put it away.
    @MainActor
    func assertKeyboardDismissesOnTapOutside(_ app: XCUIApplication, tapping target: XCUIElement, _ what: String) {
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 3), "No keyboard for \(what)")
        target.tap()
        XCTAssertTrue(waitGone(app.keyboards.firstMatch), "Keyboard stayed up after tapping outside \(what)")
    }

    /// Scrolls until the element is on screen.
    @MainActor
    func reveal(_ element: XCUIElement, in app: XCUIApplication, maxSwipes: Int = 8) {
        var swipes = 0
        while !(element.exists && element.isHittable), swipes < maxSwipes {
            app.swipeUp(velocity: .slow)
            swipes += 1
        }
    }
}
