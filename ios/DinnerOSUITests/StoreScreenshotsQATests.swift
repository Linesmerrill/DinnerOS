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
                    let times = app.descendants(matching: .any).matching(
                        NSPredicate(format: "label CONTAINS ' min' OR label CONTAINS 'Unknown' OR label CONTAINS ' hr'")
                    ).allElementsBoundByIndex.map {
                        "\($0.elementType.rawValue):\($0.label)@\(Int($0.frame.minX)),\(Int($0.frame.minY)) w\(Int($0.frame.width))"
                    }
                    print("QA-NOTE: autopilot times: \(times)")
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
            // No row cut or faded at the bottom edge, and the Walmart button kept whole under the
            // week's header (Hark's store review).
            let header = app.staticTexts["This Week"].firstMatch.frame.maxY + 6
            let walmart = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Choose '")).firstMatch
            settle(
                app, above: app.frame.maxY, band: 30, top: header,
                atMost: walmart.exists ? max(0, walmart.frame.minY - header) : 240)
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
                // Nothing resting faded under the plan bar (Hark's store review, item 5).
                // The first Customize choice whole, clear of the plan bar's fade (Hark's store
                // review, round 3).
                let choice = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'boneless chicken thighs'"))
                    .firstMatch
                lift(choice, in: app, to: app.buttons["Start Cooking"].firstMatch.frame.minY - 64)
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
        open(app, "DinnerOS Plus", shot: "08-your-plus") {
            // No card sliced by the bottom edge (Hark's store review, item 8).
            // The page is taller than the Duo's cover screen, so the Plus card goes up under the
            // title and the Free Months card shows whole (Hark's store review, round 3).
            let title = app.staticTexts["Your Plus"].firstMatch
            let header = title.exists ? title.frame.maxY + 4 : 60
            let since = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Plus since'")).firstMatch
            self.settle(
                app, above: app.frame.maxY, band: 4, top: header,
                atLeast: since.exists ? since.frame.maxY + 72 - header : 0, atMost: 900)
            // The card's shadow reaches about six points past it.
            MenuQAChecks.dragList(app, by: 17)
            sleep(1)
            self.full(app, "08-your-plus-settled")
        }
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
        // The end of the steps: no line cut by the bottom edge, and the timer floats over the
        // room after the last step rather than a step's heading (Hark's store review, items 6-7).
        if prefix.isEmpty { scrollToEnd(app) }
        full(app, prefix + "06-cook-steps")
        // A step's time opens the timer: one on screen.
        let times = app.buttons.matching(NSPredicate(format: "label CONTAINS 'minute'")).allElementsBoundByIndex
        let time =
            times.last { $0.isHittable && $0.frame.maxY < app.frame.maxY - 40 }
            ?? app.buttons.matching(NSPredicate(format: "label CONTAINS 'minutes'")).firstMatch
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
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The Menu tab didn’t open after sign-in")
        return app
    }

    /// Text, images and buttons that scroll: the Duo's side bar and the status bar left out.
    @MainActor
    private func scrolledFrames(_ app: XCUIApplication, barTop: CGFloat? = nil) -> [CGRect] {
        let screen = app.frame
        var out: [CGRect] = []
        for type in [XCUIElement.ElementType.staticText, .image, .button] {
            for e in app.descendants(matching: type).allElementsBoundByIndex {
                let f = e.frame
                guard !f.isEmpty, f.height < screen.height * 0.6, f.midX < screen.width * 0.84, f.minY > 60 else {
                    continue
                }
                if type == .button, let barTop, f.midY >= barTop { continue }
                out.append(f)
            }
        }
        return out
    }

    /// Moves the content by the least that leaves nothing resting in the `band` points above
    /// `limit` or cut by it, and nothing cut by `top`: no line sliced by an edge and nothing
    /// faded under a bar (Hark's store review). A drag moves the content a little less than the
    /// finger, so each drag after the first adds what the last one fell short by.
    @MainActor
    private func settle(
        _ app: XCUIApplication, above limit: CGFloat, band: CGFloat, top: CGFloat? = nil, atLeast floor: CGFloat = 0,
        atMost cap: CGFloat = 240
    ) {
        var movedSoFar: CGFloat = 0
        var shortBy: CGFloat = 10  // a drag loses about ten points before the content moves
        for attempt in 0..<4 {
            // A bar's own buttons at or under the limit stay put; leave them out.
            let frames = scrolledFrames(app, barTop: limit)
            func clear(_ d: CGFloat) -> Bool {
                !frames.contains { f in
                    let minY = f.minY - d
                    let maxY = f.maxY - d
                    if maxY > limit - band && minY < limit { return true }
                    if let top, minY < top && maxY > top { return true }
                    return false
                }
            }
            if clear(0) {
                print("QA-NOTE: settled after \(attempt) drags")
                return
            }
            let most: CGFloat = attempt == 0 ? max(240, floor + 240) : 80
            // Up first; back down only once the content has moved.
            let tries = stride(from: CGFloat(2), through: most, by: 2).flatMap { attempt == 0 ? [$0] : [$0, -$0] }
            guard let d = tries.first(where: { clear($0) && movedSoFar + $0 <= cap && movedSoFar + $0 >= floor }) else {
                print("QA-NOTE: no clear place within \(Int(most)) points")
                return
            }
            let before = textTops(app)
            let finger = d + (d >= 0 ? shortBy : -shortBy)
            MenuQAChecks.dragList(app, by: finger)
            sleep(1)
            if let moved = medianMove(from: before, to: textTops(app)) {
                shortBy = max(0, abs(finger) - abs(moved))
                movedSoFar += moved
                print("QA-NOTE: settle wanted \(Int(d)), dragged \(Int(finger)), moved \(Int(moved))")
            }
        }
        print("QA-NOTE: not settled after 4 drags")
    }

    /// Moves the content up so `element` ends at `y`, clear of the bar's fade below it: one drag,
    /// plus the ten or so points a drag loses before the content starts to move.
    @MainActor
    private func lift(_ element: XCUIElement, in app: XCUIApplication, to y: CGFloat) {
        guard element.exists else { return }
        let need = element.frame.maxY - y
        guard need > 2, need < 200 else {
            print("QA-NOTE: lift skipped, needs \(Int(need))")
            return
        }
        MenuQAChecks.dragList(app, by: need + 10)
        sleep(1)
        print("QA-NOTE: lift wanted \(Int(need)), now \(Int(element.frame.maxY - y)) over")
    }

    /// Where each text with a label of its own starts.
    @MainActor
    private func textTops(_ app: XCUIApplication) -> [String: CGFloat] {
        var tops: [String: CGFloat] = [:]
        var twice: Set<String> = []
        for e in app.staticTexts.allElementsBoundByIndex where !e.label.isEmpty {
            if tops[e.label] != nil { twice.insert(e.label) } else { tops[e.label] = e.frame.minY }
        }
        for label in twice { tops[label] = nil }
        return tops
    }

    /// How far the content moved up between two readings, by its texts' middle move.
    private func medianMove(from a: [String: CGFloat], to b: [String: CGFloat]) -> CGFloat? {
        let moves = a.compactMap { label, y in b[label].map { y - $0 } }.sorted()
        return moves.isEmpty ? nil : moves[moves.count / 2]
    }

    /// Scrolls to the end: until a swipe moves nothing.
    @MainActor
    private func scrollToEnd(_ app: XCUIApplication) {
        for _ in 0..<12 {
            let before = scrolledFrames(app).map(\.minY)
            app.swipeUp(velocity: .fast)
            sleep(1)
            if scrolledFrames(app).map(\.minY) == before { return }
        }
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
        failOnTestWording(testWording(on: app), in: name)
        if dump {
            let tree = XCTAttachment(string: app.debugDescription)
            tree.name = name + " tree"
            tree.lifetime = .keepAlways
            add(tree)
        }
    }
}
