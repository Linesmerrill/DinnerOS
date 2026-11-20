import XCTest

/// Add a Recipe from the Menu's ⋯ menu shows four choices, and each opens the right screen
/// (decision 709). Nothing is saved, so it's safe beside other simulators on one API.
final class AddRecipeChooserQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    @MainActor
    private func openChooser(_ app: XCUIApplication) {
        app.buttons["Week Menu"].tap()
        app.buttons["Add a Recipe"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Add a Recipe"].waitForExistence(timeout: 5), "No Add a Recipe")
    }

    /// Back from a pushed choice to the list of choices.
    @MainActor
    private func back(_ app: XCUIApplication, from title: String) {
        app.navigationBars[title].buttons.firstMatch.tap()
        XCTAssertTrue(app.buttons["AddChoice-write"].waitForExistence(timeout: 5), "Back didn’t return to the choices")
    }

    @MainActor
    func testTheFourChoicesEachOpenTheirScreen() throws {
        let app = launchSignedIn()
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "No Menu")
        openChooser(app)

        // Four choices, in order, each with a subtitle.
        let ids = ["AddChoice-write", "AddChoice-import", "AddChoice-shared", "AddChoice-companion"]
        let titles = ["Write Your Own", "Import From a Website", "Browse Shared Recipes", "Add a Companion"]
        var lastY = -CGFloat.infinity
        for (id, title) in zip(ids, titles) {
            let choice = app.buttons[id]
            XCTAssertTrue(choice.waitForExistence(timeout: 5), "No \(id)")
            XCTAssertTrue(choice.label.hasPrefix(title), "\(id) says \(choice.label)")
            XCTAssertGreaterThan(choice.label.count, title.count + 5, "\(id) has no subtitle")
            XCTAssertGreaterThan(choice.frame.minY, lastY, "\(id) is out of order")
            lastY = choice.frame.minY
        }
        snap(app, "Add a Recipe, choices (\(mode))")

        // Write Your Own: the blank form.
        app.buttons["AddChoice-write"].tap()
        XCTAssertTrue(app.textFields["Recipe name"].waitForExistence(timeout: 5), "Write Your Own has no form")
        XCTAssertTrue(app.navigationBars["Write Your Own"].exists)
        snap(app, "Add a Recipe, Write Your Own (\(mode))")
        back(app, from: "Write Your Own")

        // Import From a Website: a link, and Paste Text is there too.
        app.buttons["AddChoice-import"].tap()
        XCTAssertTrue(app.navigationBars["Import From a Website"].waitForExistence(timeout: 5), "No import screen")
        XCTAssertTrue(app.textFields["https://…"].waitForExistence(timeout: 5), "Import has no link field")
        let pasteText = app.buttons["Paste Text"].firstMatch
        XCTAssertTrue(pasteText.exists, "Import has no Paste Text")
        pasteText.tap()
        XCTAssertTrue(app.textViews.firstMatch.waitForExistence(timeout: 5), "Paste Text has no text box")
        snap(app, "Add a Recipe, Import From a Website (\(mode))")
        back(app, from: "Import From a Website")

        // Add a Companion: the companion editor, as its own sheet over the choices.
        app.buttons["AddChoice-companion"].tap()
        let companion = app.descendants(matching: .any).matching(
            NSPredicate(format: "identifier IN %@", ["CompanionName", "CompanionEditor"])
        ).firstMatch
        XCTAssertTrue(companion.waitForExistence(timeout: 5), "Add a Companion opened something else")
        snap(app, "Add a Recipe, Add a Companion (\(mode))")
        app.navigationBars["New Companion"].buttons["Cancel"].tap()
        XCTAssertTrue(
            app.buttons["AddChoice-shared"].waitForExistence(timeout: 5), "Cancel didn’t return to the choices")

        // Browse Shared Recipes closes the choices and opens Shared Recipes.
        app.buttons["AddChoice-shared"].tap()
        XCTAssertTrue(waitGone(app.navigationBars["Add a Recipe"], timeout: 5), "The choices didn’t close")
        XCTAssertTrue(app.navigationBars["Shared Recipes"].waitForExistence(timeout: 10), "Shared Recipes didn’t open")
        snap(app, "Add a Recipe, Browse Shared Recipes (\(mode))")
        app.navigationBars.buttons.firstMatch.tap()

        // Cancel closes it.
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 10))
        openChooser(app)
        app.navigationBars["Add a Recipe"].buttons["Cancel"].tap()
        XCTAssertTrue(waitGone(app.navigationBars["Add a Recipe"], timeout: 5), "Cancel didn’t close the choices")
        XCTAssertTrue(app.buttons["Week Menu"].exists)
    }
}
