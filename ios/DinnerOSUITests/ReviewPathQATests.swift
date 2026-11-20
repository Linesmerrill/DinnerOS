import XCTest

/// What an App Reviewer does with a brand-new account and nothing set up: create a household,
/// find the starter recipes, reach Plus from Autopilot and from Household, add a recipe, browse
/// Shared Recipes and Companions, and cook. Keeps a screenshot of each step. Run on a fresh
/// install against an API with a starter library (STARTER_RECIPES_HOUSEHOLD_ID).
final class ReviewPathQATests: XCTestCase {
    private var problems: [String] = []

    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 900
    }

    @MainActor
    func testNewAccountReviewPath() throws {
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
        subject.typeText("reviewer-\(Int.random(in: 10000...99999))")
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Create a Household"].waitForExistence(timeout: 15))
        app.staticTexts["Create a Household"].tap()
        let name = app.textFields["Household name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.typeText("Review Kitchen")
        app.buttons["Create"].tap()
        XCTAssertTrue(app.staticTexts["Welcome to DinnerOS"].waitForExistence(timeout: 15))
        snap(app, "01 Welcome")
        if app.buttons["Start Planning"].waitForExistence(timeout: 5) { app.buttons["Start Planning"].tap() }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 15))
        sleep(3)
        snap(app, "02 Menu with starter recipes")
        let starters = app.buttons.matching(NSPredicate(format: "label CONTAINS 'minutes'"))
        if starters.count == 0 { problems.append("Menu: no starter recipes") }

        // Autopilot without Plus leads to the purchase screen.
        let autopilot = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Plan with Autopilot'")).firstMatch
        if autopilot.waitForExistence(timeout: 5) {
            autopilot.tap()
            sleep(3)
            snap(app, "03 Plan with Autopilot")
            if !app.staticTexts["DinnerOS Plus"].exists { problems.append("Plan with Autopilot: no purchase screen") }
            close(app)
        } else {
            problems.append("Menu: no Plan with Autopilot")
        }

        // Add a Recipe: the chooser and each way in.
        app.buttons["Add Recipes"].firstMatch.tap()
        sleep(2)
        snap(app, "04 Add a Recipe chooser")
        close(app)

        // Shared Recipes.
        let shared = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Shared Recipes'")).firstMatch
        reveal(shared, in: app, maxSwipes: 10)
        if shared.exists {
            shared.tap()
            sleep(3)
            snap(app, "05 Shared Recipes")
            close(app)
        } else {
            problems.append("Menu: no Shared Recipes row")
        }

        // Companions, from the ⋯ menu.
        scrollBack(app)
        scrollBack(app)
        app.buttons["Week Menu"].tap()
        sleep(1)
        snap(app, "06 Week Menu")
        let companions = app.buttons["Companions"].firstMatch
        if companions.waitForExistence(timeout: 3) {
            companions.tap()
            sleep(2)
            snap(app, "07 Companions")
            close(app)
        } else {
            pullDown(app)
            problems.append("Week Menu: no Companions")
        }

        // A recipe, then cooking.
        let card = app.buttons.matching(NSPredicate(format: "label CONTAINS 'minutes'")).firstMatch
        reveal(card, in: app, maxSwipes: 10)
        if card.exists {
            card.tap()
            if app.buttons["Start Cooking"].firstMatch.waitForExistence(timeout: 10) {
                sleep(2)
                snap(app, "08 Recipe")
                app.buttons["Start Cooking"].firstMatch.tap()
                sleep(2)
                snap(app, "09 Cooking guide")
                if app.buttons["Skip"].firstMatch.exists { app.buttons["Skip"].firstMatch.tap() }
                sleep(1)
                snap(app, "10 Cooking")
                close(app)
                close(app)
            } else {
                snap(app, "08 Recipe didn’t open")
                problems.append("Recipe didn’t open")
                close(app)
            }
        }

        for tab in ["Shop", "Pantry", "Household"] {
            app.buttons[tab].firstMatch.tap()
            sleep(2)
            snap(app, "11 \(tab)")
        }
        let plus = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'DinnerOS Plus'")).firstMatch
        if plus.waitForExistence(timeout: 5) {
            plus.tap()
            sleep(3)
            snap(app, "12 Plus from Household")
            close(app)
        } else {
            problems.append("Household: no DinnerOS Plus row")
        }
        let free = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Give a Free Month'")).firstMatch
        if free.waitForExistence(timeout: 3) {
            free.tap()
            sleep(3)
            snap(app, "13 Give a Free Month")
            close(app)
        }
        XCTAssertTrue(app.state == .runningForeground)
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
    }

    /// Not Now, Cancel, Done or Close if the screen has one, else back, else swipe the sheet away.
    @MainActor
    private func close(_ app: XCUIApplication) {
        for title in ["Not Now", "Cancel", "Done", "Close"] where app.buttons[title].firstMatch.exists {
            let button = app.buttons[title].firstMatch
            if button.isHittable {
                button.tap()
                sleep(1)
                return
            }
        }
        let back = backButton(app)
        if back.exists, back.isHittable { back.tap() } else { pullDown(app) }
        sleep(1)
    }
}
