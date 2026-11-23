import XCTest

/// Skipping every item of a component means the household has them, not that it skips the dish
/// (Merrill, Nov 22: Milk skipped, and Shop said "Not making this" over the Cream Sauce Base).
/// The sauce stays made on Shop, on the recipe page and on the cooking screen.
final class ShopComponentQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 600
    }

    private let recipe = "Creamy Penne with Peas"
    private let sauce = "Cream Sauce Base"

    @MainActor
    func testSkippingEveryPartOfASauceStillMakesIt() throws {
        let name = recipe
        let sauce = sauce
        // Cream Sauce Base is made from butter, flour and milk by default, so it shows on Shop as
        // a component. Butter and flour are had this week through the API; Milk through Shop.
        let own = try ownHousehold("component") { own in
            try await own.shopAtWalmart()
            let id = try await own.addRecipe(
                name,
                ingredients: [("Penne", "6", "oz"), ("Frozen Peas", "1", "cup"), (sauce, "2", "tbsp")],
                steps: [
                    "Boil the penne until tender, adding the peas for the last minute.",
                    "Stir the cream sauce base into the pan until thick.", "Toss the penne and peas in the sauce.",
                ])
            try await own.plan(id, weeksAhead: 0)
            let parts = try await own.groceryItems(weeksAhead: 0).filter { $0.component == sauce }
            XCTAssertEqual(parts.count, 3, "Cream Sauce Base isn’t made from three items: \(parts)")
            for part in parts where part.name != "Milk" {
                try await own.haveThisWeek(key: part.key, name: part.name, weeksAhead: 0)
            }
        }
        let app = launchSignedIn(as: own)
        app.buttons["Shop"].firstMatch.tap()

        let header = app.descendants(matching: .any)["shop.component.\(sauce)"].firstMatch
        XCTAssertTrue(header.waitForExistence(timeout: 30), "No \(sauce) on Shop")
        let milkOptions = app.buttons["Remove Milk"].firstMatch
        reveal(milkOptions, in: app)
        XCTAssertTrue(milkOptions.waitForExistence(timeout: 10), "No Remove for Milk")
        milkOptions.tap()
        let have = app.buttons["Have It This Week"].firstMatch
        XCTAssertTrue(have.waitForExistence(timeout: 5), "No Have It This Week for Milk")
        have.tap()

        // Every part is skipped now, and the sauce is still made: nothing to buy.
        let nothing = NSPredicate(format: "label CONTAINS 'Nothing to buy'")
        XCTAssertEqual(
            XCTWaiter.wait(for: [expectation(for: nothing, evaluatedWith: header)], timeout: 20), .completed,
            "\(sauce) doesn’t say Nothing to buy: \(header.label)")
        XCTAssertFalse(header.label.contains("Not making this"), "Skipping every part said Not making this")
        XCTAssertFalse(
            app.buttons["Make \(sauce) Again"].exists, "Skipping every part offered to make the sauce again")
        reveal(header, in: app)
        sleep(1)
        snap(app, "Shop › Sauce with every item had (\(mode))")

        // The recipe page and the cooking screen still make it.
        app.buttons.matching(NSPredicate(format: "label == 'Menu'")).firstMatch.tap()
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 10), "The Menu tab didn’t open")
        open(name, in: app)
        let start = app.buttons["Start Cooking"].firstMatch
        XCTAssertTrue(start.waitForExistence(timeout: 15), "\(name) didn’t open")
        XCTAssertFalse(
            app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Not making this'")).firstMatch.exists,
            "The recipe page says Not making this")
        start.tap()
        let welcome = app.staticTexts["Welcome to Cooking with DinnerOS"]
        if welcome.waitForExistence(timeout: 4) {
            app.buttons["Skip"].firstMatch.tap()
            _ = waitGone(welcome, timeout: 5)
        }
        XCTAssertTrue(openedCooking(name, in: app), "Start Cooking didn’t open \(name)")
        let steps =
            app.segmentedControls.buttons["Steps"].firstMatch.exists
            ? app.segmentedControls.buttons["Steps"].firstMatch : app.buttons["Steps"].firstMatch
        if steps.exists, steps.isHittable { steps.tap() }
        let sauceStep = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label CONTAINS[c] 'cream sauce base' OR label CONTAINS[c] 'butter'"))
            .firstMatch
        XCTAssertTrue(sauceStep.waitForExistence(timeout: 10), "The sauce’s step isn’t in the cooking steps")
        XCTAssertFalse(
            app.staticTexts["Nothing to do here. You skip everything in this step."].exists,
            "A cooking step says it’s skipped")
        XCTAssertFalse(
            app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Not making this'")).firstMatch.exists,
            "The cooking screen says Not making this")
        app.terminate()
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

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
        for _ in 0..<12 where card == nil {
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
