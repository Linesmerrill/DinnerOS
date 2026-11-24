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
            app.staticTexts["23 oz left after this week"].exists
                || card.label.contains("23 ounces left after this week"))
        sleep(1)
        let opened = capture(app)
        keep(opened, "Shop › Freeze card (\(mode))")
        // What sits under the Walmart note's fade as Shop opens is seen whole in shots of its
        // own: Did You Order These?, then This Week's Cost down to Add Prices, each card brought
        // up under the week bar (Hark's review, build 121 round 2). Two cards don't fit above the
        // note together on an SE, so one shot each, on every phone.
        print("QA-INFO: \(FadeQAChecks.checkRest(app, screen: "Shop, as it opens", edges: .bottom))")
        var fades: [String] = []
        let cards: [(shot: String, top: XCUIElement, rows: [(String, XCUIElement)])] = [
            (
                "order", app.staticTexts["Did You Order These?"].firstMatch,
                [
                    ("Did You Order These?", app.staticTexts["Did You Order These?"].firstMatch),
                    ("Review Order", app.buttons["Review Order"].firstMatch),
                ]
            ),
            (
                "this week’s cost", app.staticTexts["This Week’s Cost"].firstMatch,
                [
                    ("This Week’s Cost", app.staticTexts["This Week’s Cost"].firstMatch),
                    ("Add Prices", app.buttons["Add Prices"].firstMatch),
                ]
            ),
        ]
        for card in cards {
            let screen = "Shop › Freeze card › \(card.shot)"
            guard card.top.exists else {
                fades.append("\(screen): it isn’t on the Shop screen")
                continue
            }
            FadeQAChecks.bringUnderTopBars(card.top, in: app)
            let shot = capture(app)
            for (what, row) in card.rows {
                fades += FadeQAChecks.checkClear(row, what: what, in: app, screen: screen, image: shot.image)
            }
            keep(shot, "\(screen) (\(mode))")
        }
        // The end of the list: its last rows rest whole above the Walmart note, not ghosted in
        // its fade (Add Prices on a large iPhone).
        FadeQAChecks.scrollToEnd(app) { capture(app, settle: false) }
        fades += FadeQAChecks.checkRest(app, screen: "Shop, scrolled to the end", edges: .bottom)
        XCTAssertTrue(fades.isEmpty, fades.joined(separator: "\n"))
        app.terminate()
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }
}
