import XCTest

/// A link that can't be read is never a dead end (decision 728): the screen says what went wrong
/// in plain words and offers Write Your Own, which opens the blank form with the link kept, and
/// Paste Text. The link is on a reserved name that never resolves, so the local API, the phone's
/// fetch and its web view all fail fast and nothing is saved.
final class ImportFailureQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    @MainActor
    func testAnUnreadableLinkOffersWriteYourOwnWithTheLinkKept() throws {
        let link = "https://recipes.invalid/hand-pies-\(Int.random(in: 1000...9999))"
        let app = launchSignedIn()
        app.buttons["Week Menu"].tap()
        // An empty library also shows Add a Recipe on the Menu, under what Week Menu opened (a
        // sheet whose choice has a subtitle): tap the one on top.
        let adds = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Add a Recipe'"))
        XCTAssertTrue(adds.firstMatch.waitForExistence(timeout: 5), "No Add a Recipe")
        sleep(1)
        let add = adds.allElementsBoundByIndex.last { $0.isHittable } ?? adds.firstMatch
        add.tap()
        XCTAssertTrue(app.navigationBars["Add a Recipe"].waitForExistence(timeout: 5), "No Add a Recipe")
        app.buttons["AddChoice-import"].tap()

        let field = app.textFields["https://…"]
        XCTAssertTrue(field.waitForExistence(timeout: 5), "No link field")
        field.tap()
        field.typeText(link)
        app.navigationBars["Import From a Website"].buttons["Next"].tap()

        // What failed, in plain words from the server.
        let message = app.staticTexts.matching(
            NSPredicate(format: "label == %@", "That page didn't load. Check the link and try again.")
        ).firstMatch
        XCTAssertTrue(message.waitForExistence(timeout: 45), "No plain message after the link failed")
        XCTAssertTrue(waitGone(app.keyboards.firstMatch), "The keyboard covers what to do next")
        let pasteText = app.buttons["ImportFailedPasteText"]
        let writeOwn = app.buttons["ImportFailedWriteOwn"]
        reveal(writeOwn, in: app, maxSwipes: 4)
        XCTAssertTrue(writeOwn.exists, "No Write Your Own after a failed import")
        XCTAssertEqual(writeOwn.label, "Write Your Own")
        XCTAssertTrue(pasteText.exists, "No Paste Text after a failed import")
        snap(app, "Import failed, what to do next (\(mode))")

        // Write Your Own: the blank form, with the link kept as where the recipe came from.
        writeOwn.tap()
        XCTAssertTrue(app.textFields["Recipe name"].waitForExistence(timeout: 5), "Write Your Own has no form")
        let source = app.descendants(matching: .any)["DraftSourceLink"].firstMatch
        XCTAssertTrue(source.waitForExistence(timeout: 5), "The form doesn't keep the link")
        XCTAssertEqual(source.value as? String, link)
        snap(app, "Import failed, Write Your Own with the link (\(mode))")
    }
}
