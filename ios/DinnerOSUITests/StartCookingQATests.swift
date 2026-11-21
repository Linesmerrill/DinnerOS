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
        // The imported recipe: long enough that its cooking screen scrolls everywhere. The recipe
        // page itself is ScreensQATests' to show, so nothing here is a picture of it (Hark's
        // review, build 122 item 13b: these shots were copies of the recipe page).
        let recipe = env["QA_IMPORTED_NAME"] ?? env["QA_MANUAL_NAME"] ?? "Street Cart-Style Chicken Bowls"
        let app = launchSignedIn()
        open(recipe, in: app)
        let start = app.buttons["Start Cooking"].firstMatch
        XCTAssertTrue(start.waitForExistence(timeout: 15), "\(recipe) didn’t open")

        for swipe in 1...6 {
            app.swipeUp()
            XCTAssertTrue(start.waitForExistence(timeout: 3), "Start Cooking left the screen after \(swipe) swipes")
            XCTAssertTrue(start.isHittable, "Start Cooking can’t be tapped after \(swipe) swipes")
            XCTAssertTrue(app.frame.contains(start.frame), "Start Cooking is off the screen after \(swipe) swipes")
        }

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

        // From the scrolled page, Start Cooking opens this recipe's cooking screen: the walkthrough
        // first on a fresh install (ScreensQATests keeps that), then the recipe itself.
        start.tap()
        let welcome = app.staticTexts["Welcome to Cooking with DinnerOS"]
        if welcome.waitForExistence(timeout: 4) {
            app.buttons["Skip"].firstMatch.tap()
            _ = waitGone(welcome, timeout: 5)
        }
        XCTAssertTrue(openedCooking(recipe, in: app), "Start Cooking didn’t open \(recipe)’s cooking screen")

        // A phone shows the steps on a tab of their own; an iPad shows them beside the
        // ingredients.
        let steps =
            app.segmentedControls.buttons["Steps"].firstMatch.exists
            ? app.segmentedControls.buttons["Steps"].firstMatch : app.buttons["Steps"].firstMatch
        if steps.exists, steps.isHittable {
            steps.tap()
            sleep(1)
            XCTAssertTrue(openedCooking(recipe, in: app), "The Steps tab left \(recipe)’s cooking screen")
            snap(app, "Start Cooking › Steps")
            let ingredients =
                app.segmentedControls.buttons["Ingredients"].firstMatch.exists
                ? app.segmentedControls.buttons["Ingredients"].firstMatch : app.buttons["Ingredients"].firstMatch
            ingredients.tap()
            sleep(1)
        }

        // Partway down the cooking screen, when there's more of it than fits.
        let top = capture(app)
        scrollCooking(app)
        var scrolled = capture(app)
        if scrolled.isSameScreen(as: top) {
            scrollCooking(app)
            scrolled = capture(app)
        }
        keepIfScrolled(scrolled, "Start Cooking › scrolled", before: top)

        // On the Duo's cover screen the rail drops Done once the cooking screen scrolls (build
        // 122): back to the top, where it shows.
        let done = app.buttons["Done"].firstMatch
        if !done.exists {
            print("QA-NOTE: Done isn’t on screen on the scrolled cooking screen")
            for _ in 0..<3 where !done.exists { scrollBack(app) }
        }
        done.tap()
        XCTAssertTrue(start.waitForExistence(timeout: 10), "Done didn’t go back to the recipe")
    }

    /// Scrolls the cooking screen up by about half a screen, dragging in its left part: the
    /// ingredients on an iPad, the whole page on a phone. A drag on the screen rather than in a
    /// scroll view found by query: the recipe page's scroll view is still there under it.
    @MainActor
    private func scrollCooking(_ app: XCUIApplication) {
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.8))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.3))
        start.press(forDuration: 0.05, thenDragTo: end)
        sleep(1)
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
        for attempt in 0..<12 where card == nil {
            card = cards.allElementsBoundByIndex.first { $0.isHittable }
            if card == nil {
                // The result can sit below the fold under the bars (the Duo's cover screen): bring
                // it up once, as ScreensQATests does.
                if attempt == 5, cards.firstMatch.exists { reveal(cards.firstMatch, in: app, maxSwipes: 6) }
                sleep(1)
            }
        }
        guard let card else {
            XCTFail("\(recipe) isn’t in All Meals")
            return
        }
        card.tap()
    }
}
