import XCTest

/// Every recipe has a cuisine (decisions 720 to 723): typing a title fills it in, Edit Recipe
/// changes it, and the recipe page shows it. Runs against the local development server. The
/// random name keeps it safe beside other simulators on the same server.
final class CuisineQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testCuisineFillsInChangesAndShows() throws {
        let app = launchSignedIn()
        let name = "QA Carnitas Tacos \(Int.random(in: 1000...9999))"

        // Open Add a Recipe and write one by hand. Build 114 may put a chooser first.
        app.buttons["Week Menu"].tap()
        app.buttons["Add a Recipe"].tap()
        let write = app.buttons["AddChoice-write"]
        if write.waitForExistence(timeout: 3) { write.tap() }
        let typeIt = app.buttons["Type It Myself"]
        if typeIt.waitForExistence(timeout: 3) { typeIt.tap() }
        let nameField = app.textFields["Recipe name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 5), "The typed recipe form didn't open")
        nameField.tap()
        nameField.typeText(name)

        // The title names Mexican food, so Cuisine fills in. Pick it by hand if it didn't.
        let cuisine = app.buttons["RecipeCuisine"]
        reveal(cuisine, in: app)
        let mexican = NSPredicate(format: "value == 'Mexican'")
        let filled = expectation(for: mexican, evaluatedWith: cuisine)
        if XCTWaiter().wait(for: [filled], timeout: 8) != .completed {
            cuisine.tap()
            let option = app.buttons["CuisineOption-mexican"]
            XCTAssertTrue(option.waitForExistence(timeout: 5), "No Mexican in the cuisine list")
            option.tap()
        }
        XCTAssertEqual(cuisine.value as? String, "Mexican")
        snap(app, "01 Cuisine filled in from the title")

        // The list searches, and None is there.
        cuisine.tap()
        XCTAssertTrue(app.navigationBars["Cuisine"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["CuisineOption-none"].exists, "No None in the cuisine list")
        app.searchFields.firstMatch.tap()
        app.searchFields.firstMatch.typeText("Mexi")
        let found = app.buttons["CuisineOption-mexican"]
        XCTAssertTrue(found.waitForExistence(timeout: 3), "Search didn't find Mexican")
        XCTAssertFalse(app.buttons["CuisineOption-italian"].exists, "Search didn't narrow the list")
        snap(app, "02 Cuisine search")
        // Picking one closes the list.
        found.tap()
        XCTAssertTrue(waitGone(app.navigationBars["Cuisine"]), "Picking from search didn't close the list")
        XCTAssertEqual(cuisine.value as? String, "Mexican")

        // Cancel leaves it as it was.
        cuisine.tap()
        let close = app.buttons["CuisineCancel"]
        XCTAssertTrue(close.waitForExistence(timeout: 5))
        close.tap()
        XCTAssertTrue(waitGone(app.navigationBars["Cuisine"]), "Cancel didn't close the list")
        XCTAssertEqual(cuisine.value as? String, "Mexican", "Cancel changed the cuisine")

        // An ingredient and a step, so it can be saved.
        let addIngredient = app.buttons["Add Ingredient"]
        reveal(addIngredient, in: app)
        addIngredient.tap()
        XCTAssertTrue(app.navigationBars["New Ingredient"].waitForExistence(timeout: 5))
        app.buttons["2"].firstMatch.tap()
        app.buttons["Next"].firstMatch.tap()
        app.typeText("Pork Shoulder")
        // The sheet's own Done: on an iPad the form behind it has one too. With the keyboard up,
        // the first Done can only put it away; a second closes the sheet.
        let sheetDone = app.navigationBars["New Ingredient"].buttons["Done"]
        (sheetDone.exists ? sheetDone : app.buttons["Done"].firstMatch).tap()
        if !waitGone(app.navigationBars["New Ingredient"], timeout: 3), sheetDone.exists { sheetDone.tap() }
        XCTAssertTrue(waitGone(app.navigationBars["New Ingredient"], timeout: 5), "New Ingredient didn't close")
        // Step 1 can start below the fold, before the form has built it.
        let step = app.descendants(matching: .any).matching(NSPredicate(
            format: "(identifier == 'Step 1' OR label == 'Step 1') AND (elementType == %d OR elementType == %d)",
            XCUIElement.ElementType.textView.rawValue, XCUIElement.ElementType.textField.rawValue)).firstMatch
        reveal(step, in: app)
        step.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        step.typeText("Braise the pork until tender.")
        app.buttons["Done"].firstMatch.tap()

        // Save: the page says Mexican.
        app.buttons["Save"].tap()
        XCTAssertTrue(app.staticTexts[name].waitForExistence(timeout: 15), "The saved recipe didn't open")
        let shown = app.descendants(matching: .any)["RecipeCuisineShown"]
        XCTAssertTrue(shown.waitForExistence(timeout: 5), "The recipe page doesn't show its cuisine")
        XCTAssertTrue(shown.label.contains("Mexican"), "The page says \(shown.label)")
        snap(app, "03 Recipe page, Mexican")

        // Edit Recipe: change it to Italian and save.
        app.buttons["More"].tap()
        app.buttons["Edit Recipe"].tap()
        XCTAssertTrue(app.navigationBars["Edit Recipe"].waitForExistence(timeout: 5))
        let editCuisine = app.buttons["RecipeCuisine"]
        reveal(editCuisine, in: app)
        XCTAssertEqual(editCuisine.value as? String, "Mexican")
        editCuisine.tap()
        let italian = app.buttons["CuisineOption-italian"]
        XCTAssertTrue(italian.waitForExistence(timeout: 5), "No Italian in the cuisine list")
        reveal(italian, in: app)
        italian.tap()
        XCTAssertTrue(waitGone(app.navigationBars["Cuisine"]), "Picking one didn't close the list")
        XCTAssertEqual(editCuisine.value as? String, "Italian")
        snap(app, "04 Edit Recipe, Italian")
        app.buttons["Save"].firstMatch.tap()
        XCTAssertTrue(waitGone(app.navigationBars["Edit Recipe"], timeout: 15), "Edit Recipe didn't save")

        // The page says Italian now.
        let italianShown = NSPredicate(format: "label CONTAINS 'Italian'")
        let updated = expectation(for: italianShown, evaluatedWith: shown)
        XCTAssertEqual(XCTWaiter().wait(for: [updated], timeout: 10), .completed, "The page says \(shown.label)")
        snap(app, "05 Recipe page, Italian")

        // Delete it.
        app.buttons["More"].tap()
        app.buttons["Delete Recipe"].tap()
        let confirm = app.buttons["Delete Recipe"].firstMatch
        XCTAssertTrue(confirm.waitForExistence(timeout: 3))
        confirm.tap()
        XCTAssertTrue(app.staticTexts["\(name) deleted."].waitForExistence(timeout: 10), "It wasn't deleted")
    }
}
