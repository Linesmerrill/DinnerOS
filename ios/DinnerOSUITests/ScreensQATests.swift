import XCTest

/// Screens the tab sweep doesn't reach by tapping rows: a brand-new person's first run, the
/// paywall a household without Plus sees, recipe pages of every kind, cooking, and the sheets
/// behind buttons. Each step keeps a screenshot and checks for blank or unnamed buttons.
/// Light or dark comes from the simulator (`xcrun simctl ui <device> appearance dark`).
final class ScreensQATests: XCTestCase {
    private var problems: [String] = []

    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 600
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    // MARK: A new person

    @MainActor
    func testNewPersonFirstRun() throws {
        applyOrientation()
        let app = XCUIApplication()
        app.launch()
        let subject = app.textFields["Subject"]
        if subject.waitForExistence(timeout: 8) {
            subject.tap()
            subject.press(forDuration: 1.0)
            if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
            subject.typeText("qa-new-\(Int.random(in: 10000...99999))")
        } else {
            XCTFail("Already signed in: uninstall the app before this test")
            return
        }
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch.tap()

        XCTAssertTrue(app.staticTexts["Set Up Your Household"].waitForExistence(timeout: 15))
        shot(app, "First run › Set Up Your Household")
        app.staticTexts["Join with a Code"].tap()
        shot(app, "First run › Join with a Code")
        back(app)
        app.staticTexts["Create a Household"].tap()
        let name = app.textFields["Household name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        shot(app, "First run › New Household")
        name.tap()
        name.typeText("QA Kitchen")
        app.buttons["Create"].tap()

        XCTAssertTrue(app.staticTexts["Welcome to DinnerOS"].waitForExistence(timeout: 15))
        shot(app, "First run › Welcome")

        // A starter recipe from Shared Recipes, opened from the welcome screen.
        app.staticTexts["Shared Recipes"].firstMatch.tap()
        sleep(3)
        shot(app, "Shared Recipes")
        let first = app.cells.firstMatch.exists ? app.cells.firstMatch : app.scrollViews.buttons.element(boundBy: 0)
        if first.waitForExistence(timeout: 5) {
            first.tap()
            sleep(3)
            shot(app, "Recipe page › starter (Shared Recipes)")
            back(app)
        } else {
            problems.append("Shared Recipes: nothing to open")
        }
        back(app)

        if app.buttons["Start Planning"].waitForExistence(timeout: 5) { app.buttons["Start Planning"].tap() }
        XCTAssertTrue(app.buttons["Menu"].firstMatch.waitForExistence(timeout: 10))
        shot(app, "Menu › new household")
        for tab in ["Shop", "Pantry"] {
            app.buttons[tab].firstMatch.tap()
            sleep(2)
            shot(app, "\(tab) › new household")
        }

        // The paywall: this household has no Plus.
        app.buttons["Household"].firstMatch.tap()
        sleep(2)
        shot(app, "Household › new household")
        let plus = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Plus'")).firstMatch
        if plus.waitForExistence(timeout: 5) {
            plus.tap()
            sleep(3)
            shot(app, "Plus paywall")
            back(app)
        } else {
            problems.append("Household: no way to Plus for a household without it")
        }
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
    }

    // MARK: The main test household

    @MainActor
    func testDeeperScreens() throws {
        var app = launchSignedIn()
        let env = ProcessInfo.processInfo.environment

        // Recipe pages: typed by hand, imported from a meal kit. Found the way a person would,
        // by searching All Meals (XCTest's own link opening signed the app out).
        for (kind, key) in [("typed by hand", "QA_MANUAL_NAME"), ("imported", "QA_IMPORTED_NAME")] {
            guard let recipe = env[key] else {
                problems.append("Recipe page › \(kind): no name given")
                continue
            }
            // A fresh Menu each time, so the search starts from the top.
            if kind != "typed by hand" { app = launchSignedIn() }
            let field = app.textFields["Search meals"].firstMatch
            if !field.exists {
                let search = app.buttons["Search meals"].firstMatch
                reveal(search, in: app, maxSwipes: 12)
                search.tap()
            }
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            field.tap()
            let typed = (field.value as? String) ?? ""
            if !typed.isEmpty, typed != "Search meals" {
                field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: typed.count))
            }
            field.typeText(recipe)
            snap(app, "Menu › search typed, \(kind) (\(mode))")
            // Put the keyboard away with Done, then tap the result card that's on screen: the same
            // name can also sit in a carousel further up.
            let done = app.toolbars.buttons["Done"].firstMatch
            if done.waitForExistence(timeout: 2) { done.tap() }
            snap(app, "Menu › search after Done, \(kind) (\(mode))")
            // The week strip keeps the week you're on (it once ran back to weeks from 2025).
            let thisWeek = app.buttons.matching(NSPredicate(format: "label CONTAINS 'This Week'")).firstMatch
            if !(thisWeek.exists && app.frame.intersects(thisWeek.frame) && !thisWeek.frame.isEmpty) {
                problems.append("Menu › search: the week strip lost this week")
            }
            let cards = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", recipe))
            var card: XCUIElement?
            for _ in 0..<10 where card == nil {
                card = cards.allElementsBoundByIndex.first { $0.isHittable }
                if card == nil { sleep(1) }
            }
            guard let card else {
                snap(app, "Recipe page › \(kind): search result missing (\(mode))")
                problems.append("Recipe page › \(kind): \(recipe) isn't in All Meals")
                continue
            }
            card.tap()
            XCTAssertTrue(app.buttons["Start Cooking"].firstMatch.waitForExistence(timeout: 15), "\(kind) didn't open")
            shot(app, "Recipe page › \(kind)")
            for tab in ["Ingredients", "Nutrition"] where app.buttons[tab].firstMatch.exists {
                app.buttons[tab].firstMatch.tap()
                shot(app, "Recipe page › \(kind) › \(tab)")
            }
            if app.buttons["More"].exists {
                app.buttons["More"].tap()
                shot(app, "Recipe page › \(kind) › More menu")
                app.swipeDown()
            }
            app.swipeUp()
            shot(app, "Recipe page › \(kind) › scrolled")
            app.buttons["Start Cooking"].firstMatch.tap()
            sleep(2)
            shot(app, "Cooking › \(kind)")
            if app.buttons["Done"].firstMatch.waitForExistence(timeout: 3) { app.buttons["Done"].firstMatch.tap() }
            sleep(1)
            back(app)
        }

        // Back from a recipe found by search, the search keyboard comes back over the tab bar.
        // Done above it must put it away.
        if app.keyboards.firstMatch.exists {
            snap(app, "Menu › search keyboard (\(mode))")
            let done = app.toolbars.buttons["Done"].firstMatch
            if done.exists { done.tap() }
            if !waitGone(app.keyboards.firstMatch) {
                problems.append("Menu › search: no way to put the keyboard away")
            }
        }

        // Menu: notifications, the ⋯ menu, setup again.
        app.buttons["Menu"].firstMatch.tap()
        sleep(1)
        let bell = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'notification'")).firstMatch
        if bell.exists {
            bell.tap()
            sleep(1)
            shot(app, "Menu › Notifications")
            back(app)
        }
        app.buttons["Week Menu"].tap()
        shot(app, "Menu › ⋯ menu")
        if app.buttons["Run Setup Again"].exists {
            app.buttons["Run Setup Again"].tap()
            sleep(2)
            shot(app, "Autopilot setup")
            back(app)
        } else {
            app.swipeDown()
        }

        // Shop: choosing products in Walmart, given time to load.
        app.buttons["Shop"].firstMatch.tap()
        sleep(1)
        let choose = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH 'Choose' AND label CONTAINS 'Walmart'")).firstMatch
        if choose.waitForExistence(timeout: 5) {
            choose.tap()
            sleep(10)
            shot(app, "Shop › Choose in Walmart (after 10 s)")
            back(app)
        }

        // Pantry: add an item.
        app.buttons["Pantry"].firstMatch.tap()
        sleep(1)
        let add = app.buttons["Add an Item"].exists ? app.buttons["Add an Item"] : app.buttons["Add"].firstMatch
        if add.exists {
            add.tap()
            sleep(1)
            shot(app, "Pantry › Add an Item")
            back(app)
        }

        // Household: inviting someone.
        app.buttons["Household"].firstMatch.tap()
        sleep(1)
        let members = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH 'Members'"))
            .firstMatch
        guard members.waitForExistence(timeout: 5) else {
            problems.append("Household: no Members row")
            XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
            return
        }
        members.tap()
        let invite = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Add Someone'")).firstMatch
        if invite.waitForExistence(timeout: 5) {
            invite.tap()
            sleep(1)
            shot(app, "Household › Members › Add Someone")
            back(app)
        }
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
    }

    // MARK: Helpers

    @MainActor
    private func shot(_ app: XCUIApplication, _ name: String) {
        snap(app, "\(name) (\(mode))")
        let screen = app.frame
        for bar in app.navigationBars.allElementsBoundByIndex {
            for button in bar.buttons.allElementsBoundByIndex {
                let frame = button.frame
                guard !frame.isEmpty, screen.intersects(frame) else { continue }
                let named =
                    !button.label.trimmingCharacters(in: .whitespaces).isEmpty
                    || button.descendants(matching: .any).allElementsBoundByIndex.contains { !$0.label.isEmpty }
                if !named { problems.append("\(name): a toolbar button has no name (blank slot?)") }
            }
        }
    }

    /// Cancel, Done or Close if the screen has one, else the back button, else swipe the sheet away.
    @MainActor
    private func back(_ app: XCUIApplication) {
        for title in ["Set Up Later", "Cancel", "Done", "Close", "Not Now"] where app.buttons[title].firstMatch.exists {
            let button = app.buttons[title].firstMatch
            if button.isHittable {
                button.tap()
                sleep(1)
                return
            }
        }
        let back = app.navigationBars.buttons.element(boundBy: 0)
        if back.exists, back.isHittable {
            back.tap()
        } else {
            app.swipeDown(velocity: .fast)
        }
        sleep(1)
    }
}
