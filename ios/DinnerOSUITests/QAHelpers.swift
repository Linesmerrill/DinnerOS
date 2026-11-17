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
        // an element XCTest still calls hittable. On an SE, Open in Walmart, its note and the tab
        // bar cover the bottom third, so keep it above that.
        // Judged by its top edge, so a tall section (a checklist) counts once its top is in the
        // upper half.
        func clear() -> Bool {
            // Judged by the frame, not isHittable: asking about a line of text in an iPad sheet
            // can fail the test outright ("Activation point invalid"). The top-half rule below
            // already keeps it clear of a pinned bottom bar.
            let screen = app.frame
            let frame = element.exists ? element.frame : .zero
            guard !frame.isEmpty, screen.contains(CGPoint(x: frame.midX, y: frame.midY)) else { return false }
            let top = element.frame.minY - screen.minY
            return top > screen.height * 0.08 && top < screen.height * 0.55
        }
        // A swipe can carry it from under the bar to under the top of the screen, or off it
        // altogether in a lazy list; then scroll back a little.
        func pastTheTop() -> Bool {
            element.exists && element.frame.minY < app.frame.minY + app.frame.height * 0.08
        }
        var seen = false
        while !clear(), swipes < maxSwipes {
            // Never seen after half the swipes: it's above, not below.
            let lookUp = !seen && !element.exists && swipes >= maxSwipes / 2
            if pastTheTop() || (seen && !element.exists) || lookUp {
                scrollBack(app)
            } else {
                seen = seen || element.exists
                app.swipeUp(velocity: .slow)
            }
            swipes += 1
        }
    }

    /// Scrolls content back toward the top with a drag that starts mid-screen. A downward swipe
    /// on iPadOS 26 can turn a full-screen app into a small window, which then breaks typing.
    @MainActor
    func scrollBack(_ app: XCUIApplication) {
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.75))
        start.press(forDuration: 0.05, thenDragTo: end)
    }
}
