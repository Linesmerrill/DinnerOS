import XCTest

/// Fills the App Review account the way a person would (docs/review-account.md): sign in with
/// email, then for each recipe open Add a Recipe, paste its text, check the servings, times and
/// cuisine, choose its photo from the web search, and save.
///
/// Local only: it needs the screenshot API (REVIEW_ACCOUNT_* set, a fake Unsplash started with
/// -photos docs/review-account/photos.json). Pass TEST_RUNNER_QA_REVIEW_EMAIL,
/// TEST_RUNNER_QA_REVIEW_PASSWORD, TEST_RUNNER_QA_RECIPES (recipes.txt) and TEST_RUNNER_QA_PHOTOS
/// (photos.json). Recipes already in the household are skipped, so a rerun picks up where it
/// stopped.
final class ReviewAccountRecipesQATests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }

    /// The cuisine each recipe is filed under, by the picker's ID.
    private let cuisines = [
        "Salmon with Asparagus and Rice": "american", "Beef and Broccoli": "chinese",
        "Spaghetti Pomodoro": "italian", "Shredded Beef Tacos": "mexican", "Chicken Tikka Masala": "indian",
        "Margherita Pizza": "italian", "Chicken Skewers with Garlic Sauce": "lebanese",
        "Turkey and Bean Chili": "american",
    ]

    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 1800
    }

    @MainActor
    func testAddsTheReviewRecipesByHand() throws {
        let recipes = try String(
            contentsOfFile: XCTUnwrap(env["QA_RECIPES"], "Set TEST_RUNNER_QA_RECIPES"), encoding: .utf8
        )
        .components(separatedBy: "===\n").map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
        let photoData = try Data(
            contentsOf: URL(fileURLWithPath: XCTUnwrap(env["QA_PHOTOS"], "Set TEST_RUNNER_QA_PHOTOS")))
        let photos = try XCTUnwrap(JSONSerialization.jsonObject(with: photoData) as? [[String: String]])
        let app = signIn()

        for text in recipes {
            let name = String(text.prefix { $0 != "\n" })
            let photographer = try XCTUnwrap(
                photos.first { $0["recipe"] == name }?["photographer"], "No photo for \(name)")
            let cuisine = try XCTUnwrap(cuisines[name], "No cuisine for \(name)")
            if app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch.exists { continue }

            // The Week Menu button (…) holds Add a Recipe.
            app.buttons["Week Menu"].firstMatch.tap()
            let add = app.buttons["Add a Recipe"].firstMatch
            XCTAssertTrue(add.waitForExistence(timeout: 5), "No Add a Recipe in the Week Menu")
            add.tap()
            let importIt = app.buttons["AddChoice-import"]
            XCTAssertTrue(importIt.waitForExistence(timeout: 10), "No Add a Recipe choices")
            importIt.tap()
            app.buttons["Paste Text"].firstMatch.tap()
            let box = app.textViews["Recipe text"].firstMatch
            XCTAssertTrue(box.waitForExistence(timeout: 5))
            box.tap()
            box.typeText(text)
            app.buttons["Next"].firstMatch.tap()

            // The review: name, servings, times and cuisine as read, then the photo.
            let nameField = app.textFields["Recipe name"].firstMatch
            XCTAssertTrue(nameField.waitForExistence(timeout: 20), "\(name): the review didn't open")
            XCTAssertEqual(nameField.value as? String, name)
            let cuisineRow = app.buttons["RecipeCuisine"].firstMatch
            reveal(cuisineRow, in: app)
            sleep(2)  // the cuisine guess waits for a pause after the name
            cuisineRow.tap()
            let option = app.buttons["CuisineOption-\(cuisine)"].firstMatch
            if !option.waitForExistence(timeout: 5) {
                app.searchFields.firstMatch.tap()
                app.searchFields.firstMatch.typeText(cuisine)
            }
            XCTAssertTrue(option.waitForExistence(timeout: 5), "\(name): no \(cuisine) cuisine")
            option.tap()
            if app.buttons["CuisineDone"].waitForExistence(timeout: 2) { app.buttons["CuisineDone"].tap() }

            scrollBack(app)
            scrollBack(app)
            let choose = app.buttons["Choose Photo"].firstMatch
            reveal(choose, in: app)
            choose.tap()
            let pick = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", photographer)).firstMatch
            XCTAssertTrue(pick.waitForExistence(timeout: 15), "\(name): \(photographer)'s photo isn't in the search")
            pick.tap()
            XCTAssertTrue(
                app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", photographer)).firstMatch
                    .waitForExistence(timeout: 15), "\(name): the photo credit didn't show")
            snap(app, "Review recipe › \(name)")

            app.buttons["Save"].firstMatch.tap()
            // Saving opens the new recipe.
            XCTAssertTrue(app.buttons["Start Cooking"].firstMatch.waitForExistence(timeout: 20), "\(name): didn't save")
            sleep(1)
            let back = app.navigationBars.buttons.element(boundBy: 0)
            if back.exists { back.tap() }
            sleep(2)
        }
    }

    @MainActor
    private func signIn() -> XCUIApplication {
        let app = XCUIApplication()
        app.launch()
        if app.buttons["Week Menu"].waitForExistence(timeout: 8) { return app }
        let open = app.buttons["signIn.email.open"]
        XCTAssertTrue(open.waitForExistence(timeout: 15), "No Sign in with email")
        open.tap()
        let field = app.textFields["signIn.email.address"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText(env["QA_REVIEW_EMAIL"] ?? "")
        let secure = app.secureTextFields["signIn.email.password"]
        secure.tap()
        secure.typeText(env["QA_REVIEW_PASSWORD"] ?? "")
        app.buttons["signIn.email.submit"].tap()
        let start = app.buttons["Start Planning"].firstMatch
        if start.waitForExistence(timeout: 20) { start.tap() }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 20), "The Menu didn't open")
        for title in ["Thanks", "Skip"] where app.buttons[title].firstMatch.exists {
            app.buttons[title].firstMatch.tap()
        }
        return app
    }
}
