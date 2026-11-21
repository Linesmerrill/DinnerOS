import XCTest

/// The Menu, Pantry and Shop More menus are opaque cards (`DSMenuPopover`) whose rows open what
/// they name, and the tab bar sits on a solid band over Shared Recipes' photos (Hark's review
/// items 10 and 11, build 122).
final class TabMoreMenusQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 900
    }

    @MainActor
    func testTabMoreMenusOpenTheirRows() {
        let app = launchSignedIn()

        // Menu: the week menu, then a row that pushes a page.
        app.buttons["Week Menu"].tap()
        XCTAssertTrue(app.buttons["Past Weeks"].waitForExistence(timeout: 5), "The week menu didn’t open")
        snap(app, "Tab menus › Menu")
        app.buttons["Shared Recipes"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Shared Recipes"].waitForExistence(timeout: 10), "Shared Recipes didn’t open")
        sleep(2)
        app.swipeUp()
        sleep(1)
        snap(app, "Tab menus › Shared Recipes scrolled")
        backButton(app).tap()

        // Pantry: the More menu, then a row that opens a sheet.
        app.buttons["Pantry"].firstMatch.tap()
        let pantryMore = app.buttons["More"].firstMatch
        XCTAssertTrue(pantryMore.waitForExistence(timeout: 10), "The Pantry has no More button")
        pantryMore.tap()
        let alerts = app.buttons["Low-Stock Alerts…"].firstMatch
        XCTAssertTrue(alerts.waitForExistence(timeout: 5), "The Pantry’s More menu didn’t open")
        snap(app, "Tab menus › Pantry")
        alerts.tap()
        XCTAssertTrue(waitGone(alerts, timeout: 5), "Low-Stock Alerts didn’t close the menu")
        pullDown(app)
        sleep(1)

        // Shop: the options menu, closed by a tap outside it.
        app.buttons["Shop"].firstMatch.tap()
        let options = app.buttons["Shop Options"].firstMatch
        if options.waitForExistence(timeout: 10) {
            options.tap()
            let saved = app.buttons["Saved Products"].firstMatch
            XCTAssertTrue(saved.waitForExistence(timeout: 5), "The Shop’s options menu didn’t open")
            snap(app, "Tab menus › Shop")
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.1, dy: 0.9)).tap()
            XCTAssertTrue(waitGone(saved, timeout: 5), "A tap outside didn’t close the Shop’s menu")
        }
    }
}
