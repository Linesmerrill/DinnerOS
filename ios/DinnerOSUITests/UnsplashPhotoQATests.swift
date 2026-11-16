import XCTest

/// Photo search with Unsplash switched on (decision 705): search, pick, and the credit shown in
/// the form, on the recipe page and in Edit Recipe. Runs only against a local API that has an
/// access key and points at `cmd/fakeunsplash` (QA_PHOTO_SEARCH=on); the keyless case, where
/// Search the Web is hidden, is in AddRecipeQATests.
final class UnsplashPhotoQATests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipUnless(
            ProcessInfo.processInfo.environment["QA_PHOTO_SEARCH"] == "on",
            "Needs a local API with photo search on")
    }

    @MainActor
    func testSearchPickAndCreditAPhoto() {
        let app = launchSignedIn()
        let name = "QA Garden Salad \(Int.random(in: 100...999))"
        let credit = "Photo by QA Photographer on Unsplash"

        app.buttons["Week Menu"].tap()
        app.buttons["Add a Recipe"].tap()
        app.buttons["Type It Myself"].tap()
        let nameField = app.textFields["Recipe name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 5))
        nameField.tap()
        nameField.typeText(name)
        app.staticTexts["Recipe"].tap()

        // Choose Photo opens on Search the Web, already searching the recipe's name.
        let choose = app.buttons["Choose Photo"]
        reveal(choose, in: app)
        choose.tap()
        XCTAssertTrue(app.navigationBars["Choose a Photo"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Search the Web"].exists, "No Search the Web with photo search on")
        let result = app.buttons[credit].firstMatch
        XCTAssertTrue(result.waitForExistence(timeout: 15), "No results credited to their photographer")
        let source = app.descendants(matching: .any)["PhotoSource"].firstMatch
        reveal(source, in: app, maxSwipes: 3)
        XCTAssertTrue(source.exists, "No Photos from Unsplash link")
        XCTAssertEqual(source.label, "Photos from Unsplash")
        snap(app, "Unsplash 01 Search results")

        // Pick one: the form shows it with its credit.
        scrollBack(app)
        app.buttons[credit].firstMatch.tap()
        let formCredit = app.descendants(matching: .any)["PhotoCredit"].firstMatch
        XCTAssertTrue(formCredit.waitForExistence(timeout: 15), "No credit under the picked photo")
        XCTAssertTrue(formCredit.label.contains(credit), "The credit says \(formCredit.label)")
        snap(app, "Unsplash 02 Picked, with credit")

        // An ingredient and a step, then save.
        let addIngredient = app.buttons["Add Ingredient"]
        reveal(addIngredient, in: app)
        addIngredient.tap()
        XCTAssertTrue(app.navigationBars["New Ingredient"].waitForExistence(timeout: 5))
        app.buttons["1"].firstMatch.tap()
        app.buttons["cup"].firstMatch.tap()
        app.buttons["Next"].firstMatch.tap()
        app.typeText("Lettuce")
        app.buttons["Done"].firstMatch.tap()
        let step = app.textViews["Step 1"].exists ? app.textViews["Step 1"] : app.textFields["Step 1"]
        reveal(step, in: app)
        step.tap()
        step.typeText("Toss everything together.")
        app.buttons["Done"].firstMatch.tap()
        app.buttons["Save"].tap()

        // The recipe page credits the photo under it.
        XCTAssertTrue(app.staticTexts[name].waitForExistence(timeout: 15), "The saved recipe didn't open")
        let pageCredit = app.descendants(matching: .any)["PhotoCredit"].firstMatch
        XCTAssertTrue(pageCredit.waitForExistence(timeout: 10), "No credit on the recipe page")
        XCTAssertTrue(pageCredit.label.contains(credit))
        snap(app, "Unsplash 03 Recipe page credit")

        // Edit Recipe keeps it.
        app.buttons["More"].tap()
        app.buttons["Edit Recipe"].tap()
        XCTAssertTrue(app.navigationBars["Edit Recipe"].waitForExistence(timeout: 5))
        XCTAssertTrue(
            app.descendants(matching: .any)["PhotoCredit"].firstMatch.waitForExistence(timeout: 5),
            "Edit Recipe lost the credit")
        snap(app, "Unsplash 04 Edit Recipe credit")
        app.buttons["Cancel"].firstMatch.tap()

        // Clean up.
        app.buttons["More"].tap()
        app.buttons["Delete Recipe"].tap()
        app.buttons["Delete Recipe"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["\(name) deleted."].waitForExistence(timeout: 10))
    }
}
