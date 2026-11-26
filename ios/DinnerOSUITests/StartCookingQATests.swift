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
            // Then Step 3 brought up to rest just under the bar, as on an iPad, so every config has
            // the same shot (Hark's review, build 121 round 2, item 1).
            bringToTop(stepCard(3, in: app), in: app, column: 0.5)
            let third = stepCard(3, in: app)
            XCTAssertTrue(
                third.exists && third.frame.minY >= CookingQAChecks.barBottom(app),
                "Step 3 rests under the bar at \(third.frame)")
            snap(app, "Start Cooking › Steps scrolled")
            // Back to the top, so the Ingredients tab is shown as it opens.
            for _ in 0..<4 where !stepCard(1, in: app).isHittable { scrollBackCooking(app, column: 0.5) }
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
                "Step 1 isn’t whole under the bar when the cooking screen opens: it’s at \(first.frame), the bar ends at \(bar)"
            )
            // The steps as they open, both columns at their tops: the Steps heading and Step 1
            // (Hark's review, build 121 round 2, item 1: this shot was the steps scrolled to Step 3).
            XCTAssertTrue(app.staticTexts["Steps"].firstMatch.exists, "The steps column has no Steps heading")
            snap(app, "Start Cooking › Steps")
            // Then the later steps, so every config has them (build 122 round 4, Hark's item 15b):
            // brought up to rest with Step 3's start just under the bar, never a step cut off by
            // the fade, in a shot named for it.
            bringToTop(stepCard(3, in: app), in: app, column: 0.75)
            XCTAssertTrue(openedCooking(recipe, in: app), "Scrolling the steps left \(recipe)’s cooking screen")
            let third = stepCard(3, in: app)
            XCTAssertTrue(
                third.exists && third.frame.minY >= CookingQAChecks.barBottom(app),
                "Step 3 rests under the bar at \(third.frame)")
            snap(app, "Start Cooking › Steps scrolled")
            scrollBackCooking(app, column: 0.75)
        }

        // Partway down the ingredients, when there's more of them than fits: the list on a phone's
        // Ingredients tab, the left column on an iPad (named for it, build 121 round 2, item 1).
        let top = capture(app)
        scrollCooking(app)
        var scrolled = capture(app)
        if scrolled.isSameScreen(as: top) {
            scrollCooking(app)
            scrolled = capture(app)
        }
        if keepIfScrolled(scrolled, "Start Cooking › Ingredients scrolled", before: top) {
            // No heading left faded out under the bar over its rows (the same review, item 4).
            let problems = CookingQAChecks.checkHeadingsShow(
                app, screen: "Start Cooking › Ingredients scrolled", column: 0.3, image: scrolled.image)
            XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
        }

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
        // Carries on past a failed check, so the sign-out at the end always runs: left signed in to
        // this test's own household, the next test couldn't find its recipe (build 121 round 2).
        continueAfterFailure = true
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
        // On an SE, 240 points down left "Ingredients" right at the picker's edge, drawn faded out
        // over "Have Ready" (build 121 round 2): the list must settle with no heading blank. Then
        // back to the top for the whole-list check.
        if !pad && app.frame.height < 700 {
            CookingQAChecks.drag(app, column: 0.5, by: 240)
            sleep(2)
            problems += CookingQAChecks.checkHeadingsShow(
                app, screen: "\(screen), 240 points down", column: 0.5, image: capture(app).image)
            for _ in 0..<3 {
                app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
                    .press(
                        forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.9)))
            }
            sleep(1)
        }
        problems += CookingQAChecks.checkEveryIngredient(
            app, expected: recipe.ingredients.map(\.name), screen: screen, column: pad ? 0.2 : 0.5,
            fadedHeading: { self.keep($0, "Cooking › a recipe with a photo: a heading faded out", unique: false) }
        ) { capture(app) }
        // Signed out first, so the next test signs in to the shared test household, not this one.
        if app.buttons["Done"].firstMatch.exists { app.buttons["Done"].firstMatch.tap() }
        signOut(app)
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
        app.terminate()
    }

    /// One recipe, one time (Hark's build 121 review, round 3): the Menu card, the recipe page and
    /// the cooking screen all read the recipe's one total time.
    @MainActor
    func testTheMenuRecipePageAndCookingShowOneTime() throws {
        continueAfterFailure = true
        let recipe = QARecipeFixture.lemonPenne
        let own = try ownHousehold("cooktime", recipe: "") { own in
            let id = try await own.importRecipe(recipe)
            try await own.plan(id, weeksAhead: 0, day: MenuCardsQATests.nightsFromToday()[0])
        }
        let app = launchSignedIn(as: own)
        var shown: [(screen: String, text: String?)] = []

        let card = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", MenuQAChecks.cardPrefix))
            .buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(recipe.name),")).firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 30), "\(recipe.name)’s card isn’t on the Menu")
        shown.append(("The Menu card", card.exists ? card.label : nil))

        card.tap()
        let total = app.descendants(matching: .any)["Total Time"].firstMatch
        XCTAssertTrue(total.waitForExistence(timeout: 15), "The recipe page shows no Total Time")
        shown.append(("The recipe page", total.exists ? total.value as? String : nil))

        let start = app.buttons["Start Cooking"].firstMatch
        XCTAssertTrue(start.waitForExistence(timeout: 10), "\(recipe.name) has no Start Cooking")
        start.tap()
        let welcome = app.staticTexts["Welcome to Cooking with DinnerOS"]
        if welcome.waitForExistence(timeout: 4) {
            app.buttons["Skip"].firstMatch.tap()
            _ = waitGone(welcome, timeout: 5)
        }
        XCTAssertTrue(openedCooking(recipe.name, in: app), "Start Cooking didn’t open \(recipe.name)")
        let header = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH 'Serves'")).firstMatch
        shown.append(("The cooking screen", header.waitForExistence(timeout: 10) ? header.label : nil))

        let problems = QATimeChecks.problems(
            shown, expected: QARecipeTimes.minutes(recipe.name), recipe: recipe.name)
        if app.buttons["Done"].firstMatch.exists { app.buttons["Done"].firstMatch.tap() }
        signOut(app)
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
        app.terminate()
    }

    /// Merrill's dinner on Nov 24 (build 122, iPad in landscape): cilantro skipped for the tacos
    /// still showed struck through in their cooking list, and their Save Some for Later card said
    /// to use ¼ oz and save ¼ oz for Wednesday's pork bowls; crispy fried onions read "Use 1
    /// Crispy Fried Onions". Now the tacos list no cilantro anywhere, the pork bowls still use all
    /// of theirs and keep some for the lettuce cups, and the onions read as packs.
    @MainActor
    func testASkippedIngredientIsNotListedOrSavedForLater() throws {
        // Carries on past a failed check, so the sign-out at the end always runs.
        continueAfterFailure = true
        let tacos = QARecipeFixture.stirFryTacos, bowls = QARecipeFixture.sesamePorkBowls
        let cups = QARecipeFixture.lettuceCups
        // The manifest names it beside the shot (Hark's build 123 review).
        let skipped = "Cilantro"
        let own = try ownHousehold("cookskip", recipe: "") { own in
            let ids = try await own.importRecipes([tacos, bowls, cups])
            // Today and the next two nights, in this order (on a Saturday all three share it, and
            // the order they were planned in still holds).
            for (recipe, day) in zip([tacos, bowls, cups], MenuCardsQATests.nightsFromToday()) {
                try await own.plan(try XCTUnwrap(ids[recipe.name]), weeksAhead: 0, day: day)
            }
            try await own.skipInDish(skipped, recipeID: try XCTUnwrap(ids[tacos.name]))
        }
        let app = launchSignedIn(as: own)
        var problems: [String] = []

        if let first = cookingList(
            tacos.name, in: app, shot: "Start Cooking › an ingredient skipped here and used later (\(skipped))")
        {
            let rows = first.rows.filter { $0.localizedCaseInsensitiveContains("cilantro") }
            if !rows.isEmpty { problems.append("\(tacos.name) skip cilantro, but its list has \(rows)") }
            if let card = first.card {
                if card.contains("Cilantro") {
                    problems.append("\(tacos.name)’s Save Some for Later names cilantro: \(card)")
                }
                // One lime story (Hark's build 123 review): step 1 halves it, step 3 squeezes
                // half, Have Ready says ½ is left over, and the card uses ½.
                if !card.contains("Lime: Use ½ Lime. Save 1 Lime for \(bowls.name)") {
                    problems.append("\(tacos.name)’s Save Some for Later doesn’t use ½ lime: \(card)")
                }
                if !card.contains("Crispy Fried Onions: Use 1 of the 2 packs. Save 1 pack for \(bowls.name)") {
                    problems.append("\(tacos.name)’s crispy fried onions don’t read as packs: \(card)")
                }
            } else {
                problems.append("\(tacos.name) has no Save Some for Later card")
            }
        } else {
            problems.append("\(tacos.name)’s cooking screen didn’t open")
        }

        if let later = cookingList(bowls.name, in: app) {
            if !later.rows.contains(where: { $0.localizedCaseInsensitiveContains("cilantro") }) {
                problems.append("\(bowls.name) use cilantro, but its list has none: \(later.rows)")
            }
            if let card = later.card {
                if !card.contains("Cilantro: Use ¼ oz. Save ¼ oz for \(cups.name)") {
                    problems.append(
                        "\(bowls.name)’s Save Some for Later doesn’t keep cilantro for \(cups.name): \(card)")
                }
            } else {
                problems.append("\(bowls.name) has no Save Some for Later card")
            }
        } else {
            problems.append("\(bowls.name)’s cooking screen didn’t open")
        }

        // Signed out first, so the next test signs in to the shared test household, not this one.
        signOut(app)
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
        app.terminate()
    }

    /// Opens `recipe`'s cooking screen from All Meals and reads its Save Some for Later card and
    /// its ingredient rows, then goes back to the Menu. `nil` when it didn't open.
    @MainActor
    private func cookingList(
        _ recipe: String, in app: XCUIApplication, shot: String? = nil
    ) -> (card: String?, rows: [String])? {
        open(recipe, in: app)
        let start = app.buttons["Start Cooking"].firstMatch
        guard start.waitForExistence(timeout: 15) else { return nil }
        start.tap()
        let welcome = app.staticTexts["Welcome to Cooking with DinnerOS"]
        if welcome.waitForExistence(timeout: 4) {
            app.buttons["Skip"].firstMatch.tap()
            _ = waitGone(welcome, timeout: 5)
        }
        guard openedCooking(recipe, in: app) else { return nil }
        let card = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH %@", "Save Some for Later")).firstMatch
        let cardLabel = card.waitForExistence(timeout: 10) ? card.label : nil
        // The card as a person sees it (Hark's build 121 review, round 4): brought up under the bar,
        // in the ingredients column.
        if let shot, cardLabel != nil {
            bringToTop(card, in: app, column: UIDevice.current.userInterfaceIdiom == .pad ? 0.2 : 0.5)
            snap(app, shot)
        }
        // One snapshot, walked here: asking each row for its label is a query each.
        var rows: [String] = []
        var stack = (try? app.snapshot()).map { [$0] } ?? []
        while let element = stack.popLast() {
            stack.append(contentsOf: element.children)
            if element.identifier == QAMarkers.cookIngredient { rows.append(element.label) }
        }
        if app.buttons["Done"].firstMatch.exists { app.buttons["Done"].firstMatch.tap() }
        let back = backButton(app)
        if back.waitForExistence(timeout: 5) { back.tap() }
        return (cardLabel, rows)
    }

    /// A step on the cooking screen's steps column: one element, labelled "Step 3, …".
    @MainActor
    private func stepCard(_ index: Int, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "Step \(index),"))
            .firstMatch
    }

    /// Drags slowly, and holds at the end so nothing coasts, until `element`'s top rests a little
    /// under the bar. No drag is shorter than `minDrag`: a shorter one is taken as a tap, and a
    /// tap on a step marks it as the one you're on, which scrolled Step 3 up under the bar on the
    /// upright iPad (build 121 round 2). A short correction goes past and comes back instead.
    @MainActor
    private func bringToTop(_ element: XCUIElement, in app: XCUIApplication, column: CGFloat) {
        let minDrag: CGFloat = 50
        for _ in 0..<10 {
            guard element.exists else {
                scrollCooking(app, column: column)
                continue
            }
            let bar = CookingQAChecks.barBottom(app)
            let top = element.frame.minY
            // Close enough: whole under the bar, its start near the top.
            if top >= bar + 8 && top <= bar + 90 { return }
            let distance = top - (bar + 40)
            var step = max(min(distance, app.frame.height * 0.4), -app.frame.height * 0.4)
            if abs(step) < minDrag { step += step > 0 ? minDrag : -minDrag }
            // Up from low on the screen, or back down from high on it.
            let y = step > 0 ? app.frame.height * 0.75 : app.frame.height * 0.3
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
        // Back from an earlier recipe, the search still holds its name.
        if let typed = field.value as? String, !typed.isEmpty, typed != field.placeholderValue {
            // The field's own clear button; else delete from the end, where a tap puts the cursor.
            let clear = app.buttons["Clear text"].firstMatch
            if clear.exists, clear.isHittable {
                clear.tap()
            } else {
                field.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)).tap()
                field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: typed.count))
            }
            if !app.keyboards.firstMatch.waitForExistence(timeout: 2) { field.tap() }
        }
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
