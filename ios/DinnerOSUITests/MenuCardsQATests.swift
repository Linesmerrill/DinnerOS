import XCTest

/// Your Meals on a week like Merrill's on Nov 22 (build 122): three dinners Monday to Wednesday
/// and garlic bread with Monday's pasta, seen on the week's first day, so no night has passed.
///
/// Every card kept room for "Didn't make it?" and the stars, which only show once a meal's night
/// is over, so the Menu had an empty band of more than 100 points between the servings control
/// and Shared Recipes. Cards now keep that room only when a card in the week shows those rows
/// (`MealCardRows`). Since Nov 23 (Merrill) the stars show on every card, a meal still ahead
/// included, and only "How was it?" waits for the night. This checks the week at rest (stars on
/// all four cards), then marks a meal made and checks it asks how it was, with the cards still
/// lining up and still no band.
final class MenuCardsQATests: XCTestCase {
    private var problems: [String] = []

    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 600
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    @MainActor
    func testYourMealsRatingAndAddOnRowsWithNoBlankBand() throws {
        let photos = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3/image/"
        let own = try ownHousehold("menucards", recipe: "", name: "Our Kitchen") { own in
            let ids = try await own.importRecipes([
                ("Chicken Penne al Limone", photos + "63853599929fb724f40ed140-e3d4003e.jpg", false),
                ("One-Pan Beef Stir-Fry Tacos", photos + "6267fa613f1203a0fa0c0004-bcdc9530.jpg", false),
                ("Sesame-Soy Pork Bowls", photos + "5fa06a5f20b9661c0d5e342f-9d881539-9859fadd.jpg", false),
                ("Garlic Bread", "https://dinneros.app/img/recipes/6aa943c326393a8c809cf023.jpg", true),
            ])
            for (name, day) in [
                ("Chicken Penne al Limone", "mon"), ("One-Pan Beef Stir-Fry Tacos", "tue"),
                ("Sesame-Soy Pork Bowls", "wed"), ("Garlic Bread", "mon"),
            ] {
                let id = try XCTUnwrap(ids[name], "\(name) wasn’t imported")
                try await own.plan(id, weeksAhead: 0, day: day)
            }
        }
        let app = launchSignedIn(as: own)
        let screen = "Menu › Your Meals (\(mode))"

        // The week's four cards, the garlic bread among them.
        let subtitle = app.staticTexts["3 meals planned · 1 add-on"]
        XCTAssertTrue(subtitle.waitForExistence(timeout: 30), "Your Meals doesn’t say 3 meals and 1 add-on")
        let cards = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", MenuQAChecks.cardPrefix))
        XCTAssertTrue(cards.firstMatch.waitForExistence(timeout: 15), "No meal cards on the Menu")
        XCTAssertEqual(cards.count, 4, "Your Meals should show 4 cards")
        let addOns = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", MenuQAChecks.addOnPrefix))
        XCTAssertEqual(addOns.count, 1, "The garlic bread’s card isn’t marked as the add-on")
        XCTAssertTrue(
            addOns.firstMatch.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Garlic Bread'")).count > 0,
            "The add-on card isn’t Garlic Bread’s")
        sleep(2)

        // At rest every night is ahead, and every card still shows its stars (Merrill, Nov 23):
        // the week shows how each meal was liked last time. Nothing asks "How was it?" yet.
        XCTAssertEqual(
            app.descendants(matching: .any).matching(identifier: MenuQAChecks.ratingRow).count, 4,
            "Every meal card should show its stars, a meal still ahead included")
        XCTAssertFalse(app.staticTexts["How was it?"].exists, "A meal whose night is still ahead asks how it was")
        problems += MenuQAChecks.checkAddOnCards(app, screen: screen)
        problems += MenuQAChecks.checkMealCardRatingRows(app, screen: screen)
        problems += MenuQAChecks.checkNoBlankBandUnderMeals(app, screen: screen)
        problems += MenuQAChecks.checkMealCardsLineUp(app, screen: screen)
        problems += MenuQAChecks.checkLoaded(app, screen: screen)
        snap(app, "Menu › Your Meals at rest (\(mode))")

        // Made already: the card asks how it was, and the week still has no band under it.
        let asking = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier ENDSWITH %@", MenuQAChecks.asksSuffix))
        // The Your Meals card's, not New to You's card for the same recipe.
        let photo = cards.buttons
            .matching(NSPredicate(format: "label BEGINSWITH 'Chicken Penne al Limone,'")).firstMatch
        XCTAssertTrue(photo.exists, "Chicken Penne al Limone’s card isn’t on the Menu")
        photo.press(forDuration: 1.2)
        let made = app.buttons["I Made It"].firstMatch
        if made.waitForExistence(timeout: 5) {
            made.tap()
            XCTAssertTrue(asking.firstMatch.waitForExistence(timeout: 10), "The card marked made isn’t one that asks")
            XCTAssertTrue(
                asking.firstMatch.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Chicken Penne'")).count > 0,
                "The card that asks isn’t Chicken Penne al Limone’s")
            let stars = asking.firstMatch.descendants(matching: .any)[MenuQAChecks.ratingRow]
            XCTAssertTrue(stars.waitForExistence(timeout: 10), "A meal marked made doesn’t ask how it was")
            XCTAssertTrue(app.staticTexts["How was it?"].exists, "The stars have no “How was it?”")
            // Past the undo, so the answer is the plan's and not only this screen's.
            sleep(9)
            problems += MenuQAChecks.checkMealCardRatingRows(app, screen: "\(screen), one made")
            problems += MenuQAChecks.checkNoBlankBandUnderMeals(app, screen: "\(screen), one made")
            problems += MenuQAChecks.checkMealCardsLineUp(app, screen: "\(screen), one made")
            snap(app, "Menu › Your Meals with a meal made (\(mode))")
        } else {
            problems.append("\(screen): no “I Made It” on the card’s long-press menu")
        }
        XCTAssertTrue(problems.isEmpty, problems.joined(separator: "\n"))
        app.terminate()
    }
}
