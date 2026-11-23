import XCTest

/// The freeze card on Shop counts whole dinners (Merrill, Nov 22: "Why does this say 12 bags to
/// freeze?"). The pantry had 9 oz of Ground Pork for a 10 oz dinner, so the list bought a 1.5 lb
/// pack to top up 1 oz. The card cut the rest into 1 oz "dinners" and capped them at 12 bags.
/// A dinner is the meal's whole 10 oz: two bags, and a little extra.
final class ShopPrepQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 600
    }

    @MainActor
    func testATopUpPackFreezesWholeDinners() throws {
        let own = try ownHousehold("prep") { own in
            try await own.shopAtWalmart()
            let pork = try await own.addRecipe(
                "Sesame Pork Bowls", ingredients: [("Ground Pork", "10", "oz"), ("Jasmine rice", "1", "cup")],
                steps: ["Brown the pork.", "Serve it over the rice."], cuisine: "Asian")
            let beef = try await own.addRecipe(
                "Beef Tacos", ingredients: [("Ground Beef", "10", "oz"), ("Flour Tortillas", "6", nil)],
                steps: ["Brown the beef.", "Fill the tortillas."], cuisine: "Mexican")
            try await own.plan(pork, weeksAhead: 0)
            try await own.plan(beef, weeksAhead: 0)
            try await own.addToPantry("Ground Pork", quantity: "9", unit: "oz")
            let items = try await own.groceryItems(weeksAhead: 0)
            let porkKey = try XCTUnwrap(items.first { $0.name == "Ground Pork" }?.key, "No Ground Pork to buy")
            let beefKey = try XCTUnwrap(items.first { $0.name == "Ground Beef" }?.key, "No Ground Beef to buy")
            try await own.chooseWalmartProduct(
                key: porkKey, productID: "168039751", name: "Ground Pork", size: "3/2", unit: "lb")
            try await own.chooseWalmartProduct(
                key: beefKey, productID: "824841960", name: "Ground Beef", size: "16", unit: "oz")
            try await own.sendToWalmart(weeksAhead: 0)
        }
        let app = launchSignedIn(as: own)
        app.buttons["Shop"].firstMatch.tap()

        let card = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH 'Put the groceries away' AND label CONTAINS 'to freeze'"))
            .firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 30), "No freeze card on Shop")
        XCTAssertTrue(card.label.contains("2 bags to freeze"), "The freeze card doesn’t say 2 bags: \(card.label)")
        XCTAssertFalse(card.label.contains("12 bags"), "The freeze card counts 1 oz bags: \(card.label)")
        XCTAssertTrue(card.label.contains("Ground Beef"), "Ground Beef’s extra isn’t on the card: \(card.label)")
        // The row says what's left after the week and how it splits, so the math checks out:
        // 24 oz bought, 1 oz kept out with the 9 oz on hand, 23 oz = 2 bags of 10 oz + 3 oz
        // (Hark, build 122 round 5).
        XCTAssertTrue(
            card.label.contains(
                "Ground Pork, 23 ounces left after this week. Freeze 2 bags of 10 ounces, 3 ounces extra"),
            "The pork row doesn’t say what’s left and how it splits: \(card.label)")
        XCTAssertFalse(card.label.contains("9 ounces"), "The pork row still says what was on hand: \(card.label)")
        // "Too little to freeze" comes after Ground Beef, the item it's about.
        XCTAssertTrue(
            card.label.contains("Ground Beef, 6 ounces extra. Too little to freeze"),
            "The note isn’t under Ground Beef: \(card.label)")
        XCTAssertTrue(
            app.staticTexts["23 oz left after this week"].exists || card.label.contains("23 ounces left after this week"))
        sleep(1)
        snap(app, "Shop › Freeze card (\(mode))")
        app.terminate()
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }
}
