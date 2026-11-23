import XCTest

/// Reporting and hiding in Shared Recipes (decisions 686 and 744): a person reports a recipe
/// another household shared and never sees it again, then hides that cook and none of the
/// cook's recipes show.
///
/// Each run makes its own two households on the local development server, so simulators can
/// run it at once against one API (QA_API_BASE, default port 8082).
final class ReportRecipeQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testReportARecipeThenHideItsCook() async throws {
        let n = Int.random(in: 10000...99999)
        let reported = "Lemon Herb Pilaf \(n)"
        let hidden = "Saffron Pilaf \(n)"
        let other = "Mushroom Pilaf \(n)"
        let cookSubject = QAPeople.subject()
        let cook = try await QAAPI.signIn(subject: cookSubject)
        let cookHousehold = try await cook.createHousehold(name: "\(QAPeople.kitchen(forSubject: cookSubject)) \(n)")
        for name in [reported, hidden, other] {
            try await cook.shareTypedRecipe(householdID: cookHousehold, name: name)
        }
        let subject = QAPeople.subject()
        let me = try await QAAPI.signIn(subject: subject)
        _ = try await me.createHousehold(name: "\(QAPeople.kitchen(forSubject: subject)) \(n)")

        let app = XCUIApplication()
        applyOrientation()
        app.launch()
        if !app.textFields["Subject"].waitForExistence(timeout: 8) {
            if app.buttons["Thanks"].waitForExistence(timeout: 3) { app.buttons["Thanks"].tap() }
            signOut(app)
        }
        signIn(app, subject: subject)
        if app.buttons["Start Planning"].waitForExistence(timeout: 10) { app.buttons["Start Planning"].tap() }
        XCTAssertTrue(app.buttons["Menu"].firstMatch.waitForExistence(timeout: 20), "No Menu after sign-in")
        let entry = app.staticTexts["Shared Recipes"].firstMatch
        reveal(entry, in: app, maxSwipes: 10)
        // The Menu has several ways to Shared Recipes, and on an SE (iOS 27) the first can sit under
        // the pinned week strip, where a tap lands on the strip: use the first one below it.
        let strip = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'This Week'")).firstMatch
        let stripBottom = strip.exists ? strip.frame.maxY : 0
        let links = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Shared Recipes'"))
        let link = links.allElementsBoundByIndex.first { $0.frame.minY > stripBottom && $0.isHittable } ?? entry
        link.tap()
        XCTAssertTrue(app.navigationBars["Shared Recipes"].waitForExistence(timeout: 10), "Shared Recipes didn’t open")

        // 1. Report one recipe: it says thanks, and the recipe is gone from search.
        open(reported, in: app)
        app.buttons["catalogRecipe.more"].firstMatch.tap()
        app.buttons["Report Recipe"].firstMatch.tap()
        XCTAssertTrue(app.buttons["It’s spam or an ad"].firstMatch.waitForExistence(timeout: 5), "No report reasons")
        snap(app, "Report 01 reasons")
        app.buttons["It’s spam or an ad"].firstMatch.tap()
        XCTAssertTrue(app.alerts["Reported"].waitForExistence(timeout: 10), "No thank-you after reporting")
        snap(app, "Report 02 thanks")
        app.alerts["Reported"].buttons["OK"].tap()
        XCTAssertFalse(found(reported, in: app), "\(reported) still shows after reporting it")

        // 2. Hide the cook from another of their recipes: none of their recipes show.
        open(hidden, in: app)
        app.buttons["catalogRecipe.more"].firstMatch.tap()
        app.buttons["Hide Recipes From This Cook"].firstMatch.tap()
        let confirm = app.buttons["Hide Their Recipes"].firstMatch
        XCTAssertTrue(confirm.waitForExistence(timeout: 5), "No confirmation before hiding the cook")
        snap(app, "Report 03 hide cook")
        confirm.tap()
        XCTAssertTrue(app.alerts["Hidden"].waitForExistence(timeout: 10), "No message after hiding the cook")
        app.alerts["Hidden"].buttons["OK"].tap()
        XCTAssertFalse(found(other, in: app), "\(other) still shows after hiding its cook")
        snap(app, "Report 04 cook hidden")
    }

    /// Searches Shared Recipes and opens the recipe.
    @MainActor
    private func open(_ name: String, in app: XCUIApplication) {
        let hit = search(name, in: app)
        XCTAssertTrue(hit.waitForExistence(timeout: 10), "Search didn’t find \(name)")
        hit.tap()
        XCTAssertTrue(app.buttons["catalogRecipe.more"].firstMatch.waitForExistence(timeout: 10), "\(name) didn’t open")
    }

    @MainActor
    private func found(_ name: String, in app: XCUIApplication) -> Bool {
        let hit = search(name, in: app)
        sleep(3)
        return hit.exists
    }

    @MainActor
    private func search(_ name: String, in app: XCUIApplication) -> XCUIElement {
        let field = app.textFields["DiscoverSearch"].firstMatch
        reveal(field, in: app, maxSwipes: 6)
        field.tap()
        let clear = app.buttons["Clear search"].firstMatch
        if clear.exists { clear.tap() }
        if !app.keyboards.firstMatch.exists { field.tap() }
        field.typeText(name)
        let browse = app.descendants(matching: .any)["DiscoverBrowseAll"].firstMatch
        return browse.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch
    }
}
