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
        } else {
            // An iPad: the steps column opens at its top, with Step 1 whole under the bar (Hark's
            // build 123 review, item 1).
            let first = stepCard(1, in: app)
            XCTAssertTrue(first.waitForExistence(timeout: 10), "The steps column has no Step 1")
            let bar = CookingQAChecks.barBottom(app)
            XCTAssertTrue(
                first.isHittable && first.frame.minY >= bar,
                "Step 1 isn’t whole under the bar when the cooking screen opens: it’s at \(first.frame), the bar ends at \(bar)")
            // Then the later steps, so every config has them (build 122 round 4, Hark's item 15b;
            // the top of this screen is ScreensQATests' "Cooking › imported"): brought up to rest
            // with Step 3's start just under the bar, never a step cut off by the fade.
            bringToTop(stepCard(3, in: app), in: app, column: 0.75)
            XCTAssertTrue(openedCooking(recipe, in: app), "Scrolling the steps left \(recipe)’s cooking screen")
            let third = stepCard(3, in: app)
            XCTAssertTrue(
                third.exists && third.frame.minY >= CookingQAChecks.barBottom(app),
                "Step 3 rests under the bar at \(third.frame)")
            snap(app, "Start Cooking › Steps")
            scrollBackCooking(app, column: 0.75)
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

    /// Merrill's dinner on Nov 23 (TestFlight 120, iPad in landscape): Chicken Penne al Limone's
    /// cooking screen showed a blank band where the photo goes, and rows went missing from the
    /// ingredient list as it scrolled. Both were drawn fully transparent by the fade under the top
    /// bar, so only the pixels tell. Here the photo must show with Ingredients right under it, and
    /// every one of the recipe's 13 ingredients must be on a row that's drawn.
    @MainActor
    func testCookingShowsThePhotoAndEveryIngredient() throws {
        let recipe = QARecipeFixture.lemonPenne
        let own = try ownHousehold("cookphoto", recipe: "") { own in
            _ = try await own.importRecipe(recipe)
        }
        let app = launchSignedIn(as: own)
        open(recipe.name, in: app)
        let start = app.buttons["Start Cooking"].firstMatch
        XCTAssertTrue(start.waitForExistence(timeout: 15), "\(recipe.name) didn’t open")
        start.tap()
        let welcome = app.staticTexts["Welcome to Cooking with DinnerOS"]
        if welcome.waitForExistence(timeout: 4) {
            app.buttons["Skip"].firstMatch.tap()
            _ = waitGone(welcome, timeout: 5)
        }
        XCTAssertTrue(openedCooking(recipe.name, in: app), "Start Cooking didn’t open \(recipe.name)")
        let mode = ProcessInfo.processInfo.environment["QA_MODE"] ?? "light"
        let screen = "Cooking › \(recipe.name) (\(mode))"

        var problems = CookingQAChecks.checkPhoto(app, recipe: recipe.name, screen: screen) { capture(app) }
        snap(app, "Cooking › a recipe with a photo")
        // The ingredients are the left column on an iPad and the whole page on a phone.
        let pad = UIDevice.current.userInterfaceIdiom == .pad
        problems += CookingQAChecks.checkEveryIngredient(
            app, expected: recipe.ingredients.map(\.name), screen: screen, column: pad ? 0.2 : 0.5
        ) { capture(app) }
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
        // Signed out, so the next test signs in to the shared test household, not this one.
        if app.buttons["Done"].firstMatch.exists { app.buttons["Done"].firstMatch.tap() }
        signOut(app)
        app.terminate()
    }

    /// A step on the cooking screen's steps column: one element, labelled "Step 3, …".
    @MainActor
    private func stepCard(_ index: Int, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "Step \(index),")).firstMatch
    }

    /// Drags slowly, and holds at the end so nothing coasts, until `element`'s top rests a fade's
    /// height under the bar.
    @MainActor
    private func bringToTop(_ element: XCUIElement, in app: XCUIApplication, column: CGFloat) {
        for _ in 0..<6 {
            guard element.exists else {
                scrollCooking(app, column: column)
                continue
            }
            let target = CookingQAChecks.barBottom(app) + 40
            let distance = element.frame.minY - target
            guard abs(distance) > 12 else { return }
            let step = max(min(distance, app.frame.height * 0.5), -app.frame.height * 0.5)
            let y = app.frame.height * 0.75
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0))
                .withOffset(CGVector(dx: 0, dy: y))
            let to = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0))
                .withOffset(CGVector(dx: 0, dy: y - step))
            from.press(forDuration: 0.05, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.3)
            sleep(1)
        }
    }

    /// Scrolls the cooking screen up by about half a screen, dragging at `column` across it: the
    /// left part is the ingredients on an iPad and the whole page on a phone, the right part an
    /// iPad's steps. A drag on the screen rather than in a scroll view found by query: the recipe
    /// page's scroll view is still there under it.
    @MainActor
    private func scrollCooking(_ app: XCUIApplication, column: CGFloat = 0.3) {
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.8))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.3))
        start.press(forDuration: 0.05, thenDragTo: end)
        sleep(1)
    }

    /// Scrolls a column of the cooking screen back to its top, with drags that start mid-screen
    /// (a fast downward swipe on iPadOS 26 can make the app a window).
    @MainActor
    private func scrollBackCooking(_ app: XCUIApplication, column: CGFloat) {
        for _ in 0..<2 {
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.3))
            let end = app.coordinate(withNormalizedOffset: CGVector(dx: column, dy: 0.8))
            start.press(forDuration: 0.05, thenDragTo: end)
        }
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
