import XCTest

/// A recipe typed by hand with steps, saved, then opened from the Menu in the same session.
/// This once left the app stuck on the recipe page.
final class SaveThenOpenQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 240
    }

    @MainActor
    func testSaveARecipeWithStepsThenOpenIt() throws {
        let app = launchSignedIn()
        let name = "QA Fudge \(Int.random(in: 100...999))"

        app.buttons["Week Menu"].tap()
        app.buttons["Add a Recipe"].tap()
        app.buttons["AddChoice-write"].tap()
        let nameField = app.textFields["Recipe name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 5))
        nameField.tap()
        nameField.typeText(name)

        let step = app.textViews["Step 1"].exists ? app.textViews["Step 1"] : app.textFields["Step 1"]
        reveal(step, in: app)
        step.tap()
        step.typeText("Boil the sugar and butter for one minute.")
        app.buttons["Done"].firstMatch.tap()
        XCTAssertTrue(waitGone(app.keyboards.firstMatch), "Done didn't put the keyboard away")

        app.buttons["Save"].tap()
        XCTAssertTrue(waitGone(app.navigationBars["Write Your Own"], timeout: 15), "Save didn't close the form")

        // Save opens the new recipe. It must settle: every query below waits for the app to idle.
        XCTAssertTrue(app.buttons["More"].waitForExistence(timeout: 15), "The saved recipe didn't open")
        snap(app, "Saved recipe opened after Save")
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 10), "The recipe page didn't go back")

        // Open it again from All Meals, the way a person would.
        let search = app.buttons["Search meals"].firstMatch
        reveal(search, in: app, maxSwipes: 12)
        search.tap()
        let field = app.textFields["Search meals"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText(name)
        let card = app.staticTexts[name].firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10), "The new recipe isn't in All Meals")
        card.tap()

        XCTAssertTrue(app.buttons["More"].waitForExistence(timeout: 15), "The recipe page didn't open")
        XCTAssertTrue(app.buttons["Start Cooking"].firstMatch.exists, "No Start Cooking on the recipe page")
        snap(app, "Saved recipe opened in the same session")
        // Still responsive: the back button works.
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 10), "The recipe page didn't go back")
    }
}
