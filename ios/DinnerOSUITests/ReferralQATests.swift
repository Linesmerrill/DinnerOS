import XCTest

/// An invite from start to finish on the local API (decision 698): the sharer's How it works
/// and code, a brand-new household entering it, the checklist ticking off as it saves 3
/// recipes and plans a dinner, then the free month and a banner for each household. In
/// development the API stands in for DeviceCheck, which the simulator can't use.
final class ReferralQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        executionTimeAllowance = 900
    }

    private var mode: String { ProcessInfo.processInfo.environment["QA_MODE"] ?? "light" }

    @MainActor
    func testAnInviteCountsOnceTheNewHouseholdSetsUp() throws {
        // 1. The sharer: How it works, then the code. An earlier test may have left another
        // person signed in, so sign in as the sharer explicitly.
        var app = launchSignedIn()
        signOut(app)
        signIn(app, subject: "dev-simulator")
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The sharer didn't get to the Menu")
        openGiveAFreeMonth(app)
        XCTAssertTrue(app.staticTexts["How it works"].waitForExistence(timeout: 10), "No How it works")
        XCTAssertTrue(
            app.staticTexts["Share your code with friends, family, even that one aunt."].exists,
            "How it works step 1 is missing")
        let codeText = app.staticTexts["Your code"]
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        let code = ((codeText.value as? String) ?? "").replacingOccurrences(of: " ", with: "")
        XCTAssertEqual(code.count, 8, "Couldn't read the code: \(code)")
        snap(app, "Give a Free Month, How it works (\(mode))")
        reveal(app.staticTexts["Your Invites"], in: app)
        snap(app, "Give a Free Month, Your Invites (\(mode))")
        signOut(app)

        // 2. A brand-new person starts a household and enters the code.
        let newName = "QA Invite Kitchen \(Int.random(in: 100...999))"
        app = signInAsNewPerson(app, householdName: newName)
        openGiveAFreeMonth(app)
        let field = app.textFields["Code"]
        reveal(field, in: app, maxSwipes: 6)
        field.tap()
        field.typeText(code)
        app.buttons["Use Code"].tap()
        let progress = app.descendants(matching: .any)["ReferralProgress"].firstMatch
        XCTAssertTrue(progress.waitForExistence(timeout: 10), "No progress after entering the code")
        XCTAssertTrue(
            app.staticTexts["Plan a dinner and save 3 recipes to unlock your free month"].exists,
            "The progress message is missing")
        reveal(progress, in: app)
        snap(app, "Your free month, nothing done yet (\(mode))")

        // 3. Three recipes, then a dinner: the checklist ticks off.
        for number in 1...3 {
            addPastedRecipe(app, name: "QA Invite Soup \(number)")
        }
        openGiveAFreeMonth(app)
        reveal(progress, in: app)
        snap(app, "Your free month, recipes done (\(mode))")
        planADinner(app)

        // 4. The invite counts: the new household's banner shows on the next open.
        XCUIDevice.shared.press(.home)
        sleep(2)
        app.activate()
        let banner = app.descendants(matching: .any)["NewsBanner"].firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 20), "No banner for the new household")
        XCTAssertTrue(
            app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Your free month of Plus is on.'"))
                .firstMatch.exists, "The new household's banner says something else")
        snap(app, "Banner, free month on (\(mode))")
        app.buttons["Close"].firstMatch.tap()
        XCTAssertTrue(waitGone(banner, timeout: 5), "The banner didn't close")
        signOut(app)

        // 5. The sharer: their banner, and the invite marked as earned.
        signIn(app, subject: "dev-simulator")
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The sharer didn't get to the Menu")
        let sharerBanner = app.descendants(matching: .any)["NewsBanner"].firstMatch
        XCTAssertTrue(sharerBanner.waitForExistence(timeout: 20), "No banner for the sharer")
        XCTAssertTrue(
            app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "\(newName) joined from your invite."))
                .firstMatch.exists, "The sharer's banner doesn't name the new household")
        snap(app, "Banner, invite counted (\(mode))")
        sharerBanner.buttons.matching(NSPredicate(format: "label != 'Close'")).firstMatch.tap()
        // Tapping it opens Give a Free Month.
        XCTAssertTrue(app.navigationBars["Give a Free Month"].waitForExistence(timeout: 10))
        let row = app.staticTexts[newName]
        reveal(row, in: app, maxSwipes: 8)
        XCTAssertTrue(row.exists, "The invite isn't in Your Invites")
        snap(app, "Your Invites, free month earned (\(mode))")
    }

    /// A household picks its own code, sees a blocked and a taken one, then gets a new random
    /// code (decision 699). A fresh household each run keeps it under 5 changes in 30 days.
    @MainActor
    func testANewOrCustomCodeReplacesTheOldOne() throws {
        var app = launchSignedIn()
        signOut(app)
        app = signInAsNewPerson(app, householdName: "QA Code Kitchen \(Int.random(in: 100...999))")
        openGiveAFreeMonth(app)
        let oldCode = readCode(app)
        XCTAssertEqual(oldCode.count, 8, "Couldn't read the code: \(oldCode)")

        // A reserved word isn't allowed.
        openChangeCode(app)
        let field = app.textFields["CustomCode"]
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText("admin")
        XCTAssertTrue(waitForCheck(app, "That code isn't allowed."), "Reserved word wasn't blocked")
        snap(app, "Change Your Code, not allowed (\(mode))")

        // A free custom code, after the confirmation.
        let custom = "qa-fam-\(Int.random(in: 10000...99999))"
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 5) + custom)
        XCTAssertTrue(waitForCheck(app, "You can use that code."), "\(custom) wasn't available")
        snap(app, "Change Your Code, available (\(mode))")
        app.buttons["Use This Code"].tap()
        XCTAssertTrue(app.alerts["Replace your code?"].waitForExistence(timeout: 5), "No confirmation")
        XCTAssertTrue(app.alerts.staticTexts["Your old code and link will stop working."].exists)
        snap(app, "Replace your code (\(mode))")
        app.alerts.buttons["Replace Code"].tap()
        XCTAssertTrue(waitGone(field, timeout: 10), "The sheet didn't close")
        XCTAssertEqual(readCode(app), custom, "The custom code isn't showing")
        snap(app, "Give a Free Month, custom code (\(mode))")

        // A new random code; the custom one is now retired for good.
        openChangeCode(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText(custom)
        XCTAssertTrue(waitForCheck(app, "That's already your code."), "The current code wasn't recognized")
        app.buttons["Get a New Random Code"].tap()
        XCTAssertTrue(app.alerts["Replace your code?"].waitForExistence(timeout: 5), "No confirmation")
        app.alerts.buttons["Replace Code"].tap()
        XCTAssertTrue(waitGone(field, timeout: 10), "The sheet didn't close")
        let random = readCode(app)
        XCTAssertEqual(random.count, 8, "Not a random code: \(random)")
        XCTAssertNotEqual(random, oldCode)
        openChangeCode(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText(custom)
        XCTAssertTrue(waitForCheck(app, "That code is taken."), "A retired code could be claimed again")
        snap(app, "Change Your Code, retired code taken (\(mode))")
        app.buttons["Cancel"].firstMatch.tap()
        // Leave the sharer signed in for the next test.
        signOut(app)
        signIn(app, subject: "dev-simulator")
    }

    // MARK: Steps

    @MainActor
    private func readCode(_ app: XCUIApplication) -> String {
        let codeText = app.staticTexts["Your code"]
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        return ((codeText.value as? String) ?? "").replacingOccurrences(of: " ", with: "")
    }

    @MainActor
    private func openChangeCode(_ app: XCUIApplication) {
        let button = app.buttons["Change Your Code"]
        reveal(button, in: app, maxSwipes: 6)
        button.tap()
        XCTAssertTrue(app.navigationBars["Change Your Code"].waitForExistence(timeout: 10), "No Change Your Code")
    }

    @MainActor
    private func waitForCheck(_ app: XCUIApplication, _ message: String) -> Bool {
        app.descendants(matching: .any)["CodeCheck"].firstMatch.waitForExistence(timeout: 10)
            && app.staticTexts[message].waitForExistence(timeout: 10)
    }

    @MainActor
    private func openGiveAFreeMonth(_ app: XCUIApplication) {
        app.buttons["Household"].firstMatch.tap()
        let row = app.descendants(matching: .any).matching(NSPredicate(format: "label == 'Give a Free Month'"))
            .firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10), "No Give a Free Month row")
        row.tap()
        XCTAssertTrue(app.navigationBars["Give a Free Month"].waitForExistence(timeout: 10))
    }

    @MainActor
    private func signOut(_ app: XCUIApplication) {
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
        XCTAssertTrue(app.textFields["Subject"].waitForExistence(timeout: 15), "Didn't sign out")
    }

    @MainActor
    private func signIn(_ app: XCUIApplication, subject name: String) {
        let subject = app.textFields["Subject"]
        XCTAssertTrue(subject.waitForExistence(timeout: 15))
        subject.tap()
        subject.press(forDuration: 1.0)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
        subject.typeText(name)
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
    private func signInAsNewPerson(_ app: XCUIApplication, householdName: String) -> XCUIApplication {
        signIn(app, subject: "qa-invite-\(Int.random(in: 10000...99999))")
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
    private func addPastedRecipe(_ app: XCUIApplication, name: String) {
        app.buttons["Menu"].firstMatch.tap()
        // An empty Menu has its own Add a Recipe button; otherwise it's in the ⋯ menu.
        let add = app.buttons["Add a Recipe"].firstMatch
        if !add.exists || !add.isHittable { app.buttons["Week Menu"].tap() }
        app.buttons["Add a Recipe"].firstMatch.tap()
        let text = app.textViews.firstMatch
        XCTAssertTrue(text.waitForExistence(timeout: 5))
        text.tap()
        text.typeText("\(name)\nIngredients\n1 cup broth\n2 carrots\nSteps\nSimmer for 10 minutes.")
        app.buttons["Next"].tap()
        XCTAssertTrue(app.buttons["Save"].waitForExistence(timeout: 10))
        app.buttons["Save"].tap()
        // Save opens the new recipe; come back to the menu.
        XCTAssertTrue(app.buttons["More"].waitForExistence(timeout: 15), "\(name) didn't save")
        app.navigationBars.buttons.firstMatch.tap()
    }

    /// One recipe on this week's menu.
    @MainActor
    private func planADinner(_ app: XCUIApplication) {
        app.buttons["Menu"].firstMatch.tap()
        app.buttons["Add Recipes"].tap()
        let add = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Add QA Invite Soup'")).firstMatch
        XCTAssertTrue(add.waitForExistence(timeout: 10), "Nothing to plan")
        add.tap()
        sleep(2)
        app.buttons["Done"].firstMatch.tap()
    }
}
