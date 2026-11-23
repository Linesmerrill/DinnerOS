import XCTest

/// Send to AnyList shows the paste guide every time, from every place it's offered, and its
/// button copies the list one item per line and says so before leaving for AnyList.
final class AnyListGuideQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 400
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    @MainActor
    func testTheGuideShowsFromEveryEntryAndCopiesTheList() throws {
        let app = launchSignedIn(as: try plannedHousehold("anylist"))

        // 1. The Grocery List's share icon is a menu: Send to AnyList first, Share List… second.
        openGroceryList(app)
        app.navigationBars["Grocery List"].buttons["Share"].tap()
        let send = app.buttons["Send to AnyList"].firstMatch
        XCTAssertTrue(send.waitForExistence(timeout: 3), "No Send to AnyList in the share menu")
        XCTAssertTrue(app.buttons["Share List…"].firstMatch.exists, "No Share List… in the share menu")
        snap(app, "Grocery List › Share menu (\(mode))")
        send.tap()
        checkGuide(app, from: "Grocery List › Share")

        app.buttons["Done"].firstMatch.tap()

        // 2. The Grocery List's ⋯ menu shows the same guide, every time.
        app.navigationBars["Grocery List"].buttons["More"].tap()
        app.buttons["Send to AnyList"].firstMatch.tap()
        checkGuide(app, from: "Grocery List › More")
        app.buttons["Done"].firstMatch.tap()
        app.navigationBars["Grocery List"].buttons["More"].tap()
        app.buttons["Send to AnyList"].firstMatch.tap()
        XCTAssertTrue(
            app.navigationBars["Add Your List to AnyList"].waitForExistence(timeout: 3),
            "The guide didn’t show a second time")
        app.buttons["Done"].firstMatch.tap()

        // Copy in the ⋯ menu says Copied too.
        app.navigationBars["Grocery List"].buttons["More"].tap()
        let copyItem = app.buttons["Copy"].firstMatch
        if copyItem.waitForExistence(timeout: 3) {
            copyItem.tap()
            assertCopiedHUD(app, "Grocery List › Copy › Copied (\(mode))")
        }

        // 3. The Shop tab's Export the List section.
        app.buttons["Shop"].firstMatch.tap()
        snap(app, "Shop tab before Send (\(mode))")
        let shopSend = app.buttons["Send to AnyList"].firstMatch
        reveal(shopSend, in: app, maxSwipes: 15)
        snap(app, "Shop › Export the List (\(mode))")
        shopSend.tap()
        checkGuide(app, from: "Shop › Export the List")

        // Copy List and Open AnyList, on this week's list: copied, said so, one item per line.
        let copy = app.buttons["Copy List and Open AnyList"]
        XCTAssertTrue(copy.isEnabled, "Copy is off with items on this week’s list")
        copy.tap()
        // AnyList's App Store page comes up next, so only the HUD's showing is checked here.
        assertCopiedHUD(app, "AnyList guide › Copied (\(mode))", checkGone: false)
        // What's copied (one item per line, "6 oz Carrots") is checked in GroceryExportTests:
        // reading the clipboard from here stops the run at iOS's Allow Paste prompt.
        // AnyList isn't on the simulator, so its App Store page opens. Come back.
        sleep(3)
        app.activate()
        if app.buttons["Done"].firstMatch.waitForExistence(timeout: 5) { app.buttons["Done"].firstMatch.tap() }
    }

    /// A household of its own with a dinner planned this week and next, so both weeks have a
    /// grocery list. Planning in the shared test household put the same Chicken Bowls on its
    /// next week again and again while the screenshots were taken (build 122).
    @MainActor
    private func plannedHousehold(_ name: String) throws -> QAOwnHousehold {
        try ownHousehold(name) { own in
            let dinner = try await own.addRecipe("Carrot Rice Bowls \(Int.random(in: 100...999))")
            try await own.plan(dinner, weeksAhead: 0)
            try await own.plan(dinner, weeksAhead: 1)
            try await own.shopAtWalmart()
        }
    }

    /// The Shop tab's Copy says Copied.
    @MainActor
    func testCopyShowsTheCopiedHUD() throws {
        let app = launchSignedIn(as: try plannedHousehold("anylistcopy"))
        app.buttons["Shop"].firstMatch.tap()
        let copy = app.buttons["Copy"].firstMatch
        reveal(copy, in: app, maxSwipes: 15)
        sleep(1)
        copy.tap()
        assertCopiedHUD(app, "Shop › Copy › Copied (\(mode))")
    }

    /// The Copied HUD shows, then goes away on its own.
    @MainActor
    private func assertCopiedHUD(_ app: XCUIApplication, _ name: String, checkGone: Bool = true) {
        // The run lengthens the HUD (QA_HUD_SECONDS) so XCTest's slow first look still sees it.
        let hud = app.descendants(matching: .any)["CopiedHUD"].firstMatch
        XCTAssertTrue(hud.waitForExistence(timeout: 3), "No Copied HUD after copying")
        snap(app, name)
        if checkGone { XCTAssertTrue(waitGone(hud, timeout: 10), "The Copied HUD didn’t go away") }
    }

    @MainActor
    private func openGroceryList(_ app: XCUIApplication) {
        app.buttons["Menu"].firstMatch.tap()
        let next = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Next Week'")).firstMatch
        XCTAssertTrue(next.waitForExistence(timeout: 10), "No Next Week on the Menu")
        next.tap()
        // Next week already has the household's dinner (plannedHousehold), so it has a list.
        let list = app.buttons["Grocery List"].firstMatch
        XCTAssertTrue(list.waitForExistence(timeout: 10), "No Grocery List button on the Menu")
        list.tap()
        XCTAssertTrue(app.navigationBars["Grocery List"].waitForExistence(timeout: 10))
    }

    /// The guide: its title, five numbered steps with pictures VoiceOver can name, the Recipe
    /// Import warning, and both buttons. Scrolls through it keeping a screenshot of each part.
    @MainActor
    private func checkGuide(_ app: XCUIApplication, from place: String) {
        XCTAssertTrue(
            app.navigationBars["Add Your List to AnyList"].waitForExistence(timeout: 5), "\(place): no guide")
        snap(app, "AnyList guide from \(place) (\(mode))")
        XCTAssertTrue(
            app.staticTexts["Don’t use AnyList Recipe Import from the share sheet. It reads your list as a recipe."]
                .exists, "\(place): no Recipe Import warning")
        let steps = [
            "Step 1. Open your list in AnyList and tap Add Item.",
            "Step 2. Tap the paste list icon at the top right, next to the book.",
            "Step 3. Tap the empty area, then tap Paste.",
            "Step 4. Each line becomes its own item. Tap the check mark.",
            "Step 5. Your items are added and sorted into aisles.",
        ]
        for (index, step) in steps.enumerated() {
            let element = app.descendants(matching: .any)[step].firstMatch
            reveal(element, in: app, maxSwipes: 6)
            XCTAssertTrue(element.exists, "\(place): missing \(step)")
            if index == 2 || index == 4 { snap(app, "AnyList guide › step \(index + 1) (\(mode))") }
        }
        XCTAssertEqual(
            app.images.matching(NSPredicate(format: "label BEGINSWITH 'AnyList' OR label BEGINSWITH 'The pasted'"))
                .count > 0, true, "\(place): the step pictures have no VoiceOver labels")
        XCTAssertTrue(app.buttons["Copy List and Open AnyList"].exists)
        XCTAssertTrue(app.buttons["Done"].firstMatch.exists)
    }
}
