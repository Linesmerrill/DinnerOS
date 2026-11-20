import XCTest

/// Start Cooking stays where a person can reach it while a recipe scrolls (build 119).
///
/// On iPhone Duo's cover screen (iOS 27) the top bar's buttons sit in a column beside the page,
/// and Start Cooking there showed only at the top and scrolled away. It is pinned to the bottom
/// bar there now. On every other iPhone it stays in the top bar, and on iPad in the bottom bar,
/// as before. QA_EXPECT_START_COOKING (top or bottom) says which this device should show.
final class StartCookingQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 400
    }

    @MainActor
    func testStartCookingStaysOnScreenWhileScrolling() throws {
        let env = ProcessInfo.processInfo.environment
        let recipe = env["QA_MANUAL_NAME"] ?? "Sip Fudge"
        let app = launchSignedIn()
        open(recipe, in: app)
        let start = app.buttons["Start Cooking"].firstMatch
        XCTAssertTrue(start.waitForExistence(timeout: 15), "\(recipe) didn’t open")
        snap(app, "Start Cooking › top of the page")

        for swipe in 1...6 {
            app.swipeUp()
            XCTAssertTrue(start.waitForExistence(timeout: 3), "Start Cooking left the screen after \(swipe) swipes")
            XCTAssertTrue(start.isHittable, "Start Cooking can’t be tapped after \(swipe) swipes")
            XCTAssertTrue(app.frame.contains(start.frame), "Start Cooking is off the screen after \(swipe) swipes")
        }
        snap(app, "Start Cooking › scrolled")

        // Where it sits: the bottom bar on the Duo's cover screen and iPad, the top bar elsewhere.
        let inTopBar = app.navigationBars.buttons["Start Cooking"].firstMatch.exists
        let lowerHalf = start.frame.midY > app.frame.midY
        switch env["QA_EXPECT_START_COOKING"] {
        case "bottom":
            XCTAssertTrue(lowerHalf && !inTopBar, "Start Cooking should be in the bottom bar, at \(start.frame)")
        case "top":
            XCTAssertTrue(!lowerHalf, "Start Cooking should be in the top bar, at \(start.frame)")
        default:
            break
        }

        start.tap()
        if app.buttons["Skip"].firstMatch.waitForExistence(timeout: 4) { app.buttons["Skip"].firstMatch.tap() }
        XCTAssertTrue(app.buttons["Done"].firstMatch.waitForExistence(timeout: 10), "Start Cooking didn’t open cooking")
        snap(app, "Start Cooking › cooking")
        app.buttons["Done"].firstMatch.tap()
        XCTAssertTrue(start.waitForExistence(timeout: 10), "Done didn’t go back to the recipe")
    }

    /// Searches All Meals on the Menu and opens the recipe, as a person would.
    @MainActor
    private func open(_ recipe: String, in app: XCUIApplication) {
        let field = app.textFields["Search meals"].firstMatch
        if !field.exists {
            let search = app.buttons["Search meals"].firstMatch
            reveal(search, in: app, maxSwipes: 12)
            search.tap()
        }
        XCTAssertTrue(field.waitForExistence(timeout: 5), "No meal search")
        field.tap()
        field.typeText(recipe)
        let done = app.toolbars.buttons["Done"].firstMatch
        if done.waitForExistence(timeout: 2) { done.tap() }
        let cards = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", recipe))
        var card: XCUIElement?
        for _ in 0..<10 where card == nil {
            card = cards.allElementsBoundByIndex.first { $0.isHittable }
            if card == nil { sleep(1) }
        }
        guard let card else {
            XCTFail("\(recipe) isn’t in All Meals")
            return
        }
        card.tap()
    }
}
