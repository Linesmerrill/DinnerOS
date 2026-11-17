import XCTest

/// The live check for a Kroger-family recipe (decision 725): the real Fry's page, read on this
/// phone in the hidden web view and parsed by the local API, reaches the review screen. It needs
/// the internet, so it only runs with QA_LIVE_IMPORT=1 and is not in the QA matrix.
final class LiveImportQATests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipUnless(
            ProcessInfo.processInfo.environment["QA_LIVE_IMPORT"] == "1", "Set QA_LIVE_IMPORT=1 to run")
    }

    @MainActor
    func testAFrysRecipeImports() throws {
        let link = "https://www.frysfood.com/r/plant-based-calzones-recipe/62519060-3b1b-4fcd-90ac-b604886fdd81"
        let app = launchSignedIn()
        app.buttons["Week Menu"].tap()
        let adds = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Add a Recipe'"))
        XCTAssertTrue(adds.firstMatch.waitForExistence(timeout: 5), "No Add a Recipe")
        sleep(1)
        let add = adds.allElementsBoundByIndex.last { $0.isHittable } ?? adds.firstMatch
        add.tap()
        app.buttons["AddChoice-import"].tap()
        let field = app.textFields["https://…"]
        XCTAssertTrue(field.waitForExistence(timeout: 5), "No link field")
        field.tap()
        field.typeText(link)
        app.navigationBars["Import From a Website"].buttons["Next"].tap()

        // The review screen, with the page's title and its ingredients.
        let name = app.textFields["Recipe name"]
        XCTAssertTrue(name.waitForExistence(timeout: 60), "The Fry's recipe didn't reach the review screen")
        XCTAssertEqual(name.value as? String, "Plant-based Calzones")
        XCTAssertFalse(app.buttons["ImportFailedWriteOwn"].exists, "The import failed")
        snap(app, "Fry's recipe imported")
    }
}
