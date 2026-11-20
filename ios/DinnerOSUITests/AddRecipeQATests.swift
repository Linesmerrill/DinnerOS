import XCTest

/// Adds a recipe by hand from start to finish, the way a person would, then opens it, checks
/// its tabs and toolbar, and deletes it. Runs against the local development server.
final class AddRecipeQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testAddARecipeByHandEndToEnd() throws {
        let app = launchSignedIn()
        let name = "QA No Bake Cookies \(Int.random(in: 100...999))"

        // Open Add a Recipe and write it myself.
        app.buttons["Week Menu"].tap()
        app.buttons["Add a Recipe"].tap()
        snap(app, "01 Add a Recipe, choices")
        app.buttons["AddChoice-write"].tap()
        let nameField = app.textFields["Recipe name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 5))
        snap(app, "02 Review form, empty")

        // Name, with the keyboard put away by tapping outside.
        nameField.tap()
        nameField.typeText(name)
        assertKeyboardDismissesOnTapOutside(app, tapping: app.staticTexts["Recipe"], "the recipe name")

        // Servings: type 36, past the old 12 limit.
        let servings = app.textFields["Servings"]
        servings.tap()
        servings.typeText("36")
        XCTAssertEqual(servings.value as? String, "36")
        XCTAssertTrue(app.buttons["Done"].firstMatch.exists, "No Done above the number pad")
        app.buttons["Done"].firstMatch.tap()
        XCTAssertTrue(waitGone(app.keyboards.firstMatch), "Done didn’t put the number pad away")

        // Prep 15 min, cook 4 hr, from the quick picks.
        app.buttons["Prep time"].tap()
        app.buttons["15 min"].firstMatch.tap()
        snap(app, "03 Prep time picker")
        app.buttons["Cook time"].tap()
        app.buttons["4 hr"].firstMatch.tap()
        XCTAssertEqual(app.buttons["Cook time"].value as? String, "4 hr")
        app.buttons["Cook time"].tap()
        let total = app.staticTexts["Total: 4 hr 15 min"]
        reveal(total, in: app, maxSwipes: 3)
        XCTAssertTrue(total.waitForExistence(timeout: 3), "Total didn’t add up")

        // Choose Photo opens the chooser. The local API has no photo search key, so there's no
        // Search the Web, only the library (decision 704).
        // It's near the top of the form, and checking the total scrolled to the bottom.
        let choose = app.buttons["Choose Photo"]
        for _ in 0..<6 where !(choose.exists && choose.isHittable && choose.frame.minY > app.frame.height * 0.15) {
            scrollBack(app)
        }
        choose.tap()
        XCTAssertTrue(app.navigationBars["Choose a Photo"].waitForExistence(timeout: 5), "Choose Photo opened nothing")
        sleep(2)
        XCTAssertFalse(app.buttons["Search the Web"].exists, "Search the Web showed with photo search off")
        XCTAssertFalse(app.textFields["Search for a photo"].exists, "The photo search box showed with search off")
        snap(app, "04 Choose a Photo, library only")
        app.buttons["Cancel"].firstMatch.tap()

        // Two ingredients in the New Ingredient sheet, with a suggestion and a note.
        let addIngredient = app.buttons["Add Ingredient"]
        reveal(addIngredient, in: app)
        addIngredient.tap()
        XCTAssertTrue(app.navigationBars["New Ingredient"].waitForExistence(timeout: 5))
        app.buttons["2"].firstMatch.tap()
        app.buttons["Tbsp"].firstMatch.tap()
        snap(app, "06 New Ingredient, amount keypad")
        app.buttons["Next"].firstMatch.tap()
        app.typeText("Garl")
        snap(app, "07 New Ingredient, suggestions")
        let suggestion = app.buttons["Garlic"].firstMatch
        if suggestion.waitForExistence(timeout: 3) {
            suggestion.tap()
        } else {
            app.typeText("ic")
        }
        let note = app.textFields["Minced, diced, softened"]
        note.tap()
        note.typeText("minced")
        app.buttons["Add Another"].tap()
        XCTAssertTrue(app.staticTexts["1 ingredient added."].waitForExistence(timeout: 3))
        app.buttons["1"].firstMatch.tap()
        app.buttons["cup"].firstMatch.tap()
        app.buttons["Next"].firstMatch.tap()
        app.typeText("Sugar")
        app.buttons["Add Another"].tap()
        app.buttons["Next"].firstMatch.tap()
        app.typeText("Oats")
        app.buttons["Done"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["2 Tbsp, minced"].waitForExistence(timeout: 5), "Row didn’t show amount, note")
        snap(app, "08 Ingredients list")

        // Reorder & Delete: delete one, undo, delete it again.
        app.buttons["Ingredients Options"].tap()
        app.buttons["Reorder & Delete Ingredients"].tap()
        XCTAssertTrue(app.navigationBars["Reorder & Delete Ingredients"].waitForExistence(timeout: 5))
        app.staticTexts["Oats"].tap()
        snap(app, "09 Reorder and Delete")
        app.buttons["Delete 1"].tap()
        app.buttons["Undo"].tap()
        XCTAssertTrue(app.staticTexts["Oats"].exists, "Undo didn’t bring it back")
        app.staticTexts["Oats"].tap()
        app.buttons["Delete 1"].tap()
        app.buttons["Done"].firstMatch.tap()
        XCTAssertFalse(app.staticTexts["Oats"].exists)

        // A row's ⋯ menu moves it.
        app.buttons["More for Sugar"].tap()
        app.buttons["Move Up"].tap()

        // Steps, with the keyboard put away by Done.
        let step = app.textViews["Step 1"].exists ? app.textViews["Step 1"] : app.textFields["Step 1"]
        reveal(step, in: app)
        step.tap()
        step.typeText("Boil the butter, sugar and milk for one minute.")
        XCTAssertTrue(app.buttons["Done"].firstMatch.exists, "No Done above the keyboard in a step")
        app.buttons["Done"].firstMatch.tap()
        XCTAssertTrue(waitGone(app.keyboards.firstMatch), "Done didn’t put the keyboard away in a step")
        app.buttons["Add Step"].tap()
        app.typeText("Stir in the oats and drop by spoonfuls.")
        pullDown(app)
        snap(app, "10 Steps")

        // Nutrition.
        let calories = app.textFields["Calories"]
        reveal(calories, in: app)
        calories.tap()
        calories.typeText("180")
        app.buttons["Done"].firstMatch.tap()
        app.buttons["More Nutrition"].tap()
        let sugar = app.textFields["Sugar"]
        reveal(sugar, in: app)
        sugar.tap()
        sugar.typeText("14")
        app.buttons["Done"].firstMatch.tap()
        snap(app, "11 Nutrition")

        // Save and open it.
        app.buttons["Save"].tap()
        XCTAssertTrue(app.staticTexts[name].waitForExistence(timeout: 15), "The saved recipe didn’t open")
        snap(app, "12 Recipe page")

        // The toolbar has no blank buttons: More holds Edit and Delete.
        let more = app.buttons["More"]
        XCTAssertTrue(more.exists, "No More menu on the recipe page")
        more.tap()
        XCTAssertTrue(app.buttons["Edit Recipe"].exists)
        XCTAssertTrue(app.buttons["Delete Recipe"].exists)
        snap(app, "13 Recipe More menu")
        app.buttons["Edit Recipe"].tap()
        XCTAssertTrue(app.navigationBars["Edit Recipe"].waitForExistence(timeout: 5))
        snap(app, "14 Edit Recipe")
        app.buttons["Cancel"].firstMatch.tap()

        // Every tab.
        for tab in ["Ingredients", "Nutrition", "Description"] {
            let button = app.buttons[tab].firstMatch
            if button.exists {
                button.tap()
                snap(app, "15 Tab \(tab)")
            }
        }

        // Delete it, then undo from the toast.
        app.buttons["More"].tap()
        app.buttons["Delete Recipe"].tap()
        let confirm = app.buttons["Delete Recipe"].firstMatch
        XCTAssertTrue(confirm.waitForExistence(timeout: 3))
        snap(app, "16 Delete confirmation")
        confirm.tap()
        XCTAssertTrue(
            app.staticTexts["\(name) deleted."].waitForExistence(timeout: 10), "No Undo toast after deleting")
        snap(app, "17 Undo toast")
        app.buttons["Undo"].tap()
        XCTAssertTrue(waitGone(app.staticTexts["\(name) deleted."], timeout: 10))
    }
}
