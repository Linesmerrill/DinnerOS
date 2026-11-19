import XCTest

/// Takes the App Store screenshots from a prepared household on a local API (see
/// docs/app-store.md, Screenshots). Each shot is the whole screen, status bar included, named for
/// its file in store-screenshots/. Sign in as QA_SUBJECT (default "jordan"), or with
/// QA_REVIEW_EMAIL and QA_REVIEW_PASSWORD as the App Review account. Run on the device
/// size the files need, with `xcrun simctl status_bar <id> override --time 9:41 ...` first.
final class StoreScreenshotsQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 900
    }

    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var dump: Bool { env["QA_DUMP"] == "1" }

    @MainActor
    func testPhoneScreenshots() throws {
        let app = signIn()
        let planned = env["QA_PLANNED_RECIPE"] ?? "Spiced Pork"
        // QA_ONLY=household retakes just the Household screens.
        if env["QA_ONLY"] != "household" {
            // Autopilot's picks for the open nights, then added to the week.
            app.buttons["By Day"].firstMatch.tap()
            let autopilot = app.buttons.matching(
                NSPredicate(format: "label BEGINSWITH 'Plan with Autopilot' OR label BEGINSWITH 'Autopilot suggested'")
            ).firstMatch
            if autopilot.waitForExistence(timeout: 8) {
                autopilot.tap()
                // The first time, Autopilot asks about the calendar and forecast: plan without.
                if app.staticTexts["Plan Around Your Week"].waitForExistence(timeout: 4) {
                    app.buttons["Not Now"].firstMatch.tap()
                }
                let add = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Add ' AND label CONTAINS 'Week'"))
                    .firstMatch
                if add.waitForExistence(timeout: 30) {
                    sleep(4)
                    full(app, "03-autopilot")
                    add.tap()
                    sleep(3)
                } else {
                    full(app, "autopilot-missing")
                }
            }

            scrollToTop(app)
            sleep(4)
            full(app, "02-week")
            showMenu(app)
            scrollToTop(app)
            sleep(5)
            full(app, "01-menu")

            app.buttons["Shop"].firstMatch.tap()
            sleep(5)
            full(app, "04-shop")

            showMenu(app)
            scrollToTop(app)
            let card = app.buttons.matching(
                NSPredicate(format: "label BEGINSWITH %@ AND label CONTAINS 'in your week'", planned)
            ).firstMatch
            reveal(card, in: app, maxSwipes: 12)
            if card.exists {
                card.tap()
                XCTAssertTrue(app.buttons["Start Cooking"].firstMatch.waitForExistence(timeout: 15))
                sleep(4)
                full(app, "05-recipe")
                cook(app, prefix: "")
            } else {
                full(app, "recipe-missing")
            }

        }

        // Cooking can stay open over the tabs; start again from the Menu.
        if !app.buttons["Week Menu"].exists {
            app.terminate()
            app.launch()
            XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30))
        }
        let household = app.buttons.matching(NSPredicate(format: "label == 'Household' AND identifier == 'person.2'"))
            .firstMatch
        (household.waitForExistence(timeout: 5) ? household : app.buttons["Household"].firstMatch).tap()
        sleep(3)
        full(app, "household-1-tab")
        open(app, "DinnerOS Plus", shot: "08-your-plus")
        open(app, "Members", shot: "household-2-members")
        open(app, "Give a Free Month", shot: "household-3-give-a-free-month") {
            let share = app.buttons["Share Your Code"].firstMatch
            guard share.waitForExistence(timeout: 5) else { return }
            share.tap()
            sleep(3)
            self.full(app, "household-3-give-a-free-month-share")
            app.swipeDown(velocity: .fast)
            sleep(1)
        }
        open(app, "Account", shot: "household-4-account")
    }

    @MainActor
    func testPadScreenshots() throws {
        XCUIDevice.shared.orientation = .landscapeLeft
        let app = signIn()
        XCUIDevice.shared.orientation = .landscapeLeft
        showMenu(app)
        scrollToTop(app)
        sleep(5)
        full(app, "ipad-01-menu")
        let planned = env["QA_PLANNED_RECIPE"] ?? "Spiced Pork"
        let card = app.buttons.matching(
            NSPredicate(format: "label BEGINSWITH %@ AND label CONTAINS 'in your week'", planned)
        ).firstMatch
        reveal(card, in: app, maxSwipes: 12)
        if card.exists {
            card.tap()
            XCTAssertTrue(app.buttons["Start Cooking"].firstMatch.waitForExistence(timeout: 15))
            sleep(4)
            full(app, "ipad-02-recipe")
            cook(app, prefix: "ipad-")
        } else {
            full(app, "ipad-recipe-missing")
        }
    }

    // MARK: Steps

    @MainActor
    private func cook(_ app: XCUIApplication, prefix: String) {
        app.buttons["Start Cooking"].firstMatch.tap()
        sleep(3)
        // The first time, a short guide covers cooking.
        if app.buttons["Skip"].firstMatch.exists { app.buttons["Skip"].firstMatch.tap() }
        sleep(1)
        if app.buttons["Steps"].firstMatch.exists { app.buttons["Steps"].firstMatch.tap() }
        sleep(2)
        full(app, prefix + "06-cook-steps")
        // A step's time opens the timer.
        let time = app.buttons.matching(NSPredicate(format: "label CONTAINS 'minutes'")).firstMatch
        if time.waitForExistence(timeout: 3) {
            time.tap()
            sleep(2)
            full(app, prefix + "07-timer")
            // Started, the timer runs above the steps.
            let start = app.buttons["Start Timer"].firstMatch
            if start.exists {
                start.tap()
                // The first timer may ask to send notifications.
                let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
                let allow = springboard.buttons["Allow"].firstMatch
                if allow.waitForExistence(timeout: 3) { allow.tap() }
                sleep(3)
                full(app, prefix + "06-cook-steps-timer")
            } else {
                app.swipeDown()
            }
        }
        // Done closes the timer first, then cooking.
        for _ in 0..<3 where !app.buttons["Start Cooking"].firstMatch.exists {
            if app.buttons["Done"].firstMatch.waitForExistence(timeout: 3) { app.buttons["Done"].firstMatch.tap() }
            sleep(1)
        }
        sleep(1)
        let back = app.navigationBars.buttons.element(boundBy: 0)
        if back.exists, back.isHittable { back.tap() }
        sleep(1)
    }

    /// The Menu tab, in its Menu view rather than By Day.
    @MainActor
    private func showMenu(_ app: XCUIApplication) {
        let tab = app.buttons.matching(NSPredicate(format: "label == 'Menu' AND identifier == 'fork.knife'")).firstMatch
        if tab.exists { tab.tap() } else { app.buttons["Menu"].firstMatch.tap() }
        sleep(1)
        let segment = app.buttons.matching(NSPredicate(format: "label == 'Menu' AND identifier != 'fork.knife'"))
            .firstMatch
        if segment.waitForExistence(timeout: 3) { segment.tap() }
        sleep(1)
    }

    @MainActor
    private func open(_ app: XCUIApplication, _ row: String, shot: String, then more: () -> Void = {}) {
        let element = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", row)).firstMatch
        let target = element.exists ? element : app.staticTexts[row].firstMatch
        guard target.waitForExistence(timeout: 5) else {
            full(app, shot + "-missing")
            return
        }
        target.tap()
        sleep(3)
        full(app, shot)
        more()
        let back = app.navigationBars.buttons.element(boundBy: 0)
        if back.exists, back.isHittable { back.tap() } else { app.swipeDown(velocity: .fast) }
        sleep(1)
    }

    @MainActor
    private func signIn() -> XCUIApplication {
        let app = XCUIApplication()
        app.launch()
        // The App Review account (docs/review-account.md): sign in with its email and password.
        let email = app.buttons["signIn.email.open"]
        if let address = env["QA_REVIEW_EMAIL"], email.waitForExistence(timeout: 8) {
            email.tap()
            let field = app.textFields["signIn.email.address"]
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            field.tap()
            field.typeText(address)
            let secure = app.secureTextFields["signIn.email.password"]
            secure.tap()
            secure.typeText(env["QA_REVIEW_PASSWORD"] ?? "")
            app.buttons["signIn.email.submit"].tap()
            let start = app.buttons["Start Planning"].firstMatch
            if start.waitForExistence(timeout: 15) { start.tap() }
        }
        let subject = app.textFields["Subject"]
        if env["QA_REVIEW_EMAIL"] == nil, subject.waitForExistence(timeout: 8) {
            subject.tap()
            // Clear what's there key by key: Select All didn't always show.
            let typed = (subject.value as? String) ?? ""
            subject.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: typed.count + 2))
            subject.typeText(env["QA_SUBJECT"] ?? "jordan")
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch.tap()
        }
        if app.buttons["Thanks"].waitForExistence(timeout: 4) { app.buttons["Thanks"].tap() }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The Menu tab didn't open after sign-in")
        return app
    }

    @MainActor
    private func scrollToTop(_ app: XCUIApplication) {
        for _ in 0..<2 {
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
            start.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.9)))
        }
    }

    /// The whole screen, status bar included.
    @MainActor
    private func full(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if dump {
            let tree = XCTAttachment(string: app.debugDescription)
            tree.name = name + " tree"
            tree.lifetime = .keepAlways
            add(tree)
        }
    }
}
