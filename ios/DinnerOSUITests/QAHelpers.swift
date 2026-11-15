import XCTest

/// Shared steps for the QA runs: sign in on the development server, wait for things, and keep a
/// named screenshot of every screen for the checklist.
extension XCTestCase {
    @MainActor
    func launchSignedIn(dark: Bool = false) -> XCUIApplication {
        // Light or dark comes from the simulator: run `xcrun simctl ui <device> appearance dark`
        // before a dark run. Neither a launch argument nor XCUIDevice.appearance took effect on
        // iOS 26.2, and the device setting could stick until a reboot.
        _ = dark
        let app = XCUIApplication()
        applyOrientation()
        for (key, value) in ProcessInfo.processInfo.environment where key.hasPrefix("QA_APP_") {
            app.launchEnvironment[String(key.dropFirst("QA_APP_".count))] = value
        }
        app.launch()
        let devSignIn = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch
        if devSignIn.waitForExistence(timeout: 8) {
            devSignIn.tap()
        }
        // Shown once after a fresh install to a household with Plus on the house.
        if app.buttons["Thanks"].waitForExistence(timeout: 4) { app.buttons["Thanks"].tap() }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The Menu tab didn't open after sign-in")
        return app
    }

    /// Portrait unless the run sets QA_ORIENTATION=landscape (the kitchen iPad lives on a
    /// stand in landscape).
    @MainActor
    func applyOrientation() {
        let landscape = ProcessInfo.processInfo.environment["QA_ORIENTATION"] == "landscape"
        XCUIDevice.shared.orientation = landscape ? .landscapeLeft : .portrait
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
        // Hittable isn't enough: a pinned bottom bar (Open in Walmart, Add to Week) can sit over
        // an element XCTest still calls hittable. Keep it out of the bottom fifth of the screen.
        func clear() -> Bool {
            guard element.exists, element.isHittable else { return false }
            let screen = app.frame
            return element.frame.maxY < screen.minY + screen.height * 0.8
        }
        while !clear(), swipes < maxSwipes {
            app.swipeUp(velocity: .slow)
            swipes += 1
        }
    }
}
