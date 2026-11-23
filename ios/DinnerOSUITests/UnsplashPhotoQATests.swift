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
    func testSearchPickAndCreditAPhoto() throws {
        // A household of its own: this test adds and deletes recipes (QAOwnHousehold).
        let app = launchSignedIn(as: try ownHousehold("unsplash"))
        let name = "Garden Salad \(Int.random(in: 100...999))"
        let credit = "Photo by Elena Park on Unsplash"

        app.buttons["Week Menu"].tap()
        app.buttons["Add a Recipe"].tap()
        app.buttons["AddChoice-write"].tap()
        let nameField = app.textFields["Recipe Name"]
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
        // The footer link exists below the results without scrolling to it. No scrolling back
        // either: on an iPad a downward drag can close the chooser.
        let source = app.descendants(matching: .any)["PhotoSource"].firstMatch
        XCTAssertTrue(source.waitForExistence(timeout: 5), "No Photos from Unsplash link")
        XCTAssertEqual(source.label, "Photos from Unsplash")
        snap(app, "Unsplash 01 Search results")

        // Pick one: the form shows it with its credit.
        result.tap()
        // On an iPad the form is a short sheet, and the credit under a tall photo can start below
        // its fold, where the form hasn't made it yet.
        let formCredit = app.descendants(matching: .any)["PhotoCredit"].firstMatch
        if !formCredit.waitForExistence(timeout: 8) { reveal(formCredit, in: app, maxSwipes: 4) }
        XCTAssertTrue(formCredit.waitForExistence(timeout: 8), "No credit under the picked photo")
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
        // The sheet's own Done: on an iPad the form behind it has one too.
        let sheetDone = app.navigationBars["New Ingredient"].buttons["Done"]
        (sheetDone.exists ? sheetDone : app.buttons["Done"].firstMatch).tap()
        // With the keyboard up, the first Done can only put it away; a second closes the sheet.
        if !waitGone(app.navigationBars["New Ingredient"], timeout: 3), sheetDone.exists { sheetDone.tap() }
        XCTAssertTrue(waitGone(app.navigationBars["New Ingredient"], timeout: 5), "New Ingredient didn’t close")
        let step = app.textViews["Step 1"].exists ? app.textViews["Step 1"] : app.textFields["Step 1"]
        reveal(step, in: app)
        // On an iPad in landscape the field can report itself not hittable under the form's
        // bottom bar; tapping its middle reaches it either way.
        step.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        step.typeText("Toss everything together.")
        app.buttons["Done"].firstMatch.tap()
        app.buttons["Save"].tap()

        // The recipe page credits the photo under it.
        XCTAssertTrue(app.staticTexts[name].waitForExistence(timeout: 15), "The saved recipe didn’t open")
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
