import XCTest

/// Signing in and out, the sharer's code, and setting up a new household, for the invite tests.
extension XCTestCase {
    @MainActor
    func readCode(_ app: XCUIApplication) -> String {
        let codeText = app.staticTexts["Your code"]
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        return ((codeText.value as? String) ?? "").replacingOccurrences(of: " ", with: "")
    }

    @MainActor
    func openGiveAFreeMonth(_ app: XCUIApplication) {
        app.buttons["Household"].firstMatch.tap()
        let row = app.descendants(matching: .any).matching(NSPredicate(format: "label == 'Give a Free Month'"))
            .firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10), "No Give a Free Month row")
        row.tap()
        XCTAssertTrue(app.navigationBars["Give a Free Month"].waitForExistence(timeout: 10))
    }

    @MainActor
    func signOut(_ app: XCUIApplication) {
        app.buttons["Household"].firstMatch.tap()
        if app.navigationBars.buttons.firstMatch.exists, !app.navigationBars["Household"].exists {
            app.navigationBars.buttons.firstMatch.tap()
        }
        let account = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH 'Account'"))
            .firstMatch
        reveal(account, in: app, maxSwipes: 6)
        account.tap()
        let signOut = app.buttons["Sign Out"].firstMatch
        reveal(signOut, in: app, maxSwipes: 6)
        let rowFrame = signOut.frame
        signOut.tap()
        sleep(1)
        // The confirmation's Sign Out is the one that isn't the row just tapped.
        let confirm = app.buttons.matching(NSPredicate(format: "label == 'Sign Out'")).allElementsBoundByIndex
            .first { $0.frame != rowFrame && $0.isHittable }
        XCTAssertNotNil(confirm, "No Sign Out in the confirmation")
        confirm?.tap()
        XCTAssertTrue(app.textFields["Subject"].waitForExistence(timeout: 15), "Didn’t sign out")
    }

    @MainActor
    func signIn(_ app: XCUIApplication, subject name: String) {
        let subject = app.textFields["Subject"]
        XCTAssertTrue(subject.waitForExistence(timeout: 15))
        // Clear what's there by deleting from the end. Select All doesn't always show, and then
        // the name was added to the old one and signed in a brand-new person.
        subject.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        if let old = subject.value as? String, !old.isEmpty, old != subject.placeholderValue {
            subject.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: old.count + 2))
        }
        subject.typeText(name)
        XCTAssertEqual(subject.value as? String, name, "The sign-in field didn’t take the name")
        // The button names the account once the field has taken the text.
        let button = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch
        let words = name.split(separator: "-").map { $0.prefix(1).uppercased() + $0.dropFirst() }.joined(separator: " ")
        let ready = expectation(
            for: NSPredicate(format: "label ENDSWITH %@", words), evaluatedWith: button)
        _ = XCTWaiter.wait(for: [ready], timeout: 5)
        // Tap the sign-in button itself; the keyboard's Go would sign in too.
        if button.exists { button.tap() }
        if app.buttons["Thanks"].waitForExistence(timeout: 4) { app.buttons["Thanks"].tap() }
    }

    @MainActor
    func signInAsNewPerson(
        _ app: XCUIApplication, householdName: String,
        subject: String = "qa-invite-\(Int.random(in: 10000...99999))"
    ) -> XCUIApplication {
        signIn(app, subject: subject)
        XCTAssertTrue(app.staticTexts["Create a Household"].waitForExistence(timeout: 15))
        app.staticTexts["Create a Household"].tap()
        let name = app.textFields["Household name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.typeText(householdName)
        app.buttons["Create"].tap()
        if app.buttons["Start Planning"].waitForExistence(timeout: 15) { app.buttons["Start Planning"].tap() }
        XCTAssertTrue(app.buttons["Menu"].firstMatch.waitForExistence(timeout: 10))
        return app
    }

    /// Paste Text, the quickest way to type a recipe in.
    @MainActor
    func addPastedRecipe(_ app: XCUIApplication, name: String) {
        app.buttons["Menu"].firstMatch.tap()
        // An empty Menu has its own Add a Recipe button; otherwise it's in the ⋯ menu.
        let add = app.buttons["Add a Recipe"].firstMatch
        if !add.exists || !add.isHittable { app.buttons["Week Menu"].tap() }
        app.buttons["Add a Recipe"].firstMatch.tap()
        // The choices, then Import From a Website, which also takes pasted text.
        let importChoice = app.buttons["AddChoice-import"]
        XCTAssertTrue(importChoice.waitForExistence(timeout: 5), "Add a Recipe didn’t show its choices")
        importChoice.tap()
        let pasteText = app.buttons["Paste Text"].firstMatch
        XCTAssertTrue(pasteText.waitForExistence(timeout: 5), "Import has no Paste Text")
        pasteText.tap()
        let text = app.textViews.firstMatch
        XCTAssertTrue(text.waitForExistence(timeout: 5))
        text.tap()
        text.typeText("\(name)\nIngredients\n1 cup broth\n2 carrots\nSteps\nSimmer for 10 minutes.")
        app.buttons["Next"].tap()
        XCTAssertTrue(app.buttons["Save"].waitForExistence(timeout: 10))
        app.buttons["Save"].tap()
        // Save opens the new recipe; come back to the menu.
        XCTAssertTrue(app.buttons["More"].waitForExistence(timeout: 15), "\(name) didn’t save")
        app.navigationBars.buttons.firstMatch.tap()
    }

    /// "Step 2 done" and "2 of 3 done" (`number`, `done`, `total`) in the card over the app, then a tap closes it. It
    /// fades on its own too, so check it as soon as it shows.
    @MainActor
    func expectStepDone(_ app: XCUIApplication, number: Int, done: Int, of total: Int, name: String) {
        let card = app.descendants(matching: .any)["StepDoneCard"].firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 15), "No Step \(number) done card")
        XCTAssertTrue(card.label.hasPrefix("Step \(number) done"), "The card says \(card.label)")
        XCTAssertTrue(card.label.contains("\(done) of \(total) done"), "The card says \(card.label)")
        snap(app, name)
        card.tap()
        XCTAssertTrue(waitGone(card, timeout: 5), "Tapping the step card didn’t close it")
    }

    /// One recipe on this week's menu.
    @MainActor
    func planADinner(_ app: XCUIApplication) {
        app.buttons["Menu"].firstMatch.tap()
        app.buttons["Add Recipes"].tap()
        let add = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Add QA Invite Soup'")).firstMatch
        XCTAssertTrue(add.waitForExistence(timeout: 10), "Nothing to plan")
        add.tap()
        sleep(2)
        // The sheet's own Done; on iPad another Done can come first.
        let done = app.navigationBars["Add Recipes"].buttons["Done"]
        if done.exists { done.tap() } else { app.buttons["Done"].firstMatch.tap() }
        XCTAssertTrue(waitGone(app.navigationBars["Add Recipes"], timeout: 5), "Add Recipes didn’t close")
    }
}

/// Fake free month codes in the local API's pool (decision 755), so a free month earned in a
/// test is ready to redeem rather than on the way. The route is only there with developer
/// sign-in on. The codes are made up and Apple never sees them.
enum OfferCodeSeed {
    static var base: String { ProcessInfo.processInfo.environment["QA_API_BASE"] ?? "http://localhost:8082" }

    /// Adds `count` codes, signed in as a throwaway developer.
    static func seed(count: Int = 4) async throws {
        let login = try await call("/api/v1/auth/dev", token: nil, body: ["subject": "qa-offer-codes"])
        let token = try XCTUnwrap(login["accessToken"] as? String)
        _ = try await call("/api/v1/plus/dev/offer-codes", token: token, body: ["count": count])
    }

    private static func call(_ path: String, token: String?, body: [String: Any]) async throws -> [String: Any] {
        var request = URLRequest(url: URL(string: base + path)!)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            throw NSError(
                domain: "OfferCodeSeed", code: status,
                userInfo: [NSLocalizedDescriptionKey: "POST \(path) answered \(status)"])
        }
        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }
}
