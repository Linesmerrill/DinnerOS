import XCTest

/// A person who says no to every permission still gets through: location for the store finder,
/// and notifications from Account. Run on a fresh install (`xcrun simctl uninstall` first) so the
/// system asks.
final class PermissionsDeniedQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 600
    }

    @MainActor
    func testSayingNoToEverything() throws {
        applyOrientation()
        let app = XCUIApplication()
        app.launch()
        let subject = app.textFields["Subject"]
        guard subject.waitForExistence(timeout: 8) else {
            XCTFail("Already signed in: uninstall the app before this test")
            return
        }
        subject.tap()
        subject.press(forDuration: 1.0)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
        subject.typeText("sam-\(Int.random(in: 10000...99999))")
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Create a Household"].waitForExistence(timeout: 15))
        app.staticTexts["Create a Household"].tap()
        let name = app.textFields["Household name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.typeText("Our Place")
        app.buttons["Create"].tap()
        if app.buttons["Start Planning"].waitForExistence(timeout: 15) { app.buttons["Start Planning"].tap() }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 15))

        // Location: Find Stores asks, the answer is no, and typing a ZIP code still works.
        app.buttons["Shop"].firstMatch.tap()
        let find = app.buttons.matching(
            NSPredicate(format: "label CONTAINS[c] 'location' OR label CONTAINS[c] 'Find Stores'")
        )
        .firstMatch
        // On a small phone the store finder is below the fold.
        if !find.waitForExistence(timeout: 5) { reveal(find, in: app, maxSwipes: 6) }
        if find.exists {
            find.tap()
            dontAllow()
            let message = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Location is off'")).firstMatch
            XCTAssertTrue(message.waitForExistence(timeout: 20), "No message after saying no to location")
            snap(app, "Shop › location denied")
        } else {
            snap(app, "Shop › no location button")
            XCTFail("No location button on the store finder")
        }

        // Notifications: Account › Turn On Notifications, then no.
        app.buttons["Household"].firstMatch.tap()
        let account = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Account'")).firstMatch
        XCTAssertTrue(account.waitForExistence(timeout: 8))
        account.tap()
        let turnOn = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Notifications'")).firstMatch
        if turnOn.waitForExistence(timeout: 5) {
            turnOn.tap()
            dontAllow()
            sleep(2)
            snap(app, "Account › notifications denied")
        }
        XCTAssertTrue(app.state == .runningForeground, "The app isn’t running after saying no")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.buttons["Menu"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 5))
    }

    /// Taps "Don't Allow" on the system's question.
    @MainActor
    private func dontAllow() {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let no = springboard.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Don'")).firstMatch
        if no.waitForExistence(timeout: 8) { no.tap() }
    }
}
