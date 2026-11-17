import XCTest

/// An invite from start to finish on the local API (decision 698): the sharer's How it works
/// and code, a brand-new household entering it, the checklist ticking off as it adds a
/// recipe and plans a dinner, then the free month and a banner for each household. In
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
        // Unique per run: earlier runs left dozens of QA Invite Kitchens in Your Invites.
        // Random too: the matrix runs this on several simulators at once.
        let newName = "QA Invite Kitchen \(Int(Date().timeIntervalSince1970) % 100_000)\(Int.random(in: 100...999))"
        let newSubject = "qa-invite-\(Int.random(in: 10000...99999))"
        app = signInAsNewPerson(app, householdName: newName, subject: newSubject)
        openGiveAFreeMonth(app)
        let field = app.textFields["Code"]
        reveal(field, in: app, maxSwipes: 6)
        field.tap()
        field.typeText(code)
        app.buttons["Use Code"].tap()
        let progress = app.descendants(matching: .any)["ReferralProgress"].firstMatch
        XCTAssertTrue(progress.waitForExistence(timeout: 10), "No progress after entering the code")
        XCTAssertTrue(
            app.staticTexts["Add a recipe and plan a dinner to unlock your free month"].exists,
            "The progress message is missing")
        XCTAssertTrue(
            app.descendants(matching: .any)["PlusExplainer"].firstMatch.exists,
            "Give a Free Month doesn't say what Plus is")
        reveal(progress, in: app)
        snap(app, "Your free month, nothing done yet (\(mode))")

        // 3. A recipe, then a dinner: the checklist ticks off.
        addPastedRecipe(app, name: "QA Invite Soup 1")
        openGiveAFreeMonth(app)
        reveal(progress, in: app)
        snap(app, "Your free month, recipes done (\(mode))")
        planADinner(app)

        // 4. The invite counts. Planning rechecks the free month, and the confirmation shows
        // once. No Home press: on iPad it can bring the app back as a small window.
        let freeMonth = app.descendants(matching: .any)["FreeMonthOn"].firstMatch
        XCTAssertTrue(freeMonth.waitForExistence(timeout: 30), "No free month confirmation")
        XCTAssertTrue(app.staticTexts["Your free month of Plus is on"].exists, "The confirmation says something else")
        snap(app, "Your free month is on (\(mode))")
        freeMonth.buttons["Done"].tap()
        XCTAssertTrue(waitGone(freeMonth, timeout: 5), "The confirmation didn't close")
        // The news banner shows on the next sign-in.
        signOut(app)
        signIn(app, subject: newSubject)
        // A new person's taste questions can come up first; they wait.
        let later = app.buttons["Set Up Later"]
        if later.waitForExistence(timeout: 8) { later.tap() }
        let banner = app.descendants(matching: .any)["NewsBanner"].firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 20), "No banner for the new household")
        XCTAssertTrue(
            app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Your free month of Plus is on.'"))
                .firstMatch.exists, "The new household's banner says something else")
        snap(app, "Banner, free month on (\(mode))")
        banner.buttons["Close"].firstMatch.tap()
        XCTAssertTrue(waitGone(banner, timeout: 5), "The banner didn't close")
        signOut(app)

        // 5. The sharer: their banner, and the invite marked as earned.
        signIn(app, subject: "dev-simulator")
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The sharer didn't get to the Menu")
        // The banner names the newest invite that counted and shows once. In a parallel run
        // another simulator signed in as the same sharer can see it first, so without it, go to
        // Give a Free Month directly: the earned invite is what matters.
        let sharerBanner = app.descendants(matching: .any)["NewsBanner"].firstMatch
        if sharerBanner.waitForExistence(timeout: 20) {
            XCTAssertTrue(
                app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", " joined from your invite."))
                    .firstMatch.exists, "The sharer's banner doesn't name a new household")
            snap(app, "Banner, invite counted (\(mode))")
            sharerBanner.buttons.matching(NSPredicate(format: "label != 'Close'")).firstMatch.tap()
            // Tapping it opens Give a Free Month.
        } else {
            openGiveAFreeMonth(app)
        }
        XCTAssertTrue(app.navigationBars["Give a Free Month"].waitForExistence(timeout: 10))
        // Each invite row reads as one element: "name, how it's going, status".
        let row = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH %@", newName)).firstMatch
        // Newest first, so it's near the top of the list: stop the moment it's on screen. A
        // reveal that waits for it to clear the bottom third can swipe it past the top on iPad.
        let screen = app.frame
        for _ in 0..<10 where !(row.exists && screen.intersects(row.frame) && row.frame.minY > screen.minY + 60) {
            app.swipeUp(velocity: .slow)
        }
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
}
